package main

import (
	"database/sql"
	"strings"
	"unicode/utf8"
)

// Index distinct 1/2/3-rune fragments, including Chinese short queries,
// punctuation and NUL. Hex tokens avoid FTS tokenization/case-folding semantics.
// Overflow documents remain candidates for every query, preserving exactness
// while bounding each entry's index construction and on-disk token count.
const maxSearchGrams = 4096

type searchGram struct {
	runes  [3]rune
	length int
}

func (g searchGram) token() string {
	const hex = "0123456789abcdef"
	var b [19]byte
	b[0] = 'g'
	for i := 0; i < g.length; i++ {
		r := uint32(g.runes[i])
		for j := 5; j >= 0; j-- {
			b[1+i*6+j] = hex[r&15]
			r >>= 4
		}
	}
	return string(b[:1+6*g.length])
}

func searchDocument(text string) string {
	seen := make(map[searchGram]struct{})
	var out strings.Builder
	var window [3]rune
	count := 0
	for _, r := range text {
		window[0], window[1], window[2] = window[1], window[2], r
		count++
		for length := 1; length <= min(3, count); length++ {
			g := searchGram{length: length}
			copy(g.runes[:], window[3-length:])
			if _, ok := seen[g]; ok {
				continue
			}
			if len(seen) == maxSearchGrams {
				out.WriteString("overflow")
				return out.String()
			}
			seen[g] = struct{}{}
			out.WriteString(g.token())
			out.WriteByte(' ')
		}
	}
	return out.String()
}

func searchMatch(q string) string {
	if q == "" || !utf8.ValidString(q) {
		return ""
	}
	runes := []rune(q)
	length := min(3, len(runes))
	last := len(runes) - length
	seen := map[searchGram]bool{}
	terms := []string{}
	// Distributed fragments limit query complexity even for pasted long text.
	// All are necessary conditions, then instr verifies the entire substring.
	for i := 0; i < 6; i++ {
		start := last * i / 5
		g := searchGram{length: length}
		copy(g.runes[:], runes[start:start+length])
		if seen[g] {
			continue
		}
		seen[g] = true
		terms = append(terms, g.token())
	}
	return "(" + strings.Join(terms, " AND ") + ") OR overflow"
}

const searchSchema = `
	CREATE VIRTUAL TABLE history_search USING fts5(
		grams, content='', contentless_delete=1, detail=none, tokenize='ascii'
	);
	INSERT INTO history_search(history_search,rank) VALUES('secure-delete',1);
	CREATE TRIGGER history_search_delete BEFORE DELETE ON history BEGIN
		DELETE FROM history_search WHERE rowid=OLD.search_id;
	END;`

// An image's recognized text replaces its search prefix (image_text.go).
const searchDocumentSQL = "coalesce(o.search,c.search_prefix) || h.source_search"
const searchDocumentJoin = " JOIN history_content c ON c.id=h.id LEFT JOIN history_ocr o ON o.id=h.id"

func replaceSearchSQL(tx *sql.Tx, id string) error {
	return writeSearchSQL(tx, `SELECT h.search_id,`+searchDocumentSQL+` FROM history h`+searchDocumentJoin+` WHERE h.id=?`, id)
}

// writeSearchSQL indexes the document selected by query. Upgrades pass the
// document of their own schema version; later tables do not exist yet.
func writeSearchSQL(tx *sql.Tx, query, id string) error {
	var searchID int64
	var text string
	if err := tx.QueryRow(query, id).Scan(&searchID, &text); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM history_search WHERE rowid=?", searchID); err != nil {
		return err
	}
	_, err := tx.Exec("INSERT INTO history_search(rowid,grams) VALUES(?,?)", searchID, searchDocument(text))
	return err
}

// Contentless FTS deletion leaves tombstones even with secure-delete enabled.
// Merge them before committing removals/source replacements; secure_delete
// also clears released SQLite pages. An open panel can still retain old WAL
// pages, just as it retains the deleted original clipboard content.
func purgeSearchSQL(tx *sql.Tx) error {
	_, err := tx.Exec("INSERT INTO history_search(history_search) VALUES('optimize')")
	return err
}

func (s *sqliteStore) upgradeSearch() error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE history ADD COLUMN search_id INTEGER;
			UPDATE history SET search_id=rowid;
			CREATE UNIQUE INDEX history_search_id ON history(search_id);` + searchSchema)
		if err != nil {
			return err
		}
		var after int64
		for {
			rows, err := tx.Query("SELECT search_id,id FROM history WHERE search_id>? ORDER BY search_id LIMIT 100", after)
			if err != nil {
				return err
			}
			ids := []string{}
			for rows.Next() {
				var id string
				if err = rows.Scan(&after, &id); err != nil {
					break
				}
				ids = append(ids, id)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				break
			}
			for _, id := range ids {
				if err := writeSearchSQL(tx, `SELECT h.search_id,c.search_prefix || h.source_search
					FROM history h JOIN history_content c ON c.id=h.id WHERE h.id=?`, id); err != nil {
					return err
				}
			}
		}
		_, err = tx.Exec("PRAGMA user_version=3")
		return err
	})
}
