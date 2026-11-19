package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testZip writes files, given as name and content, to a ZIP.
func testZip(t *testing.T, files ...[2]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "import.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, file := range files {
		w, err := zw.Create(file[0])
		if err == nil {
			_, err = w.Write([]byte(file[1]))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func manifestJSON(version int) string {
	return fmt.Sprintf(`{"format":"clipnest-export","version":%d,"app":"0.1.17","platform":"darwin","exported":1763520000000}`, version)
}

// importOf is an export file with the given history.json and more files.
func importOf(t *testing.T, history string, more ...[2]string) string {
	t.Helper()
	files := [][2]string{{manifestName, manifestJSON(1)}, {historyName, history}, {phrasesName, `{"phrases":[]}`}}
	return testZip(t, append(files, more...)...)
}

func openImport(t *testing.T, path string) *importFile {
	t.Helper()
	f, err := openImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestImportRefusesOtherFiles(t *testing.T) {
	notZip := filepath.Join(t.TempDir(), "notes.zip")
	os.WriteFile(notZip, []byte("hello"), 0600)
	basic := [][2]string{{historyName, "[]"}, {phrasesName, `{"phrases":[]}`}}
	with := func(manifest string, more ...[2]string) string {
		return testZip(t, append([][2]string{{manifestName, manifest}}, append(more, basic...)...)...)
	}
	for name, c := range map[string]struct {
		path string
		want error
	}{
		"not a ZIP":          {notZip, errNotExport},
		"no manifest":        {testZip(t, basic...), errNotExport},
		"another format":     {with(`{"format":"other","version":1}`), errNotExport},
		"newer":              {with(manifestJSON(2)), errNewerExport},
		"version 0":          {with(manifestJSON(0)), errDamaged},
		"broken manifest":    {with(`{"format":`), errDamaged},
		"two manifests":      {with(manifestJSON(1), [2]string{manifestName, manifestJSON(1)}), errDamaged},
		"manifest too large": {with(`{"format":"clipnest-export","version":1,"app":"` + strings.Repeat("x", maxManifestBytes) + `"}`), errDamaged},
		"no history":         {testZip(t, [2]string{manifestName, manifestJSON(1)}, [2]string{phrasesName, `{"phrases":[]}`}), errDamaged},
	} {
		t.Run(name, func(t *testing.T) {
			f, err := openImportFile(c.path)
			if f != nil {
				f.Close()
			}
			if !errors.Is(err, c.want) {
				t.Fatal(err)
			}
		})
	}
}

func countItems(f *importFile) (int, error) {
	n := 0
	err := f.eachItem(func(exportItem, bool) error { n++; return nil })
	return n, err
}

func TestImportHistoryLimits(t *testing.T) {
	near := strings.Repeat("s", maxHistoryItemBytes-100)
	for name, c := range map[string]struct {
		history string
		items   int
		ok      bool
	}{
		"empty":                       {"[]", 0, true},
		"two elements near the limit": {`[{"source":"` + near + `","created":1},{"source":"` + near + `","created":1}]`, 2, true},
		"element too large":           {`[{"text":"` + strings.Repeat("a", maxHistoryItemBytes) + `","created":1}]`, 0, false},
		"whitespace bomb":             {"[" + strings.Repeat(" ", maxHistoryItemBytes+1) + "]", 0, false},
		"too many":                    {"[" + strings.Repeat("{},", maxRetainedEntries) + "{}]", maxRetainedEntries, false},
		"not an array":                {`{}`, 0, false},
		"trailing data":               {"[] []", 0, false},
		"broken":                      {`[{"text":`, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			n, err := countItems(openImport(t, importOf(t, c.history)))
			if n != c.items || (err == nil) != c.ok || (err != nil && !errors.Is(err, errDamaged)) {
				t.Fatal(n, err)
			}
		})
	}
}

func TestImportChecksEachItem(t *testing.T) {
	png := testImage(t, 24, 16, 7)
	img, err := newEntry("", png, "截图")
	if err != nil {
		t.Fatal(err)
	}
	notPNG, large := imageName(strings.Repeat("c", 64)), imageName(strings.Repeat("d", 64))
	now := time.Now().UnixMilli()
	history := `[
		{"text":"好的","source":"` + strings.Repeat("长", 100) + `","created":1},
		{"text":"未来","created":` + fmt.Sprint(now+3600_000) + `},
		{"created":1},
		{"text":"x","image":"` + imageName(img.ID) + `","created":1},
		{"text":"   ","created":1},
		{"text":"` + strings.Repeat("a", maxText+1) + `","created":1},
		{"text":"零","created":0},
		{"image":"images/../x.png","created":1},
		{"image":"` + imageName(strings.Repeat("b", 64)) + `","created":1},
		{"image":"` + notPNG + `","created":1},
		{"image":"` + large + `","created":1},
		{"text":5,"created":1},
		5,
		{"image":"` + imageName(img.ID) + `","source":"截图","created":2}
	]`
	f := openImport(t, importOf(t, history,
		[2]string{imageName(img.ID), string(png)}, [2]string{notPNG, "not a png"},
		[2]string{large, strings.Repeat("\x00", maxImage+1)}))
	var kept []Entry
	invalid := 0
	err = f.eachItem(func(item exportItem, ok bool) error {
		if !ok {
			invalid++
			return nil
		}
		e, err := f.entry(item, now)
		if err != nil {
			invalid++
		} else {
			kept = append(kept, e)
		}
		return nil
	})
	if err != nil || invalid != 11 || len(kept) != 3 {
		t.Fatal(err, invalid, kept)
	}
	if kept[0].Source != "" || kept[1].Created != now || kept[2].ID != img.ID || kept[2].Kind != "image" || kept[2].Created != 2 {
		t.Fatal(kept[0].Source, kept[1].Created, kept[2].ID, kept[2].Created)
	}
}

// A header that understates a file's size doesn't let more than the limit in.
func TestImportDoesNotTrustSizesInTheZip(t *testing.T) {
	data := []byte(strings.Repeat("\x00", 9<<20))
	name := imageName(strings.Repeat("e", 64))
	path := filepath.Join(t.TempDir(), "lying.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, file := range [][2]string{{manifestName, manifestJSON(1)}, {historyName, `[{"image":"` + name + `","created":1}]`}, {phrasesName, `{"phrases":[]}`}} {
		w, err := zw.Create(file[0])
		if err == nil {
			_, err = w.Write([]byte(file[1]))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	// The header says 10 bytes; the file holds 9 MB.
	w, err := zw.CreateRaw(&zip.FileHeader{Name: name, Method: zip.Store, CRC32: crc32.ChecksumIEEE(data), CompressedSize64: uint64(len(data)), UncompressedSize64: 10})
	if err == nil {
		_, err = w.Write(data)
	}
	if err == nil {
		err = zw.Close()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	imp := openImport(t, path)
	err = imp.eachItem(func(item exportItem, ok bool) error {
		if _, err := imp.entry(item, time.Now().UnixMilli()); err == nil {
			t.Error("took a file larger than its header says")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportPhrasesFile(t *testing.T) {
	many := `{"phrases":[` + strings.Repeat(`"x",`, maxPhrases) + `"x"]}`
	for name, c := range map[string]struct {
		phrases string
		ok      bool
	}{
		"fine":         {`{"phrases":["一","二"]}`, true},
		"too many":     {many, false},
		"broken":       {`{"phrases":[`, false},
		"far too many": {`{"phrases":[` + strings.Repeat(`"x",`, 100000) + `"x"]}`, false},
		"bad UTF-8":    {"{\"phrases\":[\"\xff\"]}", false},
	} {
		t.Run(name, func(t *testing.T) {
			path := testZip(t, [2]string{manifestName, manifestJSON(1)}, [2]string{historyName, "[]"}, [2]string{phrasesName, c.phrases})
			texts, err := openImport(t, path).phrases()
			if (err == nil) != c.ok || (err != nil && !errors.Is(err, errDamaged)) {
				t.Fatal(texts, err)
			}
		})
	}
}

func TestImportSettingsFile(t *testing.T) {
	if s, err := openImport(t, importOf(t, "[]")).settings(); s != nil || err != nil {
		t.Fatal("settings without a file", s, err)
	}
	if _, err := openImport(t, importOf(t, "[]", [2]string{settingsName, "{"})).settings(); !errors.Is(err, errDamaged) {
		t.Fatal(err)
	}
	good := func() *exportSettings {
		return &exportSettings{Retention: RetentionPolicy{MaxMiB: 512}, ImageText: true, Privacy: defaultPrivacy(), Platform: runtime.GOOS, Shortcut: "Alt+V", ExcludedApps: []ExcludedApp{{BundleID: "com.example.vault", Name: "Vault"}}}
	}
	for name, c := range map[string]struct {
		change func(*exportSettings)
		ok     bool
	}{
		"fine":           {func(*exportSettings) {}, true},
		"retention":      {func(s *exportSettings) { s.Retention.KeepDays = -1 }, false},
		"privacy":        {func(s *exportSettings) { s.Privacy.Ignore = []string{"("} }, false},
		"secrets mode":   {func(s *exportSettings) { s.Privacy.Secrets = "" }, false},
		"no platform":    {func(s *exportSettings) { s.Platform = "" }, false},
		"shortcut":       {func(s *exportSettings) { s.Shortcut = "Hyper+V" }, false},
		"no shortcut":    {func(s *exportSettings) { s.Shortcut = "" }, true},
		"duplicate apps": {func(s *exportSettings) { s.ExcludedApps = append(s.ExcludedApps, s.ExcludedApps[0]) }, false},
		"another platform's shortcut and apps": {func(s *exportSettings) {
			s.Platform, s.Shortcut = "plan9", "Hyper+V"
			s.ExcludedApps = append(s.ExcludedApps, s.ExcludedApps[0])
		}, true},
	} {
		t.Run(name, func(t *testing.T) {
			s := good()
			c.change(s)
			if err := s.check(); (err == nil) != c.ok {
				t.Fatal(err)
			}
		})
	}
}
