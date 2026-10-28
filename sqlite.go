package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

const databaseName = "clipnest.sqlite3"
const databaseAppID = 0x434c504e

type sqliteStore struct {
	db      *sql.DB
	writeMu sync.Mutex
	policy  atomic.Pointer[RetentionPolicy]
}

// Files are created privately before SQLite opens them. Staging uses one
// connection and DELETE journaling so it can be published as a single file.
// enableLive switches published databases to WAL for frozen panel readers.
func openSQLite(path string, create bool) (*sqliteStore, error) {
	if create {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	}
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{"mode": {"rw"}, "_pragma": {"busy_timeout(5000)", "synchronous(FULL)", "secure_delete(ON)", "foreign_keys(ON)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &sqliteStore{db: db}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if create {
		err = s.transaction(func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				PRAGMA application_id = 1129074766;
				PRAGMA user_version = 3;
				CREATE TABLE history (
					id TEXT PRIMARY KEY, position INTEGER NOT NULL,
					search_id INTEGER NOT NULL,
					source TEXT NOT NULL, created INTEGER NOT NULL,
					kind TEXT NOT NULL, title TEXT NOT NULL, excerpt TEXT NOT NULL,
					width INTEGER NOT NULL, height INTEGER NOT NULL, byte_size INTEGER NOT NULL,
					source_search TEXT NOT NULL
				);
				CREATE INDEX history_order ON history(position);
				CREATE INDEX history_kind_order ON history(kind, position);
				CREATE INDEX history_age ON history(created);
				CREATE UNIQUE INDEX history_search_id ON history(search_id);
				CREATE TABLE history_content (
					id TEXT PRIMARY KEY REFERENCES history(id) ON DELETE CASCADE,
					text TEXT NOT NULL, image BLOB, search_prefix TEXT NOT NULL DEFAULT ''
				);
				CREATE TABLE phrases (
					id TEXT PRIMARY KEY, position INTEGER NOT NULL, text TEXT NOT NULL
				);
				CREATE TABLE settings (
					id INTEGER PRIMARY KEY CHECK (id = 1),
					paused INTEGER NOT NULL CHECK (paused IN (0, 1)),
					max_mib INTEGER NOT NULL DEFAULT 512,
					keep_days INTEGER NOT NULL DEFAULT 0
				);
				INSERT INTO settings(id, paused) VALUES (1, 0);
			` + searchSchema)
			return err
		})
	} else {
		var appID, version int
		err = db.QueryRow("PRAGMA application_id").Scan(&appID)
		if err == nil {
			err = db.QueryRow("PRAGMA user_version").Scan(&version)
		}
		if err == nil && (appID != databaseAppID || (version < 1 || version > 3)) {
			err = errors.New("不支持此数据库格式或版本")
		}
		if err == nil && version == 1 {
			err = s.upgradeV1()
		}
		if err == nil && version < 3 {
			err = s.upgradeSearch()
		}
	}
	if err == nil {
		var p RetentionPolicy
		err = db.QueryRow("SELECT max_mib, keep_days FROM settings WHERE id = 1").Scan(&p.MaxMiB, &p.KeepDays)
		if err == nil {
			err = p.validate()
		}
		if err == nil {
			s.policy.Store(&p)
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqliteStore) transaction(run func(*sql.Tx) error) error {
	// Panel readers remain concurrent, but all read-then-write transactions
	// share one writer lock to avoid SQLITE_BUSY_SNAPSHOT during promotion.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := run(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) load() (archive, []Phrase, error) {
	a := archive{Version: 1, Entries: []Entry{}}
	phrases := []Phrase{}
	var check string
	if err := s.db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil {
		return a, nil, err
	}
	if check != "ok" {
		return a, nil, errors.New("数据库完整性检查失败")
	}
	var orphaned int
	if err := s.db.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&orphaned); err != nil {
		return a, nil, err
	}
	if orphaned != 0 {
		return a, nil, errors.New("数据库内容引用不完整")
	}
	if err := s.db.QueryRow("SELECT paused FROM settings WHERE id = 1").Scan(&a.Paused); err != nil {
		return a, nil, err
	}
	rows, err := s.db.Query(`SELECT h.id, c.text, c.image, h.source, h.created
		FROM history h LEFT JOIN history_content c ON h.id = c.id ORDER BY h.position`)
	if err != nil {
		return a, nil, err
	}
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.ID, &e.Text, &e.Image, &e.Source, &e.Created); err != nil {
			break
		}
		var valid Entry
		valid, err = newEntry(e.Text, e.Image, e.Source)
		if err != nil {
			break
		}
		if valid.ID != e.ID {
			err = errors.New("历史内容与标识不一致")
			break
		}
		valid.Created = e.Created
		a.Entries = append(a.Entries, valid)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return a, nil, err
	}
	phrases, err = s.loadPhrases()
	return a, phrases, err
}

func (s *sqliteStore) loadPhrases() ([]Phrase, error) {
	phrases := []Phrase{}
	rows, err := s.db.Query("SELECT id, text FROM phrases ORDER BY position LIMIT ?", maxPhrases+1)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p Phrase
		if err = rows.Scan(&p.ID, &p.Text); err != nil {
			break
		}
		if err = checkPhraseText(p.Text); err != nil {
			break
		}
		if p.ID == "" {
			err = errors.New("常用语标识为空")
			break
		}
		phrases = append(phrases, p)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil && len(phrases) > maxPhrases {
		err = errors.New("常用语超过容量上限")
	}
	return phrases, err
}

// Used only to populate the empty staging database during migration.
func saveHistorySQL(tx *sql.Tx, a archive) error {
	for i, e := range a.Entries {
		if err := insertEntrySQL(tx, e, int64(i)); err != nil {
			return err
		}
	}
	_, err := tx.Exec("UPDATE settings SET paused = ? WHERE id = 1 AND paused <> ?", a.Paused, a.Paused)
	return err
}

func (s *sqliteStore) savePhrases(items []Phrase) error {
	return s.transaction(func(tx *sql.Tx) error { return savePhrasesSQL(tx, items) })
}

func savePhrasesSQL(tx *sql.Tx, items []Phrase) error {
	type savedPhrase struct {
		position int
		text     string
	}
	existing := map[string]savedPhrase{}
	rows, err := tx.Query("SELECT id, position, text FROM phrases")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var p savedPhrase
		if err = rows.Scan(&id, &p.position, &p.text); err != nil {
			break
		}
		existing[id] = p
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for i, p := range items {
		old, found := existing[p.ID]
		if !found {
			_, err = tx.Exec("INSERT INTO phrases (id, position, text) VALUES (?, ?, ?)", p.ID, i, p.Text)
		} else if old != (savedPhrase{i, p.Text}) {
			_, err = tx.Exec("UPDATE phrases SET position = ?, text = ? WHERE id = ?", i, p.Text, p.ID)
		}
		if err != nil {
			return fmt.Errorf("常用语写入失败：%w", err)
		}
		delete(existing, p.ID)
	}
	for id := range existing {
		if _, err := tx.Exec("DELETE FROM phrases WHERE id = ?", id); err != nil {
			return err
		}
	}
	return nil
}
