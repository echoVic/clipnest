package main

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxCachedMatches = 16384
const maxCachedSearches = 2

type searchResult struct {
	query string
	kind  string
	total int
	ids   []int64
}

func searchSQL(q, kind string) (string, []any) {
	if utf8.ValidString(q) && utf8.RuneCountInString(q) <= 3 {
		g := searchGram{length: utf8.RuneCountInString(q)}
		copy(g.runes[:], []rune(q))
		query := `SELECT h.search_id FROM history h WHERE h.search_id IN (
			SELECT rowid FROM history_search WHERE history_search MATCH ?
			UNION SELECT h.search_id FROM history_search
				JOIN history h ON h.search_id=history_search.rowid` + searchDocumentJoin + `
				WHERE history_search MATCH 'overflow' AND instr(` + searchDocumentSQL + `,?)>0
		)`
		args := []any{g.token(), q}
		if kind != "" {
			query += " AND h.kind=?"
			args = append(args, kind)
		}
		return query + " ORDER BY h.position", args
	}
	from := " FROM history h" + searchDocumentJoin
	// Most hits are inside one field. Short-circuit there without copying a
	// large prefix for every candidate. Only the small byte tail is needed
	// for matches crossing into source; BLOB substr preserves embedded NUL.
	where := ` WHERE (instr(coalesce(o.search,c.search_prefix),?)>0 OR instr(h.source_search,?)>0
		OR instr(CAST(substr(CAST(coalesce(o.search,c.search_prefix) AS BLOB),-?) AS TEXT)||h.source_search,?)>0)`
	args := []any{q, q, len(q) - 1, q}
	if kind != "" {
		where += " AND h.kind=?"
		args = append(args, kind)
	}
	if match := searchMatch(q); match != "" {
		from += " JOIN history_search ON history_search.rowid=h.search_id"
		where += " AND history_search MATCH ?"
		args = append(args, match)
	}
	return "SELECT h.search_id" + from + where + " ORDER BY h.position", args
}

// A search scans candidates once, collecting only integer IDs. Small result
// sets serve every subsequent page without reading text again. Huge matches
// retain only their count, so content size/history length cannot grow RAM
// without bound. Both paths retain the same exact order and total.
func (v *historyView) searchPage(ctx context.Context, q, kind string, offset, limit int) (HistoryPage, error) {
	for i, result := range v.searches {
		if result.query != q || result.kind != kind {
			continue
		}
		v.searches = append(append(v.searches[:i:i], v.searches[i+1:]...), result)
		if result.ids != nil {
			start := min(offset, len(result.ids))
			return v.searchSummaries(ctx, result.ids[start:min(start+limit, len(result.ids))], result.total, offset)
		}
		query, args := searchSQL(q, kind)
		rows, err := v.tx.QueryContext(ctx, query+" LIMIT ? OFFSET ?", append(args, limit, offset)...)
		if err != nil {
			return HistoryPage{}, err
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				break
			}
			ids = append(ids, id)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return HistoryPage{}, err
		}
		return v.searchSummaries(ctx, ids, result.total, offset)
	}
	query, args := searchSQL(q, kind)
	rows, err := v.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return HistoryPage{}, err
	}
	result := searchResult{query: q, kind: kind, ids: []int64{}}
	pageIDs := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		if result.total >= offset && len(pageIDs) < limit {
			pageIDs = append(pageIDs, id)
		}
		if result.total < maxCachedMatches {
			result.ids = append(result.ids, id)
		}
		result.total++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return HistoryPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return HistoryPage{}, err
	}
	if result.total > maxCachedMatches {
		result.ids = nil
	}
	v.searches = append(v.searches, result)
	if len(v.searches) > maxCachedSearches {
		v.searches[0] = searchResult{}
		v.searches = v.searches[1:]
	}
	return v.searchSummaries(ctx, pageIDs, result.total, offset)
}

func (v *historyView) searchSummaries(ctx context.Context, ids []int64, total, offset int) (HistoryPage, error) {
	page := HistoryPage{Entries: []Entry{}, Total: total, Offset: offset}
	if len(ids) == 0 {
		return page, ctx.Err()
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := v.tx.QueryContext(ctx, fmt.Sprintf(`SELECT id,kind,title,excerpt,source,created,width,height
		FROM history WHERE search_id IN (%s) ORDER BY position`, marks), args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Title, &e.Excerpt, &e.Source, &e.Created, &e.Width, &e.Height); err != nil {
			return page, err
		}
		page.Entries = append(page.Entries, e)
	}
	return page, rows.Err()
}
