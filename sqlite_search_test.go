package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSQLiteSearchIndexMatchesSubstringContract(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	texts := []string{
		"大小写 ÜNICODE 中文，剪贴板搜索", "代码 const value = foo_bar %_ [a-z] \"OR\"",
		"空格  保留\n换行\t保留", "before\x00after 中文", "emoji 👩🏽‍💻 🚀组合 e\u0301",
		"https://example.com/path?q=One", "尾部字段",
	}
	want := []Entry{}
	for _, text := range texts {
		e, err := newEntry(text, nil, "SOURCE 终端")
		if err != nil {
			t.Fatal(err)
		}
		s.History.Add(e)
		want = append([]Entry{e}, want...)
	}
	rng := rand.New(rand.NewSource(17))
	var varied strings.Builder
	for i := 0; i < 12000; i++ {
		varied.WriteRune(rune(0x4e00 + rng.Intn(12000)))
	}
	varied.WriteString(" 溢出后的独特关键词")
	e := entry(t, varied.String())
	s.History.Add(e)
	want = append([]Entry{e}, want...)
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	queries := []string{"", " ", "中", "中文", "Ünicode", "搜索", "%_", "_", "[a-z]", "\"OR\"", "\n换行",
		"after", "\x00after", "👩🏽‍💻", "🚀", "e\u0301", "source", "终端", "字段 source", "独特关键词", "does-not-exist"}
	for _, text := range texts {
		r := []rune(text)
		for length := 1; length <= 5; length++ {
			for i := 0; i+length <= len(r); i += 3 {
				queries = append(queries, string(r[i:i+length]))
			}
		}
	}
	for _, q := range queries {
		for _, kind := range []string{"all", "text", "link", "code", "image"} {
			got, err := v.Page(q, kind, 0, 50)
			expected := referenceQuery(want, q, kind)
			if err != nil || got.Total != len(expected) || !reflect.DeepEqual(got.Entries, expected) {
				t.Fatalf("%q/%s diverged from substring oracle: %d/%d %v", q, kind, got.Total, len(expected), err)
			}
		}
	}
	var indexed int
	if err := s.History.db.db.QueryRow("SELECT count(*) FROM history_search").Scan(&indexed); err != nil || indexed != len(want) {
		t.Fatal("durable substring index missing", indexed, err)
	}
}

func TestSQLiteSearchIndexTracksRecopyAndCleanup(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := entry(t, "immutable text")
	e.Source = "original"
	s.History.Add(e)
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	e.Source = "replacement"
	s.History.Add(e)
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	page, err := v.Page("original", "", 0, 50)
	if err != nil || page.Total != 1 {
		t.Fatal("snapshot index mutated", err)
	}
	v.Close()
	v, err = s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	for q, n := range map[string]int{"original": 0, "replacement": 1, "text immutable": 1} {
		p, err := v.Page(q, "", 0, 50)
		if err != nil || p.Total != n {
			t.Fatal("source or cross-field index stale", q, p.Total, err)
		}
	}
	v.Close()
	s.History.Clear()
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.History.db.db.QueryRow("SELECT count(*) FROM history_search").Scan(&n); err != nil || n != 0 {
		t.Fatal("deletion left indexed private content", n, err)
	}
}

func TestSQLiteV2SearchIndexUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), databaseName)
	s, err := openSQLite(path, true)
	if err != nil {
		t.Fatal(err)
	}
	e := entry(t, "旧库 中文 token")
	if err := s.transaction(func(tx *sql.Tx) error { return saveHistorySQL(tx, archive{1, false, []Entry{e}}) }); err != nil {
		t.Fatal(err)
	}
	// Preserve the actual v2 schema, not merely its version marker.
	_, err = s.db.Exec(`DROP TRIGGER IF EXISTS history_search_delete; DROP TABLE IF EXISTS history_search;
		DROP INDEX IF EXISTS history_search_id; ALTER TABLE history DROP COLUMN search_id; PRAGMA user_version=2`)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	s, err = openSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	var version, n int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatal("schema not upgraded", version, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM history_search").Scan(&n); err != nil || n != 1 {
		t.Fatal("index not backfilled", n, err)
	}
}

func TestSearchPaginationReusesFrozenMatchIDs(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 135; i++ {
		s.History.Add(entry(t, fmt.Sprintf("needle-%04d", i)))
	}
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	first, err := v.Page("needle", "", 0, 50)
	if err != nil || first.Total != 135 {
		t.Fatal("first result", err)
	}
	// The test transaction rolls this back on Close. A cached second page
	// must only read metadata; needing the index again proves a repeated scan.
	if _, err := v.tx.Exec("DROP TABLE history_search"); err != nil {
		t.Fatal(err)
	}
	last, err := v.Page("needle", "", 100, 50)
	if err != nil || len(last.Entries) != 35 || last.Total != 135 || last.Entries[0].Title != "needle-0034" {
		t.Fatal("pagination searched content again instead of reusing matches", err)
	}
}

