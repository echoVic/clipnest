package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func storedSnapshot(t *testing.T, h *History) ([]Entry, bool) {
	t.Helper()
	if h.db == nil {
		return h.Snapshot()
	}
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	a, _, err := h.db.load()
	if err != nil {
		t.Fatal(err)
	}
	return a.Entries, a.Paused
}

func TestSQLiteNewInstallationAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Status(); got.Backend != "sqlite" || got.MigrationAvailable {
		t.Fatalf("new installation should use SQLite: %+v", got)
	}
	a := entry(t, "# 原样保存\n\n**Markdown**\t\x00")
	s.History.Add(a)
	s.History.Pause(true)
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	p, err := s.Phrases.Save("", "  常用语\n原文  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, paused := storedSnapshot(t, reopened.History)
	if !paused || !reflect.DeepEqual(got, []Entry{a}) {
		t.Fatal("history or pause state changed on restart")
	}
	if got, err := reopened.Phrases.Get(p.ID); err != nil || got.Text != p.Text {
		t.Fatal("phrase lost on restart", err)
	}
	for _, name := range []string{"history.json", "phrases.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("new installations should not create %s", name)
		}
	}
	info, err := os.Stat(filepath.Join(dir, "clipnest.sqlite3"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("database must be private", err)
	}
}

func seedLegacyStorage(t *testing.T, dir string) (archive, []Phrase) {
	t.Helper()
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 12, 8))); err != nil {
		t.Fatal(err)
	}
	im, err := newEntry("", pngData.Bytes(), "截图")
	if err != nil {
		t.Fatal(err)
	}
	a := archive{1, false, []Entry{entry(t, "# 标题\n\n**保留原文**\r\n\t"), im, entry(t, "older")}}
	ps := []Phrase{{ID: "second", Text: "  第二条\n"}, {ID: "first", Text: "# 第一条"}}
	if err := writePrivateJSON(filepath.Join(dir, "history.json"), a); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateJSON(filepath.Join(dir, "phrases.json"), phraseArchive{1, ps}); err != nil {
		t.Fatal(err)
	}
	return a, ps
}

func TestLegacyStorageRequiresExplicitMigration(t *testing.T) {
	dir := t.TempDir()
	a, ps := seedLegacyStorage(t, dir)
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if status := s.Status(); status.Backend != "json" || !status.MigrationAvailable {
		t.Fatalf("upgrade did not offer optional migration: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(dir, databaseName)); !os.IsNotExist(err) {
		t.Fatal("legacy data migrated without consent")
	}
	frozen, _ := storedSnapshot(t, s.History)
	if !reflect.DeepEqual(frozen, a.Entries) || !reflect.DeepEqual(s.Phrases.items, ps) {
		t.Fatal("legacy state changed before migration")
	}
	// Include copies not yet flushed by the two-second timer and phrase edits
	// made after startup. Migration must use the current locked state.
	s.History.Add(entry(t, "刚复制、还未定时保存"))
	s.History.Remove(a.Entries[2].ID)
	s.History.Pause(true)
	if err := s.Phrases.Move(ps[1].ID, -1); err != nil {
		t.Fatal(err)
	}
	want, _ := storedSnapshot(t, s.History)
	wantPhrases := append([]Phrase{}, s.Phrases.items...)
	status, err := s.Migrate()
	if err != nil || status.Backend != "sqlite" || status.MigrationAvailable || status.CleanupPending {
		t.Fatalf("migration failed: %+v %v", status, err)
	}
	if !reflect.DeepEqual(frozen, a.Entries) {
		t.Fatal("open panel snapshot changed during migration")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, paused := storedSnapshot(t, reopened.History)
	if !paused || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(reopened.Phrases.items, wantPhrases) {
		t.Fatal("migration lost content, order, IDs, timestamps or pause state")
	}
	var imageType string
	if err := reopened.History.db.db.QueryRow("SELECT typeof(image) FROM history_content WHERE id = ?", a.Entries[1].ID).Scan(&imageType); err != nil || imageType != "blob" {
		t.Fatal("image is not stored as binary", imageType, err)
	}
	for _, name := range []string{"history.json", "phrases.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("verified migration left stale private content in %s", name)
		}
	}
	if _, err := reopened.Migrate(); err != nil {
		t.Fatal("repeated migration must be harmless", err)
	}
}

func TestMigrationFailurePreservesLegacyAndCanRetry(t *testing.T) {
	dir := t.TempDir()
	seedLegacyStorage(t, dir)
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before, _ := os.ReadFile(filepath.Join(dir, "history.json"))
	// A blocked destination simulates a failure before publishing the DB.
	os.Mkdir(filepath.Join(dir, databaseName), 0700)
	if _, err := s.Migrate(); err == nil {
		t.Fatal("migration reported success without installing the database")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "history.json"))
	if !bytes.Equal(before, after) || s.Status().Backend != "json" {
		t.Fatal("failed migration changed the active storage")
	}
	s.History.Add(entry(t, "仍可使用"))
	if err := s.History.Save(); err != nil {
		t.Fatal("legacy storage unusable after failure", err)
	}
	os.Remove(filepath.Join(dir, databaseName))
	if _, err := s.Migrate(); err != nil {
		t.Fatal("retry failed", err)
	}
}

