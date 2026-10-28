package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestAppExclusionsPersistCopiesAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "excluded-apps.json")
	a, err := openAppExclusions(path)
	if err != nil || a.List() == nil || len(a.List()) != 0 {
		t.Fatal("new rules must be an empty array", err)
	}
	for _, app := range []ExcludedApp{{"com.z.test", "Z"}, {"com.a.test", "A"}, {"com.z.test", "Renamed"}} {
		if err := a.Add(app); err != nil {
			t.Fatal(err)
		}
	}
	want := []ExcludedApp{{"com.a.test", "A"}, {"com.z.test", "Renamed"}}
	list := a.List()
	if !reflect.DeepEqual(list, want) || !a.Contains("com.z.test") || a.Contains("") {
		t.Fatal("incorrect rules", list)
	}
	list[0].BundleID = "mutated"
	reopened, err := openAppExclusions(path)
	if err != nil || !reflect.DeepEqual(a.List(), want) || !reflect.DeepEqual(reopened.List(), want) {
		t.Fatal("copy or restart changed rules", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("rules are not private", err)
	}
	if err := reopened.Remove("com.a.test"); err != nil || reopened.Contains("com.a.test") {
		t.Fatal("remove failed", err)
	}
	if err := reopened.Remove("com.z.test"); err != nil || reopened.List() == nil {
		t.Fatal("empty array became null", err)
	}
}

func TestAppExclusionsRejectInvalidRulesWithoutOverwriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "excluded-apps.json")
	invalid := []string{
		`{"version":2,"apps":[]}`, `{"version":1,"apps":null}`,
		`{"version":1,"apps":[{"bundleID":"","name":"App"}]}`,
		`{"version":1,"apps":[{"bundleID":"com.a","name":""}]}`,
		`{"version":1,"apps":[{"bundleID":"com.a","name":"A"},{"bundleID":"com.a","name":"B"}]}`,
		`{"version":1,"apps":[]} {}`, `{broken`,
	}
	for _, raw := range invalid {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openAppExclusions(path); err == nil {
			t.Fatalf("accepted invalid configuration %s", raw)
		}
		got, _ := os.ReadFile(path)
		if string(got) != raw {
			t.Fatal("invalid configuration rewritten")
		}
	}
	os.Remove(path)
	a, _ := openAppExclusions(path)
	for _, app := range []ExcludedApp{
		{"", "A"}, {"com.a/", "A"}, {"应用", "A"}, {strings.Repeat("a", 256), "A"},
		{"com.a", ""}, {"com.a", "  "}, {"com.a", strings.Repeat("a", 257)}, {"com.a", string([]byte{0xff})},
	} {
		if err := a.Add(app); err == nil || len(a.List()) != 0 {
			t.Fatal("accepted invalid app", app)
		}
	}
	for i := 0; i < 128; i++ {
		if err := a.Add(ExcludedApp{fmt.Sprintf("com.test.%d", i), "App"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Add(ExcludedApp{"com.extra", "Extra"}); err == nil {
		t.Fatal("rule limit ignored")
	}
	if err := a.Add(ExcludedApp{"com.test.0", "Rename at limit"}); err != nil {
		t.Fatal("rename at limit refused", err)
	}
}

func TestAppExclusionsWriteFailureHonorsPublication(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(fmt.Sprint(replaced), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "excluded-apps.json")
			a, _ := openAppExclusions(path)
			a.Add(ExcludedApp{"com.old", "Old"})
			injected := errors.New("injected write failure")
			a.write = func(path string, v any) error {
				if !replaced {
					return injected
				}
				err := writePrivateJSONWith(path, v, jsonWriteOps{
					syncFile: func(f *os.File) error { return f.Sync() }, rename: os.Rename,
					syncDir: func(string) error { return injected },
				})
				return err
			}
			if err := a.Add(ExcludedApp{"com.new", "New"}); !errors.Is(err, injected) {
				t.Fatal("write failure hidden", err)
			}
			reopened, err := openAppExclusions(path)
			if err != nil || a.Contains("com.new") != replaced || !reflect.DeepEqual(a.List(), reopened.List()) {
				t.Fatal("memory diverged from published rules", err)
			}
			if err := a.Remove("com.old"); !errors.Is(err, injected) || a.Contains("com.old") == replaced {
				t.Fatal("remove ignored publication boundary", err)
			}
		})
	}
}

func TestAppExclusionsStorageMigrationAndClearPreserveRules(t *testing.T) {
	dir := t.TempDir()
	if err := writePrivateJSON(filepath.Join(dir, "history.json"), archive{Version: 1}); err != nil {
		t.Fatal(err)
	}
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	app := ExcludedApp{"com.example.private", "Private"}
	if err := s.Exclusions.Add(app); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	s.History.Clear()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !reflect.DeepEqual(s.Exclusions.List(), []ExcludedApp{app}) {
		t.Fatal("migration or clear erased rules")
	}
	s.Close()
	bad := []byte(`{"version":99,"apps":[]}`)
	os.WriteFile(filepath.Join(dir, "excluded-apps.json"), bad, 0600)
	if _, err := openStorage(dir); err == nil {
		t.Fatal("broken rules silently resumed recording")
	}
}

func TestAppExclusionsConcurrentReadsAndEdits(t *testing.T) {
	a, _ := openAppExclusions(filepath.Join(t.TempDir(), "excluded-apps.json"))
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			for j := 0; j < 10; j++ {
				a.List()
				a.Contains("com.test")
				if err := a.Add(ExcludedApp{"com.test", "App"}); err != nil {
					t.Error(err)
				}
				if err := a.Remove("com.test"); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
}

func TestAppExclusionsBrokenPathStopsBeforeOpeningHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "excluded-apps.json")
	if err := os.Symlink(filepath.Join(dir, "missing-config"), path); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if s, err := openStorage(dir); err == nil {
		s.Close()
		t.Fatal("broken configuration path silently resumed recording")
	}
	if _, err := os.Stat(filepath.Join(dir, databaseName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("history opened before validating configuration", err)
	}
}

func TestAppExclusionsInvalidEncodedUTF8StopsStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "excluded-apps.json")
	raw := append([]byte(`{"version":1,"apps":[{"bundleID":"com.private","name":"`), 0xff)
	raw = append(raw, []byte(`"}]}`)...)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := openStorage(dir); err == nil {
		s.Close()
		t.Fatal("invalid UTF-8 was replaced and rules silently accepted")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, raw) {
		t.Fatal("invalid configuration was rewritten")
	}
}