func TestShortSearchUsesExactFragmentsWithoutReadingPayload(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := entry(t, "a 中文! b")
	s.History.Add(e)
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	// Rollback on Close restores payloads. A complete 1/2/3-rune fragment
	// already proves this substring; summary search must not fetch its body.
	if _, err := v.tx.Exec("DELETE FROM history_content"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"中", "中文", "中文!"} {
		p, err := v.Page(q, "", 0, 50)
		if err != nil || p.Total != 1 || p.Entries[0].ID != e.ID {
			t.Fatal("short search fetched payload", q, p.Total, err)
		}
	}
}

func TestSearchCancellationPreservesPanelSnapshot(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := entry(t, "中文 needle")
	s.History.Add(e)
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	old, err := v.searchContext(1)
	if err != nil {
		t.Fatal(err)
	}
	v.mu.Lock()
	done := make(chan error, 1)
	go func() { _, err := v.SearchPage(1, "needle", "", 0, 50); done <- err }()
	next, err := v.searchContext(2)
	if err != nil {
		v.mu.Unlock()
		t.Fatal(err)
	}
	select {
	case <-old.Done():
	default:
		v.mu.Unlock()
		t.Fatal("old search was not canceled")
	}
	if next.Err() != nil {
		v.mu.Unlock()
		t.Fatal("new search canceled")
	}
	v.mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("queued stale page ran", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stale page stuck")
	}
	p, err := v.SearchPage(2, "中文", "", 0, 50)
	if err != nil || p.Total != 1 {
		t.Fatal("new search blocked", err)
	}
	if _, err := v.Detail(e.ID); err != nil {
		t.Fatal("canceling search lost panel transaction", err)
	}
	if _, err := v.SearchPage(1, "needle", "", 0, 50); !errors.Is(err, context.Canceled) {
		t.Fatal("late stale query accepted", err)
	}
}

func TestLargeSearchDoesNotTruncateResultsOrGrowCache(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	total := maxCachedMatches + 3
	for i := 0; i < total; i++ {
		if err := s.History.Add(entry(t, fmt.Sprintf("common %05d", i))); err != nil {
			t.Fatal(err)
		}
		if i%512 == 0 {
			if err := s.History.Save(); err != nil {
				t.Fatal(err)
			}
		}
	}
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	for _, q := range []string{"common", "common", "com", "com", "co", "co", "c", "c"} {
		p, err := v.Page(q, "", total-2, 50)
		if err != nil || p.Total != total || len(p.Entries) != 2 || p.Entries[0].Title != "common 00001" {
			t.Fatal("large match lost results", p.Total, err)
		}
		for _, result := range v.searches {
			if result.ids != nil {
				t.Fatal("unbounded result IDs retained")
			}
		}
		if len(v.searches) > maxCachedSearches {
			t.Fatal("too many cached queries")
		}
	}
}

