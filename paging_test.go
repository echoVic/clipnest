package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSQLiteUnboundedLazyPagesAndFrozenView(t *testing.T) {
	dir := t.TempDir()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 735; i++ {
		if err := s.History.Add(entry(t, fmt.Sprintf("原文 %04d %%_ 中文", i))); err != nil {
			t.Fatal(err)
		}
		if i%100 == 0 {
			if err := s.History.Save(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.History.entries) != 0 {
		t.Fatal("startup loaded full history")
	}
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	page, err := v.Page("", "all", 700, 50)
	if err != nil || page.Total != 735 || len(page.Entries) != 35 || page.Entries[0].Title != "原文 0034 %_ 中文" {
		t.Fatalf("bad final page: %+v %v", page, err)
	}
	for _, e := range page.Entries {
		if e.Text != "" || e.Image != nil || e.searchText != "" {
			t.Fatal("page leaked payload")
		}
	}
	old, err := v.Detail(page.Entries[0].ID)
	if err != nil || old.Text != "原文 0034 %_ 中文" {
		t.Fatal("lazy detail", err)
	}
	recopy := old
	recopy.Source = "changed"
	recopy.Created++
	s.History.Add(recopy)
	s.History.Remove(page.Entries[1].ID)
	if err := s.History.Save(); err != nil {
		t.Fatal("writer blocked by snapshot", err)
	}
	again, err := v.Page("%_ 中文", "", 700, 50)
	if err != nil || again.Total != 735 || again.Entries[0].ID != old.ID || again.Entries[0].Source != old.Source {
		t.Fatal("live writes shifted frozen selection", err)
	}
	if _, err := v.Detail(page.Entries[1].ID); err != nil {
		t.Fatal("snapshot lost removed payload", err)
	}
	v.Close()
	if _, err := v.Page("", "", 0, 50); err == nil {
		t.Fatal("closed view usable")
	}
	v, err = s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	first, err := v.Page("", "", 0, 50)
	if err != nil || first.Total != 734 || first.Entries[0].Source != "changed" {
		t.Fatal("new snapshot stale", err)
	}
	if _, err := v.Page("", "", -1, 50); err == nil {
		t.Fatal("negative offset")
	}
	if _, err := v.Page("", "", 0, 501); err == nil {
		t.Fatal("unbounded page")
	}
}

func TestRetentionPolicyAgeBudgetAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if p := s.Status().Retention; p.MaxMiB != 512 || p.KeepDays != 0 {
		t.Fatal("defaults", p)
	}
	phrase, err := s.Phrases.Save("", "永久常用语")
	if err != nil {
		t.Fatal(err)
	}
	old := entry(t, "过期")
	old.Created = time.Now().Add(-72 * time.Hour).UnixMilli()
	s.History.Add(old)
	for i := 0; i < 4; i++ {
		s.History.Add(entry(t, fmt.Sprintf("%d%s", i, strings.Repeat("x", 400<<10))))
	}
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := s.History.SetRetention(RetentionPolicy{MaxMiB: 1, KeepDays: 1}); err != nil {
		t.Fatal(err)
	}
	frozen, err := v.Page("", "", 0, 50)
	if err != nil || frozen.Total != 5 {
		t.Fatal("cleanup changed frozen list", err)
	}
	if _, err := v.Detail(old.ID); err != nil {
		t.Fatal("cleanup changed frozen detail", err)
	}
	v.Close()
	latest, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	got, err := latest.Page("", "", 0, 50)
	latest.Close()
	if err != nil || got.Total != 2 || !strings.HasPrefix(got.Entries[0].Title, "3") {
		t.Fatal("oldest-first budget failed", err)
	}
	if _, err := s.Phrases.Get(phrase.ID); err != nil {
		t.Fatal("policy removed phrase", err)
	}
	if err := s.History.SetRetention(RetentionPolicy{MaxMiB: -1}); err == nil {
		t.Fatal("invalid policy accepted")
	}
	if s.Status().Retention.MaxMiB != 1 {
		t.Fatal("failed policy changed settings")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Status().Retention != (RetentionPolicy{1, 1}) {
		t.Fatal("policy did not persist")
	}
	if err := s.History.SetRetention(RetentionPolicy{}); err != nil {
		t.Fatal("unlimited rejected", err)
	}
}

func TestSQLiteV1Upgrade(t *testing.T) {
	dir := t.TempDir()
	db, err := openSQLite(filepath.Join(dir, databaseName), true)
	if err != nil {
		t.Fatal(err)
	}
	// Build an actual v1 schema to exercise upgrade rather than changing only
	// the version number on a current database.
	_, err = db.db.Exec(`DROP TRIGGER history_search_delete; DROP TABLE history_search;
		DROP TABLE history_content; DROP TABLE history; DROP TABLE settings;
		CREATE TABLE history(id TEXT PRIMARY KEY,position INTEGER NOT NULL,source TEXT NOT NULL,created INTEGER NOT NULL);
		CREATE TABLE history_content(id TEXT PRIMARY KEY REFERENCES history(id) ON DELETE CASCADE,text TEXT NOT NULL,image BLOB);
		CREATE TABLE settings(id INTEGER PRIMARY KEY CHECK(id=1),paused INTEGER NOT NULL);
		INSERT INTO settings VALUES(1,1); PRAGMA user_version=1;`)
	if err != nil {
		t.Fatal(err)
	}
	e := entry(t, "Ünicode 中文 %_")
	_, err = db.db.Exec("INSERT INTO history VALUES(?,?,?,?)", e.ID, 0, e.Source, e.Created)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.db.Exec("INSERT INTO history_content VALUES(?,?,?)", e.ID, e.Text, e.Image)
	if err != nil {
		t.Fatal(err)
	}
	db.db.Close()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	p, err := v.Page("ünicode 中文 %_", "", 0, 50)
	if err != nil || p.Total != 1 || !s.History.IsPaused() {
		t.Fatal("upgrade changed data/search", err)
	}
}

func TestSQLitePendingWritesSurviveConcurrentSaves(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if err := s.History.Add(entry(t, fmt.Sprintf("worker %d entry %d", worker, i))); err != nil {
					t.Error(err)
				}
				if i%9 == 0 {
					if err := s.History.Save(); err != nil {
						t.Error(err)
					}
				}
			}
		}(worker)
	}
	wg.Wait()
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	p, err := v.Page("", "", 0, 50)
	if err != nil || p.Total != 400 {
		t.Fatalf("lost queued writes: %d %v", p.Total, err)
	}
	if s.History.pendingBytes != 0 || len(s.History.pending) != 0 {
		t.Fatal("committed payloads retained")
	}
}

