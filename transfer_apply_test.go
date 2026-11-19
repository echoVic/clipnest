package main

import (
	"errors"
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
