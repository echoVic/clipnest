package main

import (
	"strings"
	"testing"
	"time"
)

// transferStorage is an empty SQLite storage.
func transferStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func addEntries(t *testing.T, s *Storage, entries ...Entry) {
	t.Helper()
	for _, e := range entries {
		if err := s.History.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestSensitiveEntries(t *testing.T) {
	for _, c := range []struct {
		kind, text, secret string
		want               bool
	}{
		{"text", "普通文字", "", false},
		{"text", "AWS=" + testAWSKey, "", true}, // recorded while privacy was off
		{"text", "规则后来认不出了", "AWS 访问密钥", true},
		{"image", "", "", false},
	} {
		if got := sensitive(c.kind, c.text, c.secret); got != c.want {
			t.Errorf("sensitive(%q, %q, %q) = %v", c.kind, c.text, c.secret, got)
		}
	}
}

func TestValidImageName(t *testing.T) {
	id := strings.Repeat("a1", 32)
	for name, want := range map[string]bool{
		"images/" + id + ".png":                  true,
		"images/" + strings.ToUpper(id) + ".png": false,
		"images/" + id[:63] + ".png":             false,
		"images/../" + id[:61] + ".png":          false,
		id + ".png":                              false,
		"images/" + id + ".jpg":                  false,
	} {
		if validImageName(name) != want {
			t.Errorf("validImageName(%q) != %v", name, want)
		}
	}
}

func TestExportSnapshotDoesNotHoldUpWrites(t *testing.T) {
	s := transferStorage(t)
	addEntries(t, s, entry(t, "导出前"))
	panel, err := s.History.OpenView() // holds one of the storage's connections
	if err != nil {
		t.Fatal(err)
	}
	defer panel.Close()
	db, tx, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer tx.Rollback()
	late := entry(t, "导出时复制的")
	done := make(chan error, 1)
	go func() {
		s.History.Add(late)
		done <- s.History.Save()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a copy waited for the snapshot")
	}
	var n int
	if err := tx.QueryRow("SELECT count(*) FROM history").Scan(&n); err != nil || n != 1 {
		t.Fatal("the snapshot moved", n, err)
	}
}

func TestExportSummary(t *testing.T) {
	s := transferStorage(t)
	masked := entry(t, "AWS_SECRET="+testAWSKey)
	masked.mask()
	addEntries(t, s, masked, entry(t, "token "+testGitHub), entry(t, "普通文字"), imageEntry(t, 10))
	if _, err := s.Phrases.Save("", "常用语"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ExportSummary()
	if want := (ExportSummary{History: 4, Images: 1, Phrases: 1, Sensitive: 2}); err != nil || got != want {
		t.Fatal(got, err)
	}
}
