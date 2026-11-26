package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func historyTitles(t *testing.T, h *History) []string {
	t.Helper()
	v, err := h.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	page, err := v.Page("", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	titles := []string{}
	for _, e := range page.Entries {
		titles = append(titles, e.Title)
	}
	return titles
}

func TestRetentionMaxEntriesKeepsTheNewestAndPersists(t *testing.T) {
	dir := t.TempDir()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if p := s.Status().Retention; p.MaxEntries != 0 {
		t.Fatal("new storage limits the count", p)
	}
	phrase, err := s.Phrases.Save("", "永久常用语")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		s.History.Add(entry(t, fmt.Sprintf("第 %d 条", i)))
	}
	if err := s.History.SetRetention(RetentionPolicy{MaxEntries: 3}); err != nil {
		t.Fatal(err)
	}
	if got := historyTitles(t, s.History); !reflect.DeepEqual(got, []string{"第 4 条", "第 3 条", "第 2 条"}) {
		t.Fatal("count limit did not keep the newest", got)
	}
	if _, err := s.Phrases.Get(phrase.ID); err != nil {
		t.Fatal("count limit removed a phrase", err)
	}
	// Later copies keep to the limit as they are saved.
	s.History.Add(entry(t, "第 5 条"))
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	if got := historyTitles(t, s.History); !reflect.DeepEqual(got, []string{"第 5 条", "第 4 条", "第 3 条"}) {
		t.Fatal("a new copy went past the limit", got)
	}
	for _, bad := range []int{-1, maxRetainedEntries + 1} {
		if err := s.History.SetRetention(RetentionPolicy{MaxEntries: bad}); err == nil {
			t.Fatal("invalid count accepted", bad)
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
	if p := s.Status().Retention; p != (RetentionPolicy{MaxEntries: 3}) {
		t.Fatal("count limit did not persist", p)
	}
}

func TestRetentionCountColumnOnUpgradedDatabases(t *testing.T) {
	path := filepath.Join(t.TempDir(), databaseName)
	s, err := openSQLite(path, true)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatal("new database is not the current version", version, err)
	}
	// Reduce the database to the actual v5 schema, then reopen it.
	if _, err := s.db.Exec(dropEntryPins + "UPDATE settings SET max_mib=64; ALTER TABLE settings DROP COLUMN max_entries; PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	s, err = openSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatal("v5 database not upgraded", version, err)
	}
	if p := *s.policy.Load(); p != (RetentionPolicy{MaxMiB: 64}) {
		t.Fatal("upgrade changed the policy", p)
	}
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion+1)); err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	if s, err := openSQLite(path, false); err == nil {
		s.db.Close()
		t.Fatal("opened a database from a newer version")
	}
}