func TestMigrationRejectsBrokenPhrasesAndDatabaseNeverFallsBack(t *testing.T) {
	dir := t.TempDir()
	seedLegacyStorage(t, dir)
	path := filepath.Join(dir, "phrases.json")
	os.WriteFile(path, []byte("broken"), 0600)
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(); err == nil {
		t.Fatal("broken phrases silently dropped during migration")
	}
	s.Close()
	if got, _ := os.ReadFile(path); string(got) != "broken" {
		t.Fatal("broken original overwritten")
	}
	// An existing database is authoritative, even when legacy JSON exists.
	// Falling back here would resurrect deleted records from an old archive.
	os.WriteFile(filepath.Join(dir, databaseName), []byte("broken sqlite"), 0600)
	if s, err := openStorage(dir); err == nil {
		s.Close()
		t.Fatal("corrupt database silently fell back to JSON")
	}
}

func TestSQLiteIncrementalWritesAndRollback(t *testing.T) {
	dir := t.TempDir()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 32, 16))); err != nil {
		t.Fatal(err)
	}
	im, err := newEntry("", pngData.Bytes(), "image")
	if err != nil {
		t.Fatal(err)
	}
	s.History.Add(im)
	a := entry(t, "immutable payload")
	s.History.Add(a)
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	// Updating existing payloads (including BLOBs), even to identical values,
	// fails. Metadata-only reordering must not trip this trigger.
	db := s.History.db.db
	_, err = db.Exec(`CREATE TRIGGER immutable_payload BEFORE UPDATE ON history_content
		BEGIN SELECT RAISE(ABORT, 'payload rewritten'); END;
		CREATE TRIGGER reject_bad_phrase BEFORE INSERT ON phrases
		WHEN NEW.text = 'rejected' BEGIN SELECT RAISE(ABORT, 'injected write failure'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	b := entry(t, "new payload")
	s.History.Add(b)
	a.Source = "new source"
	a.Created++
	a.searchText = prepareSearch(a)
	s.History.Add(a)
	if err := s.History.Save(); err != nil {
		t.Fatal("existing payload rewritten", err)
	}
	p, err := s.Phrases.Save("", "kept")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Phrases.Save("", "rejected"); err == nil {
		t.Fatal("injected write failure hidden")
	}
	if got := phraseTexts(t, s.Phrases); !reflect.DeepEqual(got, []string{"kept"}) {
		t.Fatal("failed phrase commit changed memory", got)
	}
	disk, ps, err := s.History.db.load()
	if err != nil || !reflect.DeepEqual(disk.Entries, []Entry{a, b, im}) || !reflect.DeepEqual(ps, []Phrase{p}) {
		t.Fatal("rollback or incremental write changed persisted state", err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_delete BEFORE DELETE ON history
		BEGIN SELECT RAISE(ABORT, 'injected deletion failure'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	s.History.Clear()
	if err := s.History.Save(); err == nil {
		t.Fatal("failed deletion was not reported")
	}
	db.Exec("DROP TRIGGER fail_delete")
	if err := s.History.Save(); err != nil {
		t.Fatal("failed save did not retry the newest state", err)
	}
	if a, _, err := s.History.db.load(); err != nil || len(a.Entries) != 0 {
		t.Fatal("deleted history returned", err)
	}
	var remaining int
	if err := db.QueryRow("SELECT count(*) FROM history_content").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("clearing history left orphaned private content", err)
	}
}

func TestMigrationConcurrentWritersKeepFinalState(t *testing.T) {
	dir := t.TempDir()
	_, ps := seedLegacyStorage(t, dir)
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for n := 0; n < 3; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				switch n {
				case 0:
					if _, err := s.Migrate(); err != nil {
						t.Error(err)
					}
				case 1:
					s.History.Clear()
					if err := s.History.Save(); err != nil {
						t.Error(err)
					}
				case 2:
					if _, err := s.Phrases.Save(ps[0].ID, "latest phrase"); err != nil {
						t.Error(err)
					}
				}
			}
		}(n)
	}
	wg.Wait()
	s.Close()
	reopened, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, _ := storedSnapshot(t, reopened.History)
	p, err := reopened.Phrases.Get(ps[0].ID)
	if len(got) != 0 || err != nil || p.Text != "latest phrase" {
		t.Fatal("concurrent save resurrected history or lost a phrase edit", err)
	}
}

func TestSQLiteCleanupRetryNeverReimportsLegacy(t *testing.T) {
	dir := t.TempDir()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	seedLegacyStorage(t, dir)
	s, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, _ := storedSnapshot(t, s.History); len(got) != 0 || !s.Status().CleanupPending {
		t.Fatal("old JSON was re-imported or cleanup not offered")
	}
	os.Remove(filepath.Join(dir, "phrases.json"))
	os.Mkdir(filepath.Join(dir, "phrases.json"), 0700)
	if status, err := s.Migrate(); err == nil || status.Backend != "sqlite" || !status.CleanupPending {
		t.Fatal("cleanup failure silently hidden or rolled back storage")
	}
	if _, err := s.Phrases.Save("", "SQLite remains usable"); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "phrases.json"))
	if status, err := s.Migrate(); err != nil || status.CleanupPending {
		t.Fatal("cleanup retry failed", err)
	}
}

func TestStorageStatusBinding(t *testing.T) {
	dir := t.TempDir()
	seedLegacyStorage(t, dir)
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d := &Desktop{storage: s, h: s.History, p: s.Phrases}
	if status := d.StorageStatus(); status.Backend != "json" || !status.MigrationAvailable {
		t.Fatal("upgrade status is not exposed to the UI", status)
	}
	if _, err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if status := d.StorageStatus(); status.Backend != "sqlite" || status.MigrationAvailable {
		t.Fatal("completed migration still prompts", status)
	}
}

func TestSQLiteRejectsOrphanedContent(t *testing.T) {
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.History.db.db.Exec(`PRAGMA foreign_keys = OFF;
		INSERT INTO history_content (id, text) VALUES ('orphan', 'private content');
		PRAGMA foreign_keys = ON;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.History.db.load(); err == nil {
		t.Fatal("orphaned content was silently accepted")
	}
}

