package main

import (
	"cmp"
	"database/sql"
	"encoding/hex"
	"slices"
	"time"
)

// ImportPlan is what importing a file would do, counted without writing.
type ImportPlan struct {
	Manifest exportManifest
	// History: entries to add, entries already here, entries this
	// computer's privacy settings don't record, and history items and
	// phrases that fail the checks.
	New, Existing, Skipped, Invalid int
	NewPhrases, DuplicatePhrases    int
	PhrasesOver                     int // new phrases past the 500 limit
	// Pruned is how many entries the current retention policy deletes
	// right after the import, PrunedNew how many of those are imported.
	Pruned, PrunedNew int
	// Settings are the file's settings when they can be imported here, and
	// SettingsProblem why not when it has some that can't.
	Settings        *exportSettings
	SettingsProblem string
	// PrunedWithSettings is Pruned under the file's retention policy.
	PrunedWithSettings int
}

// nothingNew reports whether importing would add nothing.
func (p ImportPlan) nothingNew() bool {
	return p.New == 0 && p.NewPhrases == 0 && p.Settings == nil
}

// importMeta is what the check keeps of an entry: never its content.
type importMeta struct {
	created, size int64
	existing      bool
}

func entryKey(id string) [32]byte {
	var key [32]byte
	hex.Decode(key[:], []byte(id))
	return key
}

// PlanImport reads the file at path and counts what importing it would
// do, without writing anything.
func (s *Storage) PlanImport(path string) (ImportPlan, error) {
	f, err := openImportFile(path)
	if err != nil {
		return ImportPlan{}, err
	}
	defer f.Close()
	plan := ImportPlan{Manifest: f.manifest}
	now := time.Now()
	// The file's entries, in the order they first appear.
	var metas []importMeta
	index := map[[32]byte]int{}
	err = f.eachItem(func(item exportItem, ok bool) error {
		e, outcome := f.importedEntry(item, ok, now.UnixMilli(), s.Privacy)
		switch outcome {
		case itemInvalid:
			plan.Invalid++
			return nil
		case itemSkipped:
			plan.Skipped++
			return nil
		}
		key := entryKey(e.ID)
		if i, found := index[key]; found {
			metas[i].created = max(metas[i].created, e.Created)
			return nil
		}
		index[key] = len(metas)
		metas = append(metas, importMeta{created: e.Created, size: int64(e.size())})
		return nil
	})
	if err != nil {
		return ImportPlan{}, err
	}
	texts, err := f.phrases()
	if err != nil {
		return ImportPlan{}, err
	}
	settings, err := f.settings()
	if err != nil {
		return ImportPlan{}, err
	}
	if settings != nil {
		if err := settings.check(); err != nil {
			plan.SettingsProblem = err.Error()
		} else {
			plan.Settings = settings
		}
	}
	current, err := s.Phrases.texts()
	if err != nil {
		return ImportPlan{}, err
	}
	add, duplicate, over, invalid := planPhrases(current, texts)
	plan.NewPhrases, plan.DuplicatePhrases, plan.PhrasesOver = len(add), duplicate, over
	plan.Invalid += invalid

	db, tx, err := s.snapshot()
	if err != nil {
		return ImportPlan{}, err
	}
	defer db.Close()
	defer tx.Rollback()
	merged, err := mergeHistory(tx, metas, index)
	if err != nil {
		return ImportPlan{}, err
	}
	for _, m := range metas {
		if m.existing {
			plan.Existing++
		} else {
			plan.New++
		}
	}
	plan.Pruned, plan.PrunedNew = predictPrune(merged, s.Status().Retention, now)
	if plan.Settings != nil {
		plan.PrunedWithSettings, _ = predictPrune(merged, plan.Settings.Retention, now)
	}
	return plan, nil
}

// mergeHistory lists the history as the import leaves it, before
// retention, in the order Import numbers it: newest first; among equal
// times the existing entries in their order, then the new ones in file
// order. An existing entry takes the file's time when that is later. It
// marks the metas already here.
func mergeHistory(tx *sql.Tx, metas []importMeta, index map[[32]byte]int) ([]pruneItem, error) {
	items := make([]pruneItem, 0, len(metas))
	rows, err := tx.Query("SELECT id, created, byte_size FROM history ORDER BY position")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var it pruneItem
		if err := rows.Scan(&id, &it.created, &it.size); err != nil {
			return nil, err
		}
		if i, found := index[entryKey(id)]; found {
			metas[i].existing = true
			it.created = max(it.created, metas[i].created)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, m := range metas {
		if !m.existing {
			items = append(items, pruneItem{created: m.created, size: m.size, imported: true})
		}
	}
	slices.SortStableFunc(items, func(a, b pruneItem) int { return cmp.Compare(b.created, a.created) })
	return items, nil
}

// planPhrases picks the file's phrases to append to current: valid ones
// whose text isn't there yet, while there is room.
func planPhrases(current, file []string) (add []string, duplicate, over, invalid int) {
	have := make(map[string]bool, len(current)+len(file))
	for _, text := range current {
		have[text] = true
	}
	for _, text := range file {
		switch {
		case checkPhraseText(text) != nil:
			invalid++
		case have[text]:
			duplicate++
		case len(current)+len(add) >= maxPhrases:
			over++
		default:
			have[text] = true
			add = append(add, text)
		}
	}
	return add, duplicate, over, invalid
}