func TestSearchIndexStableAcrossVacuumAndRollback(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items := []Entry{}
	for i := 0; i < 5; i++ {
		e := entry(t, fmt.Sprintf("indexed %d", i))
		e.Source = "\x01"
		items = append(items, e)
		s.History.Add(e)
	}
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	for _, e := range items[:3] {
		s.History.Remove(e.ID)
	}
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.History.db.db.Exec("VACUUM"); err != nil {
		t.Fatal(err)
	}
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	p, err := v.Page("indexed", "", 0, 50)
	v.Close()
	if err != nil || p.Total != 2 || p.Entries[0].ID != items[4].ID {
		t.Fatal("vacuum invalidated index IDs", err)
	}
	_, err = s.History.db.db.Exec(`CREATE TRIGGER reject_pause BEFORE UPDATE ON settings
		BEGIN SELECT RAISE(ABORT,'injected after-index failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	e := items[4]
	e.Source = "\x03"
	s.History.Add(e)
	s.History.Pause(true)
	if err := s.History.Save(); err == nil {
		t.Fatal("failure after index update was hidden")
	}
	a, _, err := s.History.db.load()
	if err != nil || a.Paused || a.Entries[0].Source != "\x01" {
		t.Fatal("metadata was not rolled back", err)
	}
	var n int
	err = s.History.db.db.QueryRow("SELECT count(*) FROM history_search WHERE history_search MATCH 'g000003'").Scan(&n)
	if err != nil || n != 0 {
		t.Fatal("failed source replacement leaked into index", err)
	}
	if _, err := s.History.db.db.Exec("DROP TRIGGER reject_pause"); err != nil {
		t.Fatal(err)
	}
	if err := s.History.Save(); err != nil {
		t.Fatal("failed index write did not retry", err)
	}
}

func TestDeletedSearchFragmentsArePurged(t *testing.T) {
	for _, action := range []string{"remove", "clear", "expire", "recopy"} {
		t.Run(action, func(t *testing.T) {
			s, err := openStorage(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			e, err := newEntry("\x02", nil, "\x01")
			if err != nil {
				t.Fatal(err)
			}
			e.Created = time.Now().Add(-72 * time.Hour).UnixMilli()
			s.History.Add(e)
			if err := s.History.Save(); err != nil {
				t.Fatal(err)
			}
			contains := func() bool {
				var n int
				err := s.History.db.db.QueryRow("SELECT count(*) FROM history_search_data WHERE instr(block,CAST(? AS BLOB))>0", "g000001").Scan(&n)
				if err != nil {
					t.Fatal(err)
				}
				return n > 0
			}
			if !contains() {
				t.Fatal("fixture did not contain private indexed fragment")
			}
			switch action {
			case "remove":
				s.History.Remove(e.ID)
			case "clear":
				s.History.Clear()
			case "expire":
				if err := s.History.SetRetention(RetentionPolicy{0, 1}); err != nil {
					t.Fatal(err)
				}
			case "recopy":
				e.Source = "new source"
				s.History.Add(e)
			}
			if err := s.History.Save(); err != nil {
				t.Fatal(err)
			}
			if contains() {
				t.Fatal("deleted private fragment remained in FTS storage")
			}
		})
	}
}

func TestActiveSearchCancellationKeepsTransactionUsable(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := entry(t, "still readable")
	s.History.Add(e)
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ctx, err := v.searchContext(1)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		close(started)
		var n int64
		err := v.tx.QueryRowContext(ctx, `WITH RECURSIVE n(x) AS (
			VALUES(0) UNION ALL SELECT x+1 FROM n WHERE x<100000000
		) SELECT sum(x) FROM n`).Scan(&n)
		done <- err
	}()
	<-started
	time.Sleep(20 * time.Millisecond)
	if _, err := v.searchContext(2); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("active SQL was not canceled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("active SQL did not stop")
	}
	if _, err := v.Detail(e.ID); err != nil {
		t.Fatal("SQL interrupt invalidated snapshot", err)
	}
	if p, err := v.SearchPage(2, "readable", "", 0, 50); err != nil || p.Total != 1 {
		t.Fatal("new query failed after interruption", err)
	}
}

// The previous SQLite path is retained only as a performance and result oracle.
func scanSearchPage(tx *sql.Tx, q, kind string, offset, limit int) (HistoryPage, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	p := HistoryPage{Entries: []Entry{}, Offset: offset}
	from := " FROM history h JOIN history_content c ON c.id=h.id WHERE instr(c.search_prefix || h.source_search,?)>0"
	args := []any{q}
	if kind != "" && kind != "all" {
		from += " AND h.kind=?"
		args = append(args, kind)
	}
	if err := tx.QueryRow("SELECT count(*)"+from, args...).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := tx.Query("SELECT h.id,h.kind,h.title,h.excerpt,h.source,h.created,h.width,h.height"+from+" ORDER BY h.position LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Title, &e.Excerpt, &e.Source, &e.Created, &e.Width, &e.Height); err != nil {
			return p, err
		}
		p.Entries = append(p.Entries, e)
	}
	return p, rows.Err()
}

func BenchmarkSQLiteSearch(b *testing.B) {
	for _, size := range []struct{ n, bytes int }{{10000, 2048}, {1000, 128 << 10}} {
		b.Run(fmt.Sprintf("%dx%d", size.n, size.bytes), func(b *testing.B) {
			s, err := openStorage(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			for i := 0; i < size.n; i++ {
				text := fmt.Sprintf("record-%05d\n", i) + strings.Repeat("ordinary text 文本代码 example variable ", size.bytes/42)
				if i%100 == 0 {
					text += " needle独特关键词"
				}
				e, err := newEntry(text, nil, "Editor")
				if err != nil {
					b.Fatal(err)
				}
				s.History.Add(e)
				if i%100 == 0 {
					if err := s.History.Save(); err != nil {
						b.Fatal(err)
					}
				}
			}
			v, err := s.History.OpenView()
			if err != nil {
				b.Fatal(err)
			}
			defer v.Close()
			for _, q := range []string{"独特", "needle独特关键词", "missing词", "文本"} {
				for _, mode := range []string{"scan", "indexed"} {
					b.Run(mode+"/"+q, func(b *testing.B) {
						b.ReportAllocs()
						b.ResetTimer()
						for b.Loop() {
							var p HistoryPage
							var err error
							if mode == "scan" {
								p, err = scanSearchPage(v.tx, q, "", 0, 50)
							} else {
								v.searches = nil
								p, err = v.Page(q, "", 0, 50)
							}
							if err != nil {
								b.Fatal(err)
							}
							if q == "missing词" && p.Total != 0 {
								b.Fatal("invalid benchmark result")
							}
						}
					})
				}
			}
		})
	}
}
