package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
)

const historyPageSize = 50

type HistoryPage struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
	Offset  int     `json:"offset"`
}

// The read transaction retains the exact ordering AND payloads seen when the
// panel opens, even if the writer recopies or expires an item during selection.
type historyView struct {
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	tx            *sql.Tx
	legacy        []Entry
	closed        bool
	lastQuery     string
	lastKind      string
	lastTotal     int
	counted       bool
	searches      []searchResult
	queryMu       sync.Mutex
	queryRevision uint64
	queryContext  context.Context
	queryCancel   context.CancelFunc
}

func (h *History) OpenView() (*historyView, error) {
	return h.openView(context.Background())
}

func (h *History) openView(parent context.Context) (*historyView, error) {
	if err := h.Save(); err != nil {
		return nil, err
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	v := &historyView{ctx: ctx, cancel: cancel}
	if h.db == nil {
		v.legacy, _ = h.Snapshot()
		return v, nil
	}
	var err error
	v.tx, err = h.db.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err == nil {
		// BEGIN alone doesn't acquire a snapshot; the first read does.
		err = v.tx.QueryRowContext(ctx, "SELECT count(*) FROM history").Scan(&v.lastTotal)
		v.counted = err == nil
	}
	if err != nil {
		v.Close()
		return nil, err
	}
	return v, nil
}

func (v *historyView) Close() {
	v.cancel()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return
	}
	v.closed = true
	if v.tx != nil {
		_ = v.tx.Rollback()
	}
	v.legacy = nil
	v.searches = nil
}

func (v *historyView) Page(q, kind string, offset, limit int) (HistoryPage, error) {
	return v.page(v.ctx, q, kind, offset, limit)
}

// Separate from the transaction mutex: a new query interrupts an old SQL scan
// before waiting for its result. Canceling only the query context preserves the
// panel's read transaction and lazy detail access.
func (v *historyView) searchContext(revision uint64) (context.Context, error) {
	v.queryMu.Lock()
	defer v.queryMu.Unlock()
	if v.ctx.Err() != nil || revision < v.queryRevision {
		return nil, context.Canceled
	}
	if v.queryContext == nil || revision > v.queryRevision {
		if v.queryCancel != nil {
			v.queryCancel()
		}
		v.queryRevision = revision
		v.queryContext, v.queryCancel = context.WithCancel(v.ctx)
	}
	return v.queryContext, nil
}

func (v *historyView) SearchPage(revision uint64, q, kind string, offset, limit int) (HistoryPage, error) {
	ctx, err := v.searchContext(revision)
	if err != nil {
		return HistoryPage{}, err
	}
	return v.page(ctx, q, kind, offset, limit)
}

func (v *historyView) page(ctx context.Context, q, kind string, offset, limit int) (HistoryPage, error) {
	page := HistoryPage{Entries: []Entry{}, Offset: offset}
	if offset < 0 || limit < 1 || limit > 200 || len(q) > maxText {
		return page, errors.New("分页参数无效")
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if kind == "all" {
		kind = ""
	}
	switch kind {
	case "", "text", "link", "code", "image":
	default:
		return page, errors.New("内容类型无效")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed || v.ctx.Err() != nil {
		return page, errors.New("面板已关闭，请重新打开")
	}
	if err := ctx.Err(); err != nil {
		return page, err
	}
	if v.tx == nil {
		matches := queryEntries(v.legacy, q, kind)
		page.Total = len(matches)
		start := min(offset, page.Total)
		page.Entries = matches[start:min(start+limit, page.Total)]
		return page, nil
	}
	if q != "" {
		return v.searchPage(ctx, q, kind, offset, limit)
	}
	from := " FROM history h"
	where := " WHERE 1=1"
	args := []any{}
	if kind != "" {
		where += " AND h.kind=?"
		args = append(args, kind)
	}
	if !v.counted || v.lastQuery != q || v.lastKind != kind {
		if err := v.tx.QueryRowContext(ctx, "SELECT count(*)"+from+where, args...).Scan(&v.lastTotal); err != nil {
			return page, err
		}
		v.lastQuery, v.lastKind, v.counted = q, kind, true
	}
	page.Total = v.lastTotal
	rows, err := v.tx.QueryContext(ctx, `SELECT h.id,h.kind,h.title,h.excerpt,h.source,h.created,h.width,h.height`+
		from+where+" ORDER BY h.position LIMIT ? OFFSET ?", append(args, limit, offset)...)
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

func (v *historyView) Detail(id string) (Entry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed || v.ctx.Err() != nil {
		return Entry{}, errors.New("面板已关闭，请重新打开")
	}
	if v.tx == nil {
		for _, e := range v.legacy {
			if e.ID == id {
				if e.Kind == "image" {
					e.ImageTextState = imageTextNeedsSQLite
					if !imageTextSupported {
						e.ImageTextState = imageTextUnsupported
					}
				}
				return e, nil
			}
		}
		return Entry{}, errors.New("这条内容已移除")
	}
	var text, source, imageText string
	var image []byte
	var created int64
	var state int
	var enabled bool
	err := v.tx.QueryRowContext(v.ctx, `SELECT c.text,c.image,h.source,h.created,h.ocr_state,coalesce(o.text,''),
		(SELECT ocr_enabled FROM settings WHERE id=1) FROM history h`+searchDocumentJoin+` WHERE h.id=?`, id).Scan(
		&text, &image, &source, &created, &state, &imageText, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, errors.New("这条内容已移除")
	}
	if err != nil {
		return Entry{}, err
	}
	e, err := newEntry(text, image, source)
	if err != nil {
		return Entry{}, err
	}
	if e.ID != id {
		return Entry{}, errors.New("历史内容与标识不一致")
	}
	e.Created = created
	imageTextDetail(&e, state, imageText, enabled)
	return e, nil
}
