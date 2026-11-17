package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func maskedEntry(t *testing.T, text string) Entry {
	t.Helper()
	e := entry(t, text)
	if !e.mask() {
		t.Fatalf("%q holds no secret", text)
	}
	return e
}

func TestSecretColumnOnNewAndUpgradedDatabases(t *testing.T) {
	path := filepath.Join(t.TempDir(), databaseName)
	s, err := openSQLite(path, true)
	if err != nil {
		t.Fatal(err)
	}
	plain, secret := entry(t, "plain text"), maskedEntry(t, "key "+testAWSKey)
	if err := s.transaction(func(tx *sql.Tx) error { return saveHistorySQL(tx, archive{1, false, []Entry{plain, secret}}) }); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatal("new database is not version 5", version, err)
	}
	var label string
	if err := s.db.QueryRow("SELECT secret FROM history WHERE id=?", secret.ID).Scan(&label); err != nil || label != "AWS 访问密钥" {
		t.Fatal("secret type not stored", label, err)
	}
	// Reduce the database to the actual v4 schema, then reopen it.
	if _, err := s.db.Exec("ALTER TABLE history DROP COLUMN secret; PRAGMA user_version=4"); err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	s, err = openSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatal("v4 database not upgraded", version, err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM history WHERE secret=''").Scan(&n); err != nil || n != 2 {
		t.Fatal("existing entries are not plain after upgrade", n, err)
	}
	s.db.Close()
	s, _ = openSQLite(path, false)
	if _, err := s.db.Exec("PRAGMA user_version=6"); err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	if s, err := openSQLite(path, false); err == nil {
		s.db.Close()
		t.Fatal("opened a database from a newer version")
	}
}

func TestMaskedEntriesNeverExposeTheSecret(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	text := "export AWS_ACCESS_KEY_ID=" + testAWSKey + "\nexport REGION=us-east-1"
	secret := maskedEntry(t, text)
	s.History.Add(entry(t, "something else"))
	s.History.Add(secret)
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	hidden := strings.ToLower(testAWSKey[4:16]) // the masked part of the key
	var title, excerpt, prefix string
	err = s.History.db.db.QueryRow("SELECT h.title,h.excerpt,c.search_prefix FROM history h JOIN history_content c ON c.id=h.id WHERE h.id=?",
		secret.ID).Scan(&title, &excerpt, &prefix)
	if err != nil {
		t.Fatal(err)
	}
	for name, field := range map[string]string{"title": title, "excerpt": excerpt, "search prefix": prefix} {
		if strings.Contains(strings.ToLower(field), hidden) {
			t.Errorf("stored %s keeps the key: %q", name, field)
		}
	}

	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	page, err := v.Page("", "", 0, 50)
	if err != nil || len(page.Entries) != 2 {
		t.Fatal(page, err)
	}
	if got := page.Entries[0]; got.ID != secret.ID || got.Secret != "AWS 访问密钥" || got.Title != secret.Title || got.Masked != "" {
		t.Fatalf("page entry %+v", got)
	}
	for q, want := range map[string]int{hidden: 0, hidden[:3]: 0, "region=us-east": 1, "akia••••mple": 1} {
		p, err := v.Page(q, "", 0, 50)
		if err != nil || p.Total != want {
			t.Errorf("search %q found %d, want %d (%v)", q, p.Total, want, err)
		}
		if len(p.Entries) > 0 && p.Entries[0].Secret == "" {
			t.Errorf("search %q dropped the secret type", q)
		}
	}
	d, err := v.Detail(secret.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Text != text || d.Secret != "AWS 访问密钥" || d.Title != secret.Title || d.Excerpt != secret.Excerpt ||
		d.Masked != "export AWS_ACCESS_KEY_ID=AKIA••••MPLE\nexport REGION=us-east-1" {
		t.Fatalf("detail %+v", d)
	}
}

func TestRecopyUpdatesMaskState(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	text := "token " + testGitHub
	hidden := strings.ToLower(testGitHub[4:20])
	search := func(q string) HistoryPage {
		t.Helper()
		v, err := s.History.OpenView()
		if err != nil {
			t.Fatal(err)
		}
		defer v.Close()
		p, err := v.Page(q, "", 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	s.History.Add(entry(t, text)) // recorded while recognition was off
	if p := search(hidden); p.Total != 1 || p.Entries[0].Secret != "" {
		t.Fatal("plain copy should be searchable", p)
	}
	s.History.Add(maskedEntry(t, text))
	if p := search(hidden); p.Total != 0 {
		t.Fatal("masking a re-copy left the key searchable", p)
	}
	if p := search("token ghp_"); p.Total != 1 || p.Entries[0].Secret != "GitHub 令牌" || strings.Contains(strings.ToLower(p.Entries[0].Title), hidden) {
		t.Fatal("re-copy did not update the stored mask", p)
	}
	s.History.Add(entry(t, text))
	if p := search(hidden); p.Total != 1 || p.Entries[0].Secret != "" || p.Entries[0].Title != text {
		t.Fatal("unmasking a re-copy did not restore it", p)
	}
}

func TestJSONHistoryRemasksOnReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	h, err := openHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	secret := maskedEntry(t, "key "+testAWSKey)
	h.Add(entry(t, "plain"))
	h.Add(secret)
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	h, err = openHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := h.Snapshot()
	if len(entries) != 2 || entries[0].Secret != "AWS 访问密钥" || entries[0].Title != secret.Title || entries[1].Secret != "" {
		t.Fatal("reload lost the mask", entries)
	}
	v, err := h.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if p, _ := v.Page(strings.ToLower(testAWSKey[4:16]), "", 0, 50); p.Total != 0 {
		t.Fatal("reloaded secret is searchable", p)
	}
	if d, err := v.Detail(secret.ID); err != nil || d.Masked != "key AKIA••••MPLE" || d.Text != secret.Text {
		t.Fatal("legacy detail", d, err)
	}
}

func TestRemaskHidesSecretsTheRulesNoLongerRecognize(t *testing.T) {
	e := entry(t, "a value that looks ordinary")
	e.remask("API 密钥")
	if e.Secret != "API 密钥" || e.Masked != maskDots || e.Title != maskDots || strings.Contains(e.searchText, "ordinary") {
		t.Fatalf("%+v", e)
	}
	plain := entry(t, "plain")
	before := plain
	if plain.remask(""); plain.Title != before.Title || plain.Secret != "" {
		t.Fatalf("%+v", plain)
	}
}

func TestMigrationKeepsMaskedEntries(t *testing.T) {
	dir := t.TempDir()
	h, err := openHistory(filepath.Join(dir, "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	secret := maskedEntry(t, "key "+testAWSKey)
	h.Add(entry(t, "plain"))
	h.Add(secret)
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Migrate(); err != nil {
		t.Fatal("migration rejected a masked entry", err)
	}
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if p, _ := v.Page("", "", 0, 50); len(p.Entries) != 2 || p.Entries[0].Secret != "AWS 访问密钥" || p.Entries[0].Title != secret.Title {
		t.Fatal("migration lost the mask", p)
	}
	if p, _ := v.Page(strings.ToLower(testAWSKey[4:16]), "", 0, 50); p.Total != 0 {
		t.Fatal("migrated secret is searchable", p)
	}
}
