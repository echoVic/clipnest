package main

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"
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

// exportOptions are what an export takes besides the storage.
type exportOptions struct {
	Secrets  bool   // include sensitive entries
	Shortcut string // the summon shortcut, "" when none is registered
	App      string // the Clipnest version
	Now      time.Time
	// read runs inside the snapshot, after the images; tests stall or fail
	// the export there.
	read func() error
}

// Export writes the history, phrases and settings to path. It reads a
// snapshot on its own connection, so recording and panel edits go on, and
// the file appears only once it is complete. The summary counts what it
// wrote; Sensitive counts the sensitive entries whether or not they went in.
func (s *Storage) Export(path string, opts exportOptions) (ExportSummary, error) {
	// The JSON settings are taken before the read starts.
	settings := exportSettings{
		Privacy: s.Privacy.Settings(), Platform: runtime.GOOS,
		Shortcut: opts.Shortcut, ExcludedApps: s.Exclusions.List(),
	}
	db, tx, err := s.snapshot()
	if err != nil {
		return ExportSummary{}, err
	}
	defer db.Close()
	defer tx.Rollback()
	r := &settings.Retention
	err = tx.QueryRow("SELECT max_mib, keep_days, max_entries, ocr_enabled FROM settings WHERE id=1").Scan(&r.MaxMiB, &r.KeepDays, &r.MaxEntries, &settings.ImageText)
	if err != nil {
		return ExportSummary{}, err
	}
	part := path + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return ExportSummary{}, err
	}
	err = f.Chmod(0600) // a part file left by a crash may have other permissions
	var sum ExportSummary
	if err == nil {
		sum, err = writeExport(f, tx, settings, opts)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(part, path)
	}
	if err != nil {
		os.Remove(part)
		return ExportSummary{}, err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		log.Print("导出文件所在的文件夹未能同步：", err)
	}
	return sum, nil
}

// writeExport writes the ZIP. Images go first: a ZIP is written one file at
// a time, and history.json only names them.
func writeExport(w io.Writer, tx *sql.Tx, settings exportSettings, opts exportOptions) (ExportSummary, error) {
	var sum ExportSummary
	zw := zip.NewWriter(w)
	header := func(name string, method uint16) *zip.FileHeader {
		return &zip.FileHeader{Name: name, Method: method, Modified: opts.Now}
	}
	rows, err := tx.Query("SELECT h.id, c.image FROM history h JOIN history_content c ON c.id=h.id WHERE h.kind='image' ORDER BY h.position")
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var id string
		var png []byte
		if err = rows.Scan(&id, &png); err != nil {
			break
		}
		var fw io.Writer
		if fw, err = zw.CreateHeader(header(imageName(id), zip.Store)); err == nil {
			_, err = fw.Write(png)
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil && opts.read != nil {
		err = opts.read()
	}
	if err != nil {
		return sum, err
	}

	fw, err := zw.CreateHeader(header(historyName, zip.Deflate))
	if err != nil {
		return sum, err
	}
	enc := json.NewEncoder(fw)
	enc.SetEscapeHTML(false)
	if _, err := io.WriteString(fw, "["); err != nil {
		return sum, err
	}
	rows, err = tx.Query("SELECT h.id, h.kind, h.source, h.created, h.secret, c.text FROM history h JOIN history_content c ON c.id=h.id ORDER BY h.position")
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var id, kind, source, secret, text string
		var created int64
		if err = rows.Scan(&id, &kind, &source, &created, &secret, &text); err != nil {
			break
		}
		if sensitive(kind, text, secret) {
			sum.Sensitive++
			if !opts.Secrets {
				continue
			}
		}
		item := exportItem{Text: text, Source: source, Created: created}
		if kind == "image" {
			item = exportItem{Image: imageName(id), Source: source, Created: created}
			sum.Images++
		}
		if sum.History > 0 {
			if _, err = io.WriteString(fw, ","); err != nil {
				break
			}
		}
		if err = enc.Encode(item); err != nil {
			break
		}
		sum.History++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		_, err = io.WriteString(fw, "]\n")
	}
	if err != nil {
		return sum, err
	}

	texts := []string{}
	rows, err = tx.Query("SELECT text FROM phrases ORDER BY position")
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var text string
		if err = rows.Scan(&text); err != nil {
			break
		}
		texts = append(texts, text)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return sum, err
	}
	sum.Phrases = len(texts)

	manifest := exportManifest{
		Format: exportFormat, Version: exportVersion, App: opts.App, Platform: runtime.GOOS,
		Exported: opts.Now.UnixMilli(), History: sum.History, Images: sum.Images, Phrases: sum.Phrases,
		Secrets: opts.Secrets && sum.Sensitive > 0,
	}
	for _, doc := range []struct {
		name string
		v    any
	}{{phrasesName, exportPhrases{texts}}, {settingsName, settings}, {manifestName, manifest}} {
		fw, err := zw.CreateHeader(header(doc.name, zip.Deflate))
		if err != nil {
			return sum, err
		}
		enc := json.NewEncoder(fw)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(doc.v); err != nil {
			return sum, err
		}
	}
	return sum, zw.Close()
}
