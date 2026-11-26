package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestImportRoundTrip(t *testing.T) {
	from := transferStorage(t)
	texts := []Entry{entry(t, "文字"), entry(t, "https://example.com/a"), entry(t, "func main() {\n}")}
	// Built after the texts, so creation order equals insertion order. The
	// image is still waiting for recognition here, so its title matches the
	// copy's, which waits too.
	img := imageEntry(t, 30)
	addEntries(t, from, append(texts, img)...)
	if err := from.History.SetEntryPinned(texts[0].ID, true); err != nil {
		t.Fatal(err)
	}
	savePhrases(t, from, "一", "二")
	privacy := from.Privacy.Settings()
	privacy.ClearSync, privacy.Ignore = false, []string{"^PIN"}
	from.Privacy.Save(privacy)
	from.Exclusions.Add(ExcludedApp{BundleID: "com.example.vault", Name: "Vault"})
	from.History.SetRetention(RetentionPolicy{MaxMiB: 100})

	to := transferStorage(t)
	result, err := to.Import(exportOf(t, from, false), true)
	if err != nil {
		t.Fatal(err)
	}
	if result.New != 4 || !result.Settings || result.Shortcut != "Alt+V" || len(result.Problems) != 0 {
		t.Fatalf("%+v", result)
	}
	want, _ := storedSnapshot(t, from.History)
	got, _ := storedSnapshot(t, to.History)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("history differs", got, want)
	}
	var maxMiB, keepDays, maxEntries int
	if err := to.History.db.db.QueryRow("SELECT max_mib, keep_days, max_entries FROM settings WHERE id=1").Scan(&maxMiB, &keepDays, &maxEntries); err != nil || maxMiB != 100 || keepDays != 0 || maxEntries != 0 {
		t.Fatal("retention row", maxMiB, keepDays, maxEntries, err)
	}
	if imageTextStates(t, to.History.db.db)[img.ID] != ocrPending {
		t.Fatal("the image isn't queued for recognition")
	}
	if !slices.Equal(phraseList(t, to), []string{"一", "二"}) {
		t.Fatal(phraseList(t, to))
	}
	if !reflect.DeepEqual(to.Privacy.Settings(), privacy) || !slices.Equal(to.Exclusions.List(), from.Exclusions.List()) || to.Status().Retention != (RetentionPolicy{MaxMiB: 100}) {
		t.Fatal("settings", to.Privacy.Settings(), to.Exclusions.List(), to.Status().Retention)
	}
}

func TestImportMergesByContent(t *testing.T) {
	to := transferStorage(t)
	addEntries(t, to, textEntry(t, "丁", "这边", 120), textEntry(t, "乙", "这边", 100), textEntry(t, "戊", "这边", 300))
	from := transferStorage(t)
	addEntries(t, from, textEntry(t, "甲", "那边", 50), textEntry(t, "乙", "那边", 200), textEntry(t, "gamma ray", "那边", 150), textEntry(t, "戊", "那边", 10))
	path := exportOf(t, from, false)
	result, err := to.Import(path, false)
	if err != nil || result.New != 2 || result.Existing != 2 {
		t.Fatalf("%+v %v", result, err)
	}
	entries, _ := storedSnapshot(t, to.History)
	var got []string
	for _, e := range entries {
		got = append(got, e.Text+"@"+e.Source)
	}
	want := []string{"戊@这边", "乙@那边", "gamma ray@那边", "丁@这边", "甲@那边"}
	if !slices.Equal(got, want) {
		t.Fatal(got)
	}
	v, err := to.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	page, err := v.Page("gamma", "", 0, 10)
	v.Close()
	if err != nil || len(page.Entries) != 1 {
		t.Fatal("imported entry isn't searchable", page.Entries, err)
	}
	if again, err := to.Import(path, false); err != nil || again.New != 0 || again.Existing != 4 {
		t.Fatalf("second import %+v %v", again, err)
	}
}

func TestImportAppendsPhrases(t *testing.T) {
	to := transferStorage(t)
	savePhrases(t, to, "二", "四")
	from := transferStorage(t)
	savePhrases(t, from, "一", "二", "三")
	result, err := to.Import(exportOf(t, from, false), false)
	if err != nil || result.NewPhrases != 2 || result.DuplicatePhrases != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if got := phraseList(t, to); !slices.Equal(got, []string{"二", "四", "一", "三"}) {
		t.Fatal(got)
	}
	stored, err := to.History.db.loadPhrases()
	if err != nil || !slices.Equal(phraseTexts(stored), []string{"二", "四", "一", "三"}) {
		t.Fatal("phrases not in the database", stored, err)
	}
}

