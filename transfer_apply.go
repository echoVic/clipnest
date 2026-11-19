package main

import (
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"
)

// ImportResult is what an import did.
type ImportResult struct {
	New, Existing, Skipped, Invalid int
	NewPhrases, DuplicatePhrases    int
	PhrasesOver                     int
	Pruned                          int
	// Settings says whether the file's settings were imported. Shortcut is
	// its summon shortcut for the desktop to register, "" for none.
	Settings bool
	Shortcut string
	// Problems are the settings that couldn't be saved after the commit.
	Problems []string
}

// Import merges the file at path into the storage in one transaction,
// checking it all again. With withSettings it also takes the file's
// settings: retention and image text in the transaction, privacy and the
// excluded apps right after it. The caller keeps recording and panel edits
// off meanwhile.
func (s *Storage) Import(path string, withSettings bool) (ImportResult, error) {
	return s.importFile(path, withSettings, nil)
}

// importFile is Import with a hook that runs just before the commit; its
// error rolls the import back.
func (s *Storage) importFile(path string, withSettings bool, beforeCommit func() error) (ImportResult, error) {
	f, err := openImportFile(path)
	if err != nil {
		return ImportResult{}, err
	}
	defer f.Close()
	var settings *exportSettings
	if withSettings {
		settings, err = f.settings()
		if err == nil && settings == nil {
			err = errors.New("文件里没有设置")
		}
		if err == nil {
			err = settings.check()
		}
		if err != nil {
			return ImportResult{}, fmt.Errorf("文件里的设置无法导入：%w", err)
		}
	}
	texts, err := f.phrases()
	if err != nil {
		return ImportResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ImportResult{}, errors.New("存储已关闭")
	}
	if !s.sqlite.Load() {
		return ImportResult{}, errors.New("请先迁移到 SQLite，再导入或导出。")
	}
	h, p := s.History, s.Phrases
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if err := h.saveLocked(nil); err != nil {
		return ImportResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken != nil {
		return ImportResult{}, p.broken
	}
	add, duplicate, over, invalid := planPhrases(phraseTexts(p.items), texts)
	items := slices.Clone(p.items)
	ids := map[string]bool{}
	for _, x := range items {
		ids[x.ID] = true
	}
	for _, text := range add {
		id := p.newID()
		for ids[id] {
			id = p.newID()
		}
		ids[id] = true
		items = append(items, Phrase{ID: id, Text: text})
	}

	db := h.db
	policy := *db.policy.Load()
	if settings != nil {
		policy = settings.Retention
	}
	now := time.Now()
	result := ImportResult{NewPhrases: len(add), DuplicatePhrases: duplicate, PhrasesOver: over, Invalid: invalid}
	err = db.transaction(func(tx *sql.Tx) error {
		reindexed, err := importHistorySQL(tx, f, s.Privacy, now, &result)
		if err != nil {
			return err
		}
		if err := savePhrasesSQL(tx, items); err != nil {
			return err
		}
		if settings != nil {
			if _, err := tx.Exec("UPDATE settings SET max_mib=?,keep_days=?,max_entries=? WHERE id=1", policy.MaxMiB, policy.KeepDays, policy.MaxEntries); err != nil {
				return err
			}
			if err := db.setImageTextSQL(tx, settings.ImageText); err != nil {
				return err
			}
		}
		var before, after int
		if err := tx.QueryRow("SELECT count(*) FROM history").Scan(&before); err != nil {
			return err
		}
		pruned, err := pruneSQL(tx, policy, now)
		if err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT count(*) FROM history").Scan(&after); err != nil {
			return err
		}
		result.Pruned = before - after
		if pruned || reindexed {
			if err := purgeSearchSQL(tx); err != nil {
				return err
			}
		}
		if beforeCommit != nil {
			return beforeCommit()
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, fmt.Errorf("导入失败：%w，数据没有改动", err)
	}
	p.items = items
	if settings == nil {
		return result, nil
	}
	db.policy.Store(&policy)
	db.imageText.Store(settings.ImageText)
	result.Settings = true
	if err := s.Privacy.Save(settings.Privacy); err != nil {
		result.Problems = append(result.Problems, "隐私设置没能保存："+err.Error())
	}
	if settings.Platform == runtime.GOOS {
		if err := s.Exclusions.Replace(settings.ExcludedApps); err != nil {
			result.Problems = append(result.Problems, "排除的应用没能保存："+err.Error())
		}
		result.Shortcut = settings.Shortcut
	}
	return result, nil
}

// importHistorySQL writes the file's history in tx: new entries the way
// copies are written, and for content already here the later time and its
// source. Then it numbers the whole history newest first again, so imported
// entries sit among the others by time; among equal times the existing ones
// stay first. It reports whether search entries were replaced, leaving old
// terms to purge.
func importHistorySQL(tx *sql.Tx, f *importFile, privacy *privacyStore, now time.Time, result *ImportResult) (bool, error) {
	var next int64
	if err := tx.QueryRow("SELECT coalesce(max(position),0) FROM history").Scan(&next); err != nil {
		return false, err
	}
	reindexed := false
	seen := map[[32]byte]bool{}
	err := f.eachItem(func(item exportItem, ok bool) error {
		e, outcome := f.importedEntry(item, ok, now.UnixMilli(), privacy)
		switch outcome {
		case itemInvalid:
			result.Invalid++
			return nil
		case itemSkipped:
			result.Skipped++
			return nil
		}
		key := entryKey(e.ID)
		var created int64
		var source string
		err := tx.QueryRow("SELECT created, source FROM history WHERE id=?", e.ID).Scan(&created, &source)
		if errors.Is(err, sql.ErrNoRows) {
			// After the existing entries, in file order, until renumbered.
			next++
			seen[key] = true
			result.New++
			return insertEntrySQL(tx, e, next)
		}
		if err != nil {
			return err
		}
		if !seen[key] {
			seen[key] = true
			result.Existing++
		}
		if e.Created <= created {
			return nil
		}
		if _, err := tx.Exec("UPDATE history SET created=?,source=?,source_search=? WHERE id=?", e.Created, e.Source, strings.ToLower(e.Source), e.ID); err != nil {
			return err
		}
		if source == e.Source {
			return nil
		}
		reindexed = true
		return replaceSearchSQL(tx, e.ID)
	})
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(`UPDATE history SET position=r.n FROM
		(SELECT id, row_number() OVER (ORDER BY created DESC, position) AS n FROM history) AS r
		WHERE r.id=history.id AND history.position <> r.n`)
	return reindexed, err
}
