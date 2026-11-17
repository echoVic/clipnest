package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
)

func privacyView() (*preferencesView, *[]PrivacySettings) {
	v := settingsView()
	v.tab = 1
	v.state.Privacy = PrivacySettings{Secrets: secretsMask, ClearSync: true, Ignore: []string{`^\d{6}$`}}
	saved := &[]PrivacySettings{}
	v.actions.savePrivacy = func(s PrivacySettings) { *saved = append(*saved, s) }
	return v, saved
}

func TestPreferencesSensitiveContentControls(t *testing.T) {
	v, saved := privacyView()
	tt := ui.NewTester(v.draw, 520, 1600)
	for _, text := range []string{"敏感内容", "密钥、令牌与银行卡号", "遮罩后记录", "清空时删除", "忽略内容", `^\d{6}$`} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q in %v", text, tt.Texts())
		}
	}
	if !tt.HasText("文字与图片仅保存在这台电脑。标记为机密的内容不会记录，识别到的密钥会遮罩。") {
		t.Fatal("privacy note not updated", tt.Texts())
	}
	if err := tt.Click("遮罩后记录"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("不记录"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("清空时删除"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(`移除 ^\d{6}$`); err != nil {
		t.Fatal(err)
	}
	want := []PrivacySettings{
		{Secrets: secretsSkip, ClearSync: true, Ignore: []string{`^\d{6}$`}},
		{Secrets: secretsMask, ClearSync: false, Ignore: []string{`^\d{6}$`}},
		{Secrets: secretsMask, ClearSync: true, Ignore: []string{}},
	}
	if !reflect.DeepEqual(*saved, want) {
		t.Fatalf("saved %+v\nwant  %+v", *saved, want)
	}
}

func TestPreferencesIgnoreRuleInput(t *testing.T) {
	v, saved := privacyView()
	tt := ui.NewTester(v.draw, 520, 1600)
	if err := tt.Click("添加忽略规则"); err != nil {
		t.Fatal(err)
	}
	tt.Type("(unclosed")
	tt.Click("添加")
	if len(*saved) != 0 || !v.failed || v.notice == "" || v.ignoreDraft != "(unclosed" {
		t.Fatal("invalid rule was saved or the draft was lost", *saved, v.notice)
	}
	tt.Click("添加忽略规则")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("  (?i)internal  ")
	tt.Key(0, ui.KeyEnter)
	want := PrivacySettings{Secrets: secretsMask, ClearSync: true, Ignore: []string{`^\d{6}$`, "(?i)internal"}}
	if len(*saved) != 1 || !reflect.DeepEqual((*saved)[0], want) {
		t.Fatal("rule not added on Enter", *saved)
	}
	if v.ignoreDraft == "" {
		t.Fatal("draft cleared before the save succeeded")
	}
	state := v.state
	state.Privacy = want
	v.setState(state)
	if v.ignoreDraft != "" {
		t.Fatal("saved rule left in the input", v.ignoreDraft)
	}
	v.busy = true
	tt.Frame()
	tt.Click("移除 (?i)internal")
	if len(*saved) != 1 {
		t.Fatal("busy view dispatched an edit")
	}
}

func TestPreferencesIgnoreRulesEmptyState(t *testing.T) {
	v, _ := privacyView()
	v.state.Privacy.Ignore = []string{}
	tt := ui.NewTester(v.draw, 520, 1600)
	if !tt.HasText("尚未添加规则") {
		t.Fatal("empty rule list has no hint", tt.Texts())
	}
}

func TestSavePrivacyConsumesTheCurrentCopyOnlyOnChange(t *testing.T) {
	d := clipboardDesktop(t)
	consumed := 0
	consume := func() { consumed++ }
	next := PrivacySettings{Secrets: secretsSkip, ClearSync: true, Ignore: []string{"x"}}
	if got, err := d.savePrivacy(next, consume); err != nil || !reflect.DeepEqual(got, next) || consumed != 1 {
		t.Fatal("change not saved or current copy not consumed", got, err, consumed)
	}
	if _, err := d.savePrivacy(next, consume); err != nil || consumed != 1 {
		t.Fatal("unchanged settings consumed the current copy", err, consumed)
	}
	bad := PrivacySettings{Secrets: secretsMask, Ignore: []string{"("}}
	if got, err := d.savePrivacy(bad, consume); err == nil || !reflect.DeepEqual(got, next) || consumed != 1 {
		t.Fatal("invalid settings changed state", got, err)
	}
	injected := errors.New("disk full")
	d.storage.Privacy.write = func(string, any) error { return injected }
	if _, err := d.savePrivacy(defaultPrivacy(), consume); !errors.Is(err, injected) || consumed != 1 {
		t.Fatal("failed write consumed the current copy", err, consumed)
	}
	if !reflect.DeepEqual(d.Privacy(), next) {
		t.Fatal("failed write changed settings", d.Privacy())
	}
}