func TestImportRedoesPrivacy(t *testing.T) {
	from := transferStorage(t)
	addEntries(t, from, entry(t, "AWS="+testAWSKey)) // recorded while privacy was off
	path := exportOf(t, from, true)
	to := transferStorage(t)
	if _, err := to.Import(path, false); err != nil {
		t.Fatal(err)
	}
	entries, _ := storedSnapshot(t, to.History)
	if len(entries) != 1 || entries[0].Secret == "" {
		t.Fatal("not masked under this computer's settings", entries)
	}
	skip := transferStorage(t)
	privacy := skip.Privacy.Settings()
	privacy.Secrets = secretsSkip
	skip.Privacy.Save(privacy)
	if result, err := skip.Import(path, false); err != nil || result.Skipped != 1 || result.New != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestImportLeavesSettingsUnlessAsked(t *testing.T) {
	from := transferStorage(t)
	privacy := from.Privacy.Settings()
	privacy.ClearSync = false
	from.Privacy.Save(privacy)
	from.History.SetImageText(false)
	to := transferStorage(t)
	before := to.Privacy.Settings()
	result, err := to.Import(exportOf(t, from, false), false)
	if err != nil || result.Settings || result.Shortcut != "" {
		t.Fatalf("%+v %v", result, err)
	}
	if !reflect.DeepEqual(to.Privacy.Settings(), before) || !to.Status().ImageText {
		t.Fatal("settings changed")
	}
	if _, err := to.Import(exportOf(t, from, false), true); err != nil || to.Status().ImageText {
		t.Fatal("image text stays on", err)
	}
}

func TestImportSkipsAnotherPlatformsShortcutAndApps(t *testing.T) {
	from := transferStorage(t)
	path := withSettings(t, exportOf(t, from, false), func(s *exportSettings) {
		s.Platform = "plan9"
		s.Privacy.ClearSync = false
		s.ExcludedApps = []ExcludedApp{{BundleID: "com.example.vault", Name: "Vault"}}
	})
	to := transferStorage(t)
	result, err := to.Import(path, true)
	if err != nil || result.Shortcut != "" || len(to.Exclusions.List()) != 0 || to.Privacy.Settings().ClearSync {
		t.Fatalf("%+v %v %v", result, err, to.Exclusions.List())
	}
}

// A file from an older version may hold a common combination like Ctrl+V:
// everything else imports, and the shortcut stays as it is.
func TestImportLeavesOutACommonShortcut(t *testing.T) {
	from := transferStorage(t)
	addEntries(t, from, entry(t, "导入的"))
	path := withSettings(t, exportOf(t, from, false), func(s *exportSettings) { s.Shortcut = "Ctrl+V" })
	to := transferStorage(t)
	result, err := to.Import(path, true)
	if err != nil || result.New != 1 || !result.Settings || result.Shortcut != "" {
		t.Fatalf("%+v %v", result, err)
	}
	if len(result.Problems) != 1 || !strings.HasPrefix(result.Problems[0], "没有导入快捷键 ") || !strings.Contains(result.Problems[0], "常用快捷键") {
		t.Fatal(result.Problems)
	}
}

func TestImportFailureChangesNothing(t *testing.T) {
	from := transferStorage(t)
	addEntries(t, from, entry(t, "新的"))
	savePhrases(t, from, "新常用语")
	from.History.SetRetention(RetentionPolicy{MaxEntries: 5})
	from.History.SetImageText(false)
	to := transferStorage(t)
	addEntries(t, to, entry(t, "原来的"))
	savePhrases(t, to, "原来的常用语")
	privacy := to.Privacy.Settings()
	_, err := to.importFile(exportOf(t, from, false), true, func() error { return errors.New("磁盘已满") })
	if err == nil || !strings.Contains(err.Error(), "数据没有改动") {
		t.Fatal(err)
	}
	stored, _ := to.History.db.loadPhrases()
	if !slices.Equal(historyTexts(t, to), []string{"原来的"}) || !slices.Equal(phraseList(t, to), []string{"原来的常用语"}) ||
		!slices.Equal(phraseTexts(stored), []string{"原来的常用语"}) || to.Status().Retention != (RetentionPolicy{MaxMiB: 512}) ||
		!reflect.DeepEqual(to.Privacy.Settings(), privacy) {
		t.Fatal("the failed import left changes")
	}
	var maxMiB, keepDays, maxEntries int
	var ocr bool
	if err := to.History.db.db.QueryRow("SELECT max_mib, keep_days, max_entries, ocr_enabled FROM settings WHERE id=1").Scan(&maxMiB, &keepDays, &maxEntries, &ocr); err != nil || maxMiB != 512 || keepDays != 0 || maxEntries != 0 || !ocr || !to.Status().ImageText {
		t.Fatal("settings row changed", maxMiB, keepDays, maxEntries, ocr, err)
	}
}

func TestImportCleanupMatchesThePlan(t *testing.T) {
	for _, imported := range []bool{false, true} {
		to := transferStorage(t)
		now := time.Now().UnixMilli()
		addEntries(t, to, textEntry(t, "新一", "", now-1000), textEntry(t, "新二", "", now))
		to.History.SetRetention(RetentionPolicy{MaxEntries: 2})
		from := transferStorage(t)
		addEntries(t, from, textEntry(t, "旧一", "", now-9000), textEntry(t, "中", "", now-500), textEntry(t, "旧三", "", now-7000))
		from.History.SetRetention(RetentionPolicy{MaxEntries: 4})
		path := exportOf(t, from, false)
		plan, err := to.PlanImport(path)
		if err != nil {
			t.Fatal(err)
		}
		result, err := to.Import(path, imported)
		want := plan.Pruned
		if imported {
			want = plan.PrunedWithSettings
		}
		if err != nil || result.Pruned != want {
			t.Fatalf("settings %v: planned %d, pruned %d (%v)", imported, want, result.Pruned, err)
		}
		survivors := historyTexts(t, to)
		wantSurvivors := []string{"新二", "中"}
		if imported {
			wantSurvivors = []string{"新二", "中", "新一", "旧三"}
		}
		if !slices.Equal(survivors, wantSurvivors) {
			t.Fatalf("settings %v: survivors %v", imported, survivors)
		}
		lost := 0
		for _, text := range []string{"旧一", "中", "旧三"} {
			if !slices.Contains(survivors, text) {
				lost++
			}
		}
		// The plan only predicts how many imported entries go under the current policy.
		if !imported && lost != plan.PrunedNew {
			t.Fatalf("settings %v: %d of the file's entries gone, plan said %d", imported, lost, plan.PrunedNew)
		}
	}
}

func TestImportShrinksTheWAL(t *testing.T) {
	var items []string
	for i := range 20 {
		items = append(items, fmt.Sprintf(`{"text":"%d %s","source":"","created":%d}`, i, strings.Repeat("x", 300<<10), 1000+i))
	}
	path := importOf(t, "["+strings.Join(items, ",")+"]")
	to := transferStorage(t)
	result, err := to.Import(path, false)
	if err != nil || result.New != 20 {
		t.Fatal(result, err)
	}
	info, err := os.Stat(filepath.Join(to.dir, databaseName+"-wal"))
	if err == nil && info.Size() > 1<<20 {
		t.Fatalf("the WAL is %d bytes after an import", info.Size())
	}
}

func TestPlanImportAgreesWithImport(t *testing.T) {
	policies := []RetentionPolicy{{}, {KeepDays: 3}, {MaxEntries: 12}, {MaxMiB: 1}, {KeepDays: 5, MaxEntries: 20, MaxMiB: 1}}
	for seed := uint64(1); seed <= 20; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			r := rand.New(rand.NewPCG(seed, seed*7919))
			now := time.Now()
			ago := func() int64 { return now.Add(-time.Duration(r.IntN(8*24)) * time.Hour).UnixMilli() }
			text := func(label string) string {
				if r.IntN(6) == 0 {
					return label + strings.Repeat("y", 200_000+r.IntN(100_000))
				}
				return label + strings.Repeat("z", r.IntN(50))
			}
			to := transferStorage(t)
			var existing []Entry
			for i := range 10 {
				e := textEntry(t, text(fmt.Sprintf("old%d ", i)), "旧", ago())
				existing = append(existing, e)
				addEntries(t, to, e)
			}
			var items []string
			var fileTexts []string
			add := func(text, source string, created int64) {
				b, _ := json.Marshal(exportItem{Text: text, Source: source, Created: created})
				items = append(items, string(b))
				fileTexts = append(fileTexts, text)
			}
			for i := range 25 {
				switch r.IntN(7) {
				case 0: // already in the target, maybe later
					add(existing[r.IntN(len(existing))].Text, "新", ago())
				case 1: // duplicate inside the file
					if len(fileTexts) > 0 {
						add(fileTexts[r.IntN(len(fileTexts))], "复", ago())
						continue
					}
					fallthrough
				case 2: // equal timestamps
					add(text(fmt.Sprintf("tie%d ", i)), "同", now.Add(-48*time.Hour).UnixMilli())
				case 3: // the future
					add(text(fmt.Sprintf("future%d ", i)), "未来", now.Add(72*time.Hour).UnixMilli())
				case 4: // invalid
					items = append(items, `{"text":"","created":1}`)
				default:
					add(text(fmt.Sprintf("new%d ", i)), "文件", ago())
				}
			}
			path := importOf(t, "["+strings.Join(items, ",")+"]")
			if err := to.History.SetRetention(policies[r.IntN(len(policies))]); err != nil {
				t.Fatal(err)
			}
			before := historyTexts(t, to)
			plan, err := to.PlanImport(path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := to.Import(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if plan.New != result.New || plan.Existing != result.Existing || plan.Skipped != result.Skipped || plan.Invalid != result.Invalid || plan.Pruned != result.Pruned {
				t.Fatalf("plan %+v, import %+v", plan, result)
			}
			after := map[string]bool{}
			for _, x := range historyTexts(t, to) {
				after[x] = true
			}
			was := map[string]bool{}
			for _, x := range before {
				was[x] = true
			}
			missing := 0
			seen := map[string]bool{}
			for _, x := range fileTexts {
				if !was[x] && !seen[x] && !after[x] {
					missing++
				}
				seen[x] = true
			}
			if plan.PrunedNew != missing {
				t.Fatalf("PrunedNew %d, %d new texts missing", plan.PrunedNew, missing)
			}
		})
	}
}
