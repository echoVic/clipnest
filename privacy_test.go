package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestPrivacyDefaultsAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "privacy.json")
	p, err := openPrivacy(path)
	if err != nil {
		t.Fatal(err)
	}
	want := PrivacySettings{Secrets: "mask", ClearSync: true, Ignore: []string{}}
	if got := p.Settings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults %+v, want %+v", got, want)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("defaults were written before any change", err)
	}
	if p.Ignored("anything") {
		t.Fatal("no rules ignore nothing")
	}

	next := PrivacySettings{Secrets: "skip", ClearSync: false, Ignore: []string{`^\d{6}$`, `(?i)internal only`}}
	if err := p.Save(next); err != nil {
		t.Fatal(err)
	}
	got := p.Settings()
	got.Ignore[0] = "mutated"
	reopened, err := openPrivacy(path)
	if err != nil || !reflect.DeepEqual(p.Settings(), next) || !reflect.DeepEqual(reopened.Settings(), next) {
		t.Fatal("copy or restart changed settings", err, reopened.Settings())
	}
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("settings are not private", err)
	}
	for text, ignored := range map[string]bool{
		"123456": true, "1234567": false, "This is INTERNAL ONLY.": true, "internal": false,
	} {
		if reopened.Ignored(text) != ignored {
			t.Errorf("Ignored(%q) = %v", text, !ignored)
		}
	}
	if err := reopened.Save(PrivacySettings{Secrets: "off", Ignore: nil}); err != nil || reopened.Settings().Ignore == nil {
		t.Fatal("nil rules must save as an empty list", err)
	}
}

func TestPrivacyRejectsInvalidSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "privacy.json")
	p, _ := openPrivacy(path)
	tooMany := make([]string, 65)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("rule%d", i)
	}
	for name, s := range map[string]PrivacySettings{
		"unknown mode":   {Secrets: "show", Ignore: []string{}},
		"empty mode":     {Secrets: "", Ignore: []string{}},
		"empty rule":     {Secrets: "mask", Ignore: []string{""}},
		"blank rule":     {Secrets: "mask", Ignore: []string{"  "}},
		"long rule":      {Secrets: "mask", Ignore: []string{strings.Repeat("a", 513)}},
		"duplicate":      {Secrets: "mask", Ignore: []string{"a", "a"}},
		"does not parse": {Secrets: "mask", Ignore: []string{"(unclosed"}},
		"matches empty":  {Secrets: "mask", Ignore: []string{"a*"}},
		"invalid utf-8":  {Secrets: "mask", Ignore: []string{string([]byte{0xff})}},
		"too many":       {Secrets: "mask", Ignore: tooMany},
	} {
		if err := p.Save(s); err == nil {
			t.Errorf("%s: accepted %+v", name, s)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid settings were written", err)
	}
	if err := p.Save(PrivacySettings{Secrets: "mask", Ignore: []string{strings.Repeat("a", 512)}}); err != nil {
		t.Fatal("512-byte rule refused", err)
	}
	if err := p.Save(PrivacySettings{Secrets: "mask", Ignore: tooMany[:64]}); err != nil {
		t.Fatal("64 rules refused", err)
	}
}

func TestPrivacyBrokenFileStopsStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "privacy.json")
	for _, raw := range []string{
		`{"version":2,"secrets":"mask","clearSync":true,"ignore":[]}`,
		`{"version":1,"secrets":"mask","clearSync":true,"ignore":null}`,
		`{"version":1,"secrets":"bogus","clearSync":true,"ignore":[]}`,
		`{"version":1,"secrets":"mask","clearSync":true,"ignore":["("]}`,
		`{"version":1,"secrets":"mask","clearSync":true,"ignore":[]} {}`,
		`{broken`,
		string(append([]byte(`{"version":1,"secrets":"mask","clearSync":true,"ignore":["`), 0xff, '"', ']', '}')),
	} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openPrivacy(path); err == nil {
			t.Errorf("accepted broken settings %q", raw)
		}
		if got, _ := os.ReadFile(path); string(got) != raw {
			t.Fatal("broken settings rewritten")
		}
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "privacy.json"), []byte(`{"version":99}`), 0600)
	if s, err := openStorage(dir); err == nil {
		s.Close()
		t.Fatal("broken privacy settings silently resumed recording")
	}
	if _, err := os.Stat(filepath.Join(dir, databaseName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("history opened before validating privacy settings", err)
	}
}

func TestPrivacyWriteFailureHonorsPublication(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(fmt.Sprint(replaced), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "privacy.json")
			p, _ := openPrivacy(path)
			injected := errors.New("injected write failure")
			p.write = func(path string, v any) error {
				if !replaced {
					return injected
				}
				return writePrivateJSONWith(path, v, jsonWriteOps{
					syncFile: func(f *os.File) error { return f.Sync() }, rename: os.Rename,
					syncDir: func(string) error { return injected },
				})
			}
			next := PrivacySettings{Secrets: "off", ClearSync: false, Ignore: []string{"secret"}}
			if err := p.Save(next); !errors.Is(err, injected) {
				t.Fatal("write failure hidden", err)
			}
			if reflect.DeepEqual(p.Settings(), next) != replaced || p.Ignored("secret") != replaced {
				t.Fatal("memory diverged from published settings", p.Settings())
			}
		})
	}
}

func TestStorageOpensPrivacySettings(t *testing.T) {
	dir := t.TempDir()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Privacy.Save(PrivacySettings{Secrets: "skip", ClearSync: true, Ignore: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	s.History.Clear()
	s.Close()
	s, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Privacy.Settings(); got.Secrets != "skip" || !s.Privacy.Ignored("x") {
		t.Fatal("settings lost across restart", got)
	}
}
