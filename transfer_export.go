package main

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
)

// openSnapshot opens its own read-only connection to the database at path
// and starts a read transaction on it. Under WAL the transaction keeps the
// database as of its first read while the storage goes on writing, and it
// takes neither of the storage's two connections, one of which an open
// panel holds.
func openSnapshot(path string) (*sql.DB, *sql.Tx, error) {
	q := url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(5000)", "query_only(1)"}}
	db, err := sql.Open("sqlite", sqliteFileURI(path)+"?"+q.Encode())
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err == nil {
		// BEGIN alone doesn't take the snapshot; the first read does.
		var n int
		if err = tx.QueryRow("SELECT count(*) FROM history").Scan(&n); err != nil {
			tx.Rollback()
		}
	}
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return db, tx, nil
}

// snapshot writes the history not yet in the database, then opens a
// snapshot of the database.
func (s *Storage) snapshot() (*sql.DB, *sql.Tx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, errors.New("存储已关闭")
	}
	if !s.sqlite.Load() {
		return nil, nil, errors.New("请先迁移到 SQLite，再导入或导出。")
	}
	if err := s.History.Save(); err != nil {
		return nil, nil, err
	}
	return openSnapshot(filepath.Join(s.dir, databaseName))
}

// ExportSummary counts what an export holds. Sensitive counts the history
// entries that may hold a password or key.
type ExportSummary struct {
	History, Images, Phrases, Sensitive int
}

func (s *Storage) ExportSummary() (ExportSummary, error) {
	var sum ExportSummary
	db, tx, err := s.snapshot()
	if err != nil {
		return sum, err
	}
	defer db.Close()
	defer tx.Rollback()
	err = tx.QueryRow("SELECT count(*), count(*) FILTER (WHERE kind='image') FROM history").Scan(&sum.History, &sum.Images)
	if err == nil {
		err = tx.QueryRow("SELECT count(*) FROM phrases").Scan(&sum.Phrases)
	}
	if err != nil {
		return sum, err
	}
	rows, err := tx.Query("SELECT h.kind, c.text, h.secret FROM history h JOIN history_content c ON c.id=h.id WHERE h.kind<>'image'")
	if err != nil {
		return sum, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, text, secret string
		if err := rows.Scan(&kind, &text, &secret); err != nil {
			return sum, err
		}
		if sensitive(kind, text, secret) {
			sum.Sensitive++
		}
	}
	return sum, rows.Err()
}