func TestMigrationVerificationFailureKeepsOriginals(t *testing.T) {
	dir := t.TempDir()
	seedLegacyStorage(t, dir)
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before, _ := os.ReadFile(filepath.Join(dir, "history.json"))
	// A representation discrepancy must be caught after staging, before rename.
	s.History.entries[0].Title = "inconsistent metadata"
	if _, err := s.Migrate(); err == nil {
		t.Fatal("verification mismatch accepted")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "history.json"))
	if !bytes.Equal(before, after) || s.Status().Backend != "json" {
		t.Fatal("verification failure changed original state")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 2 {
		t.Fatal("failed migration leaked a database or staging directory", err)
	}
}

func TestMigrationWithOnlyOneLegacyArchive(t *testing.T) {
	for _, present := range []string{"history.json", "phrases.json"} {
		t.Run(present, func(t *testing.T) {
			dir := t.TempDir()
			a, ps := seedLegacyStorage(t, dir)
			missing := "history.json"
			if present == missing {
				missing = "phrases.json"
			}
			if err := os.Remove(filepath.Join(dir, missing)); err != nil {
				t.Fatal(err)
			}
			s, err := openStorage(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if !s.Status().MigrationAvailable {
				t.Fatal("legacy file not detected")
			}
			if _, err := s.Migrate(); err != nil {
				t.Fatal(err)
			}
			got, gotPhrases, err := s.History.db.load()
			if err != nil {
				t.Fatal(err)
			}
			if present == "history.json" && (!reflect.DeepEqual(got, a) || len(gotPhrases) != 0) {
				t.Fatal("history-only migration changed data")
			}
			if present == "phrases.json" && (len(got.Entries) != 0 || !reflect.DeepEqual(gotPhrases, ps)) {
				t.Fatal("phrases-only migration changed data")
			}
		})
	}
}

// Optional compatibility check against a directory of real v1 archives.
// Copy first: this test never opens a writable store in the fixture directory.
func TestMigrationArchiveFixture(t *testing.T) {
	source := os.Getenv("CLIPNEST_MIGRATION_FIXTURE_DIR")
	if source == "" {
		t.Skip("set CLIPNEST_MIGRATION_FIXTURE_DIR to check existing v1 archives")
	}
	dir := t.TempDir()
	for _, name := range []string{"history.json", "phrases.json"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	history, paused := storedSnapshot(t, s.History)
	phrases := append([]Phrase{}, s.Phrases.items...)
	if _, err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, gotPaused := storedSnapshot(t, s.History)
	if !reflect.DeepEqual(got, history) || gotPaused != paused || !reflect.DeepEqual(s.Phrases.items, phrases) {
		t.Fatal("real archive did not survive migration and restart")
	}
	t.Logf("verified %d history entries and %d phrases; source files untouched", len(got), len(phrases))
}

func TestSQLiteFileURI(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"unix absolute", "/tmp/clipnest/clipnest.sqlite3", "file:///tmp/clipnest/clipnest.sqlite3"},
		{"unix space", "/tmp/app data/clipnest.sqlite3", "file:///tmp/app%20data/clipnest.sqlite3"},
		{"windows drive", `C:\Users\cocoon\AppData\Roaming\Clipnest\clipnest.sqlite3`, "file:///C:/Users/cocoon/AppData/Roaming/Clipnest/clipnest.sqlite3"},
		{"windows staging", `C:\Users\a b\.clipnest-migration-1\clipnest.sqlite3`, "file:///C:/Users/a%20b/.clipnest-migration-1/clipnest.sqlite3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sqliteFileURI(tc.path); got != tc.want {
				t.Fatalf("sqliteFileURI(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}
