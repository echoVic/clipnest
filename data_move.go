package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"
)

// movedSettings are the JSON files a move copies along with the database.
// The JSON history and phrases from before SQLite are not copied: once the
// database is in use they are only left to clean up.
var movedSettings = []string{"excluded-apps.json", "privacy.json", "shortcut.json", phraseShortcutFile}

// moveOps are the steps tests make fail.
type moveOps struct {
	staged func(stage string) error // after copying, before checking
	rename func(from, to string) error
}

func defaultMoveOps() moveOps {
	return moveOps{staged: func(string) error { return nil }, rename: os.Rename}
}

// MoveTo copies the data to target, checks the copy, puts it in place and
// calls commit, which records target as the data directory. Then it closes
// the storage, for Clipnest to start again on target. Any failure before
// commit returns leaves target without Clipnest files and the storage open.
//
// Writes wait until the move ends, but the history lock stays free: the
// clipboard monitor reads the paused state on the main thread, and the copy
// can take seconds. The caller stops recording and edits meanwhile.
func (s *Storage) MoveTo(target string, commit func() error) error {
	return s.moveTo(target, commit, defaultMoveOps())
}

func (s *Storage) moveTo(target string, commit func() error, ops moveOps) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("存储已关闭")
	}
	if !s.sqlite.Load() {
		return errors.New("请先迁移到 SQLite，再更改数据位置")
	}
	h, p := s.History, s.Phrases
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if err := h.saveLocked(nil); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken != nil {
		return p.broken
	}
	s.Exclusions.mu.RLock()
	defer s.Exclusions.mu.RUnlock()
	s.Privacy.mu.RLock()
	defer s.Privacy.mu.RUnlock()
	db := h.db
	db.writeMu.Lock()
	defer db.writeMu.Unlock()

	_, statErr := os.Stat(target)
	created := errors.Is(statErr, fs.ErrNotExist)
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	unlock, err := acquireDataLock(target)
	if err != nil {
		removeCreated(target, created)
		return err
	}
	published, err := s.publishCopy(db, target, ops)
	if err == nil {
		err = commit()
	}
	if err != nil {
		var left []error
		for _, path := range published {
			if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				left = append(left, rerr)
			}
		}
		unlock()
		os.Remove(filepath.Join(target, ".writer.lock"))
		removeCreated(target, created)
		if len(left) > 0 {
			return fmt.Errorf("移动失败，仍使用原位置：%w。复制到「%s」的文件没能删除，可以手动删除（%v）", err, target, errors.Join(left...))
		}
		return fmt.Errorf("移动失败，仍使用原位置：%w", err)
	}
	unlock()
	s.closed = true
	// The copy is in place and recorded, and the restart opens it.
	if err := db.db.Close(); err != nil {
		log.Print("移动后关闭原数据库失败：", err)
	}
	return nil
}

func removeCreated(dir string, created bool) {
	if created {
		os.Remove(dir)
	}
}

// publishCopy copies into a staging folder in target, checks the copy and
// moves its files into target, never over a file already there. It returns
// the paths it put in target.
func (s *Storage) publishCopy(db *sqliteStore, target string, ops moveOps) ([]string, error) {
	stale, _ := filepath.Glob(filepath.Join(target, moveStagingPrefix+"*"))
	for _, dir := range stale {
		os.RemoveAll(dir)
	}
	stage, err := os.MkdirTemp(target, moveStagingPrefix)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	// The panel's read transaction holds the only connection until the
	// panel closes, which the caller has asked for.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	conn, err := db.db.Conn(ctx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("历史仍在读取，请稍后再试：%w", err)
	}
	want, err := countTables(conn)
	copyPath := filepath.Join(stage, databaseName)
	if err == nil {
		// VACUUM INTO writes into an empty file that already exists; left to
		// create it, SQLite would let the umask decide who can read it.
		err = writeSynced(copyPath, nil)
	}
	if err == nil {
		_, err = conn.ExecContext(context.Background(), "VACUUM INTO ?", copyPath)
	}
	conn.Close()
	if err != nil {
		return nil, err
	}
	settings := map[string][]byte{}
	for _, name := range movedSettings {
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := writeSynced(filepath.Join(stage, name), data); err != nil {
			return nil, err
		}
		settings[name] = data
	}
	if err := ops.staged(stage); err != nil {
		return nil, err
	}
	if err := checkCopy(stage, want, settings); err != nil {
		return nil, err
	}

	names := []string{databaseName}
	for _, name := range movedSettings {
		if _, ok := settings[name]; ok {
			names = append(names, name)
		}
	}
	var published []string
	for _, name := range names {
		dest := filepath.Join(target, name)
		if _, err := os.Lstat(dest); !errors.Is(err, fs.ErrNotExist) {
			if err == nil {
				err = fmt.Errorf("目标文件夹里已有 %s，未覆盖", name)
			}
			return published, err
		}
		if err := ops.rename(filepath.Join(stage, name), dest); err != nil {
			return published, err
		}
		published = append(published, dest)
	}
	return published, syncDirectory(target)
}

func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

// checkCopy opens the copied database: it must pass the integrity check and
// have as many rows in every table as the original. The settings must read
// back byte for byte.
func checkCopy(stage string, want map[string]int, settings map[string][]byte) error {
	copied, err := openSQLite(filepath.Join(stage, databaseName), false)
	if err != nil {
		return err
	}
	defer copied.db.Close()
	var check string
	if err := copied.db.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil {
		return err
	}
	if check != "ok" {
		return errors.New("复制的数据库未通过完整性检查")
	}
	got, err := countTables(copied.db)
	if err != nil {
		return err
	}
	if len(got) != len(want) {
		return errors.New("复制的数据库表不一致")
	}
	for table, n := range want {
		if got[table] != n {
			return fmt.Errorf("复制的数据库 %s 表行数不一致", table)
		}
	}
	for name, data := range settings {
		read, err := os.ReadFile(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		if !bytes.Equal(read, data) {
			return fmt.Errorf("复制的 %s 内容不一致", name)
		}
	}
	return copied.db.Close()
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func countTables(q queryer) (map[string]int, error) {
	ctx := context.Background()
	rows, err := q.QueryContext(ctx, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, name := range names {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "`+name+`"`).Scan(&n); err != nil {
			return nil, err
		}
		counts[name] = n
	}
	return counts, nil
}
