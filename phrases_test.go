package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func phraseTexts(t *testing.T, p *Phrases) []string {
	t.Helper()
	list, err := p.List("")
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{}
	for _, x := range list {
		full, err := p.Get(x.ID)
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, full.Text)
	}
	return texts
}
func TestPhrasesPersistInUserOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "phrases.json")
	p := openPhrases(path)
	if got := phraseTexts(t, p); len(got) != 0 {
		t.Fatal("missing file must start empty")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("opening must not create the file")
	}
	a, err := p.Save("", "use flux-sdd impl\nfor each impl，use sdt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := p.Save("", "Dead prose.")
	c, _ := p.Save("", "third")
	if len(a.ID) != 16 || a.ID == b.ID || b.ID == c.ID {
		t.Fatal("ids must be unique 16-char hex")
	}
	if edited, err := p.Save(b.ID, "  Simple prose.\n"); err != nil || edited.ID != b.ID {
		t.Fatal("editing must keep the id")
	}
	if err := p.Move(c.ID, -1); err != nil {
		t.Fatal(err)
	}
	if err := p.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	got := phraseTexts(t, openPhrases(path))
	if len(got) != 2 || got[0] != "third" || got[1] != "  Simple prose.\n" {
		t.Fatalf("order or exact text lost: %q", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("phrases file is not private")
	}
}
func TestPhraseMoveStopsAtEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phrases.json")
	p := openPhrases(path)
	a, _ := p.Save("", "a")
	b, _ := p.Save("", "b")
	os.Remove(path)
	if err := p.Move(a.ID, -1); err != nil {
		t.Fatal(err)
	}
	if err := p.Move(b.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a move past the ends must not write")
	}
	if err := p.Move(a.ID, 2); err == nil {
		t.Fatal("only single steps are allowed")
	}
	if got := strings.Join(phraseTexts(t, p), ""); got != "ab" {
		t.Fatalf("order changed: %q", got)
	}
}
func TestPhraseValidation(t *testing.T) {
	p := openPhrases(filepath.Join(t.TempDir(), "phrases.json"))
	for text, want := range map[string]string{
		"":                                   "内容不能为空",
		" \n\t":                              "内容不能为空",
		"\xff":                               "内容编码无效",
		strings.Repeat("x", maxPhraseText+1): "单条常用语最多 64 KB",
	} {
		if _, err := p.Save("", text); err == nil || err.Error() != want {
			t.Fatalf("got %v, want %s", err, want)
		}
	}
	if _, err := p.Save("", strings.Repeat("x", maxPhraseText)); err != nil {
		t.Fatal("a 64 KB phrase must be accepted")
	}
	if _, err := p.Save("missing", "x"); err == nil || err.Error() != "这条常用语已删除" {
		t.Fatal("editing a missing phrase must say it is gone")
	}
}
func TestPhraseCountLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phrases.json")
	items := make([]Phrase, maxPhrases)
	for i := range items {
		items[i] = Phrase{ID: fmt.Sprintf("%016x", i+1), Text: fmt.Sprintf("phrase %d", i)}
	}
	if err := writePrivateJSON(path, phraseArchive{1, items}); err != nil {
		t.Fatal(err)
	}
	p := openPhrases(path)
	if got, err := p.List(""); err != nil || len(got) != maxPhrases {
		t.Fatal("a full file must load")
	}
	if _, err := p.Save("", "one too many"); err == nil || err.Error() != "常用语最多 500 条" {
		t.Fatal("the 501st phrase must be refused")
	}
	if _, err := p.Save(items[0].ID, "edits still work when full"); err != nil {
		t.Fatal(err)
	}
}
func TestPhraseSearchAndExcerpt(t *testing.T) {
	p := openPhrases(filepath.Join(t.TempDir(), "phrases.json"))
	a, _ := p.Save("", "Can you make it like Google dev docs style.\n\n  More dead prose. "+strings.Repeat("词", 200))
	p.Save("", "use worktree for each impl")
	got, _ := p.List("  GOOGLE ")
	if len(got) != 1 || got[0].ID != a.ID || got[0].Text != "" {
		t.Fatal("search must be case-insensitive and return summaries without text")
	}
	if want := shorten("Can you make it like Google dev docs style. More dead prose. "+strings.Repeat("词", 200), 140); got[0].Excerpt != want {
		t.Fatalf("excerpt = %q", got[0].Excerpt)
	}
	if all, _ := p.List(""); len(all) != 2 {
		t.Fatal("an empty query lists everything")
	}
}
func TestFailedWriteKeepsPhrasesUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phrases.json")
	p := openPhrases(path)
	a, _ := p.Save("", "kept")
	// A directory where the file belongs makes the final rename fail.
	os.Remove(path)
	os.Mkdir(path, 0700)
	if _, err := p.Save("", "lost"); err == nil || !strings.HasPrefix(err.Error(), "常用语保存失败：") {
		t.Fatalf("create failure not reported: %v", err)
	}
	if _, err := p.Save(a.ID, "changed"); err == nil {
		t.Fatal("edit failure not reported")
	}
	if err := p.Delete(a.ID); err == nil {
		t.Fatal("delete failure not reported")
	}
	if got := phraseTexts(t, p); len(got) != 1 || got[0] != "kept" {
		t.Fatalf("memory changed after a failed write: %q", got)
	}
}
func TestCorruptPhrasesFileIsReadOnly(t *testing.T) {
	for name, body := range map[string]string{
		"syntax":    `{"version":1,"phrases":[`,
		"version":   `{"version":2,"phrases":[]}`,
		"empty id":  `{"version":1,"phrases":[{"id":"","text":"x"}]}`,
		"duplicate": `{"version":1,"phrases":[{"id":"a","text":"x"},{"id":"a","text":"y"}]}`,
		"blank":     `{"version":1,"phrases":[{"id":"a","text":"  "}]}`,
	} {
		path := filepath.Join(t.TempDir(), "phrases.json")
		os.WriteFile(path, []byte(body), 0600)
		p := openPhrases(path)
		_, listErr := p.List("")
		_, getErr := p.Get("a")
		_, saveErr := p.Save("", "new")
		for _, err := range []error{listErr, getErr, saveErr, p.Delete("a"), p.Move("a", 1)} {
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("%s: every operation must refuse with the file path, got %v", name, err)
			}
		}
		if b, _ := os.ReadFile(path); string(b) != body {
			t.Fatalf("%s: unreadable file was overwritten", name)
		}
	}
}
func TestPhraseBindingsUseTheStore(t *testing.T) {
	d := &Desktop{p: openPhrases(filepath.Join(t.TempDir(), "phrases.json"))}
	saved, err := d.SavePhrase("", "Simple.")
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := d.Phrases("SIMPLE"); len(list) != 1 || list[0].ID != saved.ID {
		t.Fatal("Phrases must list the saved phrase")
	}
	if got, _ := d.PhraseDetail(saved.ID); got.Text != "Simple." {
		t.Fatal("PhraseDetail must return the full text")
	}
	// The lookup fails before anything touches the clipboard.
	if _, err := d.InsertPhrase(0, "missing", true); err == nil || err.Error() != "这条常用语已删除" {
		t.Fatal("inserting a deleted phrase must fail")
	}
	if err := d.MovePhrase(saved.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := d.DeletePhrase(saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.PhraseDetail(saved.ID); err == nil {
		t.Fatal("a deleted phrase is still readable")
	}
}