func TestSQLiteLazyValidationAndCanceledSnapshot(t *testing.T) {
	dir := t.TempDir()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := entry(t, "original")
	s.History.Add(e)
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.History.db.db.Exec("UPDATE history_content SET text='corrupted' WHERE id=?", e.ID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = openStorage(dir)
	if err != nil {
		t.Fatal("startup decoded history payloads", err)
	}
	defer s.Close()
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	p, err := v.Page("", "", 0, 50)
	if err != nil || p.Total != 1 {
		t.Fatal("summary read requires payload", err)
	}
	if _, err := v.Detail(e.ID); err == nil {
		t.Fatal("corrupted content was delivered")
	}
	v.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if v, err := s.History.openView(ctx); err == nil {
		v.Close()
		t.Fatal("canceled open succeeded")
	}
	var busy, log, checkpointed int
	if err := s.History.db.db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed); err != nil || busy != 0 {
		t.Fatal("closed view retained WAL reader", err)
	}
}

func TestRetentionFailureRollsBackSettingsAndDeletion(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := entry(t, "keep on failure")
	e.Created = time.Now().Add(-72 * time.Hour).UnixMilli()
	s.History.Add(e)
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	_, err = s.History.db.db.Exec(`CREATE TRIGGER reject_cleanup BEFORE DELETE ON history
		BEGIN SELECT RAISE(ABORT,'disk failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.History.SetRetention(RetentionPolicy{0, 1}); err == nil {
		t.Fatal("cleanup failure hidden")
	}
	if s.Status().Retention != (RetentionPolicy{512, 0}) {
		t.Fatal("memory policy changed on failure")
	}
	var days, count int
	if err := s.History.db.db.QueryRow("SELECT keep_days FROM settings").Scan(&days); err != nil || days != 0 {
		t.Fatal("settings not rolled back", err)
	}
	if err := s.History.db.db.QueryRow("SELECT count(*) FROM history").Scan(&count); err != nil || count != 1 {
		t.Fatal("content not rolled back", err)
	}
}

func TestRelaxingRetentionIncludesPendingCopies(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.History.SetRetention(RetentionPolicy{1, 0}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s.History.Add(entry(t, fmt.Sprintf("%d%s", i, strings.Repeat("x", 400<<10))))
	}
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	s.History.Add(entry(t, "pending"+strings.Repeat("x", 400<<10)))
	if err := s.History.SetRetention(RetentionPolicy{}); err != nil {
		t.Fatal(err)
	}
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	p, err := v.Page("", "", 0, 50)
	if err != nil || p.Total != 3 {
		t.Fatalf("relaxing policy deleted under old limit: %d %v", p.Total, err)
	}
}

func TestSQLiteWritersDoNotInvalidateEachOthersSnapshot(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db := s.History.db
	read := make(chan struct{})
	secondStarted := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		done <- db.transaction(func(tx *sql.Tx) error {
			var count int
			if err := tx.QueryRow("SELECT count(*) FROM history").Scan(&count); err != nil {
				return err
			}
			close(read)
			<-secondStarted
			// Give the second transaction time to commit between this read and
			// write. Without writer serialization the WAL snapshot is stale.
			time.Sleep(30 * time.Millisecond)
			_, err := tx.Exec("INSERT INTO phrases VALUES('first',0,'first')")
			return err
		})
	}()
	<-read
	go func() {
		close(secondStarted)
		done <- db.transaction(func(tx *sql.Tx) error {
			_, err := tx.Exec("INSERT INTO phrases VALUES('second',1,'second')")
			return err
		})
	}()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Error("concurrent writer failed", err)
		}
	}
}
