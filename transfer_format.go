package main

import (
	"errors"
	"fmt"
	"strings"
)

// An export file is a ZIP of JSON documents and PNG images. See
// docs/superpowers/specs/2026-11-19-import-export-design.md.
const (
	exportFormat  = "clipnest-export"
	exportVersion = 1

	manifestName = "manifest.json"
	historyName  = "history.json"
	phrasesName  = "phrases.json"
	settingsName = "settings.json"
	imagePrefix  = "images/"
)

// What an import reads at most. Sizes in a ZIP's headers aren't trusted:
// every file is read through these limits.
const (
	maxManifestBytes = 64 << 10
	maxSettingsBytes = 1 << 20
	maxPhrasesBytes  = 200 << 20 // 500 phrases of 64 KB, escaped
	// One element of history.json: 512 KB of text escapes to at most six
	// times as long, plus the other fields.
	maxHistoryItemBytes = 4 << 20
	maxSourceBytes      = 256
	// Files in the ZIP besides one image per entry.
	maxExtraFiles = 16
)

type exportManifest struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	App      string `json:"app"`
	Platform string `json:"platform"`
	Exported int64  `json:"exported"` // Unix milliseconds
	// The counts are for people; an import counts the content itself.
	History int  `json:"history"`
	Images  int  `json:"images"`
	Phrases int  `json:"phrases"`
	Secrets bool `json:"secrets"` // whether sensitive entries are included
}

// exportItem is one history entry: text, or the name of its image. The
// rest of an entry is derived again on import.
type exportItem struct {
	Text        string `json:"text,omitempty"`
	Image       string `json:"image,omitempty"`
	Source      string `json:"source"`
	Created     int64  `json:"created"`
	EntryPinned bool   `json:"entryPinned,omitempty"`
}

type exportPhrases struct {
	Phrases []string `json:"phrases"`
}

// exportSettings are the settings an export carries. The shortcut and the
// excluded apps belong to Platform.
type exportSettings struct {
	Retention    RetentionPolicy `json:"retention"`
	ImageText    bool            `json:"imageText"`
	Privacy      PrivacySettings `json:"privacy"`
	Platform     string          `json:"platform"`
	Shortcut     string          `json:"shortcut"`
	ExcludedApps []ExcludedApp   `json:"excludedApps"`
}

var (
	errNotExport   = errors.New("这不是 Clipnest 的导出文件")
	errNewerExport = errors.New("这个文件来自更新版本的 Clipnest，请先更新")
	errDamaged     = errors.New("导入文件已损坏")
)

func damaged(detail string) error { return fmt.Errorf("%w：%s", errDamaged, detail) }

func imageName(id string) string { return imagePrefix + id + ".png" }

// validImageName reports whether name is images/<64 lowercase hex>.png, the
// only image names an export has.
func validImageName(name string) bool {
	id, ok := strings.CutPrefix(name, imagePrefix)
	if ok {
		id, ok = strings.CutSuffix(id, ".png")
	}
	if !ok || len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// sensitive reports whether a history entry may hold a password or key: it
// was recorded masked, or the rules recognize a secret in it now. History
// recorded while privacy was off isn't masked, but it still counts.
func sensitive(kind, text, secret string) bool {
	return kind != "image" && (secret != "" || len(findSecrets(text)) > 0)
}

func platformName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return goos
}
