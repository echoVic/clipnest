package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
)

type StorageStatus struct {
	Backend            string          `json:"backend"`
	MigrationAvailable bool            `json:"migrationAvailable"`
	CleanupPending     bool            `json:"cleanupPending"`
	Retention          RetentionPolicy `json:"retention"`
	ImageText          bool            `json:"imageText"`
}

// Storage owns the shared database; history and phrases keep their existing
// distinct commit semantics. The data-directory lock must be held by main.
type Storage struct {
	History    *History
	Phrases    *Phrases
	Exclusions *appExclusions
	dir        string
	mu         sync.Mutex
	sqlite     atomic.Bool
	cleanup    atomic.Bool
	closed     bool
}

func openStorage(dir string) (*Storage, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	exclusions, err := openAppExclusions(filepath.Join(dir, "excluded-apps.json"))
	if err != nil {
		return nil, err
	}
	legacy := false
	for _, name := range []string{"history.json", "phrases.json"} {
		_, err := os.Lstat(filepath.Join(dir, name))
		if err == nil {
			legacy = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	path := filepath.Join(dir, databaseName)
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		h, err := openHistory(filepath.Join(dir, "history.json"))
		if err != nil {
			return nil, err
		}
		s := &Storage{dir: dir, History: h, Phrases: openPhrases(filepath.Join(dir, "phrases.json")), Exclusions: exclusions}
		if !legacy {
			if _, err := s.Migrate(); err != nil {
				s.Close()
				return nil, err
			}
		}
		return s, nil
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("数据库路径不是普通文件")
	}
	// Never create schemas or fall back to JSON when a database already exists.
	db, err := openSQLite(path, false)
	if err != nil {
		return nil, fmt.Errorf("数据库无法打开，未读取旧 JSON：%w", err)
	}
	var paused bool
	err = db.db.QueryRow("SELECT paused FROM settings WHERE id=1").Scan(&paused)
	var phrases []Phrase
	if err == nil {
		phrases, err = db.loadPhrases()
	}
	if err == nil {
		err = db.enableLive()
	}
	if err != nil {
		db.db.Close()
		return nil, err
	}
	s := &Storage{dir: dir, History: &History{db: db, paused: paused}, Phrases: &Phrases{db: db, items: phrases}, Exclusions: exclusions}
	s.sqlite.Store(true)
	s.cleanup.Store(legacy)
	if err := s.History.Cleanup(); err != nil {
		db.db.Close()
		return nil, err
	}
	return s, nil
}

// Migrate freezes both stores while a private staging database is written and
// verified. The lock order also excludes an older history Save from publishing
// JSON after the switch, and queues phrase edits until the new writer is ready.
func (s *Storage) Migrate() (StorageStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.statusLocked(), errors.New("存储已关闭")
	}
	h, p := s.History, s.Phrases
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.sqlite.Load() {
		if h.saveErr != nil {
			return s.statusLocked(), h.saveErr
		}
		err := s.cleanupLegacy()
		return s.statusLocked(), err
	}
	if p.broken != nil {
		return s.statusLocked(), fmt.Errorf("请先修复常用语文件再迁移：%w", p.broken)
	}
	a := archive{1, h.paused, h.entries}
	db, installed, err := s.installDatabase(a, p.items)
	if !installed {
		return s.statusLocked(), err
	}
	s.sqlite.Store(true)
	s.cleanup.Store(true)
	if db == nil {
		// Publication already happened. Continuing to write JSON could diverge
		// from the authoritative DB. A restart will reopen the installed file.
		h.saveErr = fmt.Errorf("数据库已生成但无法重新打开，请重启 Clipnest：%w", err)
		p.broken = h.saveErr
		return s.statusLocked(), h.saveErr
	}
	h.db, p.db = db, db
	h.entries = nil
	h.saved = h.version
	if err != nil {
		return s.statusLocked(), fmt.Errorf("SQLite 已启用，旧文件尚未清理，请重试：%w", err)
	}
	err = s.cleanupLegacy()
	return s.statusLocked(), err
}

func (s *Storage) installDatabase(a archive, phrases []Phrase) (*sqliteStore, bool, error) {
	dest := filepath.Join(s.dir, databaseName)
	if _, err := os.Lstat(dest); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = errors.New("目标数据库已存在，未覆盖")
		}
		return nil, false, err
	}
	dir, err := os.MkdirTemp(s.dir, ".clipnest-migration-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(dir)
	staged := filepath.Join(dir, databaseName)
	db, err := openSQLite(staged, true)
	if err != nil {
		return nil, false, err
	}
	defer db.db.Close()
	err = db.transaction(func(tx *sql.Tx) error {
		if err := saveHistorySQL(tx, a); err != nil {
			return err
		}
		return savePhrasesSQL(tx, phrases)
	})
	if err != nil {
		return nil, false, err
	}
	loaded, loadedPhrases, err := db.load()
	if err != nil {
		return nil, false, err
	}
	if !reflect.DeepEqual(a, loaded) || !reflect.DeepEqual(phrases, loadedPhrases) {
		return nil, false, errors.New("迁移校验不一致，原数据保持不变")
	}
	if err := db.db.Close(); err != nil {
		return nil, false, err
	}
	if err := os.Rename(staged, dest); err != nil {
		return nil, false, err
	}
	syncErr := syncDirectory(s.dir)
	opened, err := openSQLite(dest, false)
	if err == nil {
		err = opened.enableLive()
		if err != nil {
			opened.db.Close()
			opened = nil
		}
	}
	return opened, true, errors.Join(syncErr, err)
}

// Cleanup only follows a verified, published database. On a cleanup failure
// SQLite remains authoritative and the UI offers retry; old JSON is never
// merged into it, which would resurrect deleted private clipboard content.
func (s *Storage) cleanupLegacy() error {
	if !s.cleanup.Load() {
		return nil
	}
	if err := syncDirectory(s.dir); err != nil {
		return err
	}
	for _, name := range []string{"history.json", "phrases.json"} {
		path := filepath.Join(s.dir, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err == nil && !info.Mode().IsRegular() {
			err = errors.New("旧存储路径不是普通文件")
		}
		if err != nil {
			return fmt.Errorf("SQLite 已启用，%s 清理失败：%w", name, err)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("SQLite 已启用，%s 清理失败：%w", name, err)
		}
	}
	if err := syncDirectory(s.dir); err != nil {
		return err
	}
	s.cleanup.Store(false)
	return nil
}

func (s *Storage) Status() StorageStatus {
	s.History.mu.Lock()
	defer s.History.mu.Unlock()
	return s.statusLocked()
}

func (s *Storage) statusLocked() StorageStatus {
	p := RetentionPolicy{512, 0}
	if s.History.db != nil {
		p = *s.History.db.policy.Load()
	}
	if s.sqlite.Load() {
		return StorageStatus{Backend: "sqlite", CleanupPending: s.cleanup.Load(), Retention: p, ImageText: s.History.db != nil && s.History.db.imageText.Load()}
	}
	return StorageStatus{Backend: "json", MigrationAvailable: true, Retention: p}
}

func (s *Storage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	err := s.History.Save()
	s.History.writeMu.Lock()
	defer s.History.writeMu.Unlock()
	s.Phrases.mu.Lock()
	defer s.Phrases.mu.Unlock()
	if s.History.db != nil {
		err = errors.Join(err, s.History.db.db.Close())
	}
	s.closed = true
	return err
}
