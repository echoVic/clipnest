package main

import (
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Tiling decodes the whole image at 4 bytes per pixel: a 16000×16000
// screenshot peaked at 1.25 GB. This limit keeps the decoded image near 200 MB.
const maxImageTextPixels = 50_000_000

// Stored in history.ocr_state.
const (
	ocrPending = 0
	ocrDone    = 1 // also non-images and images over the pixel limit
	ocrFailed  = 2
)

// ImageTextState describes an image's recognized text in the preview.
type ImageTextState string

const (
	imageTextPending     ImageTextState = "pending"
	imageTextReady       ImageTextState = "ready" // the text may be empty
	imageTextFailed      ImageTextState = "failed"
	imageTextDisabled    ImageTextState = "disabled"
	imageTextTooLarge    ImageTextState = "tooLarge"
	imageTextNeedsSQLite ImageTextState = "needsSQLite"
	imageTextUnsupported ImageTextState = "unsupported" // not macOS
)

// Recognized text lives outside history_content: SQLite rewrites a whole row
// on update, which would rewrite the image BLOB for every result or removal.
// search has a text entry's shape: the text, its title and a trailing space
// before the source, so the hidden default title no longer matches.
const (
	imageTextStateColumn   = "ocr_state INTEGER NOT NULL DEFAULT 1 CHECK (ocr_state IN (0, 1, 2))"
	imageTextEnabledColumn = "ocr_enabled INTEGER NOT NULL DEFAULT 1 CHECK (ocr_enabled IN (0, 1))"
	imageTextSchema        = `
		CREATE TABLE history_ocr (
			id TEXT PRIMARY KEY REFERENCES history(id) ON DELETE CASCADE,
			text TEXT NOT NULL, search TEXT NOT NULL
		);
		CREATE INDEX history_ocr_queue ON history(ocr_state, position);`
)

func imageTextEligible(e Entry) bool {
	return e.Kind == "image" && e.Width*e.Height <= maxImageTextPixels
}

// imageTextResult drops blank lines and surrounding spaces and keeps at most
// maxText bytes, cut on a character boundary.
func imageTextResult(text string) string {
	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	text = strings.Join(lines, "\n")
	if len(text) > maxText {
		cut := maxText
		for !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
	}
	return text
}

func imageTextTitle(text string) string {
	first, _, _ := strings.Cut(text, "\n")
	return shorten(first, 80)
}

// imageTextCopy is the entry CopyImageText delivers for an image's detail:
// newEntry gives it the ID capture computes, so the copy is not recorded.
func imageTextCopy(image Entry) (Entry, error) {
	if image.Kind != "image" || image.ImageText == "" {
		return Entry{}, errors.New("这张图片没有可复制的文字")
	}
	return newEntry(image.ImageText, nil, "")
}

// After a storage error the worker waits this many ticks before retrying, so a
// result that cannot be saved does not run Vision on the same image every tick.
const imageTextRetryTicks = 15

// recognizeImages drains the queue at start and on each tick until stop
// closes. Recognition failures mark the image; only storage errors back off.
func (h *History) recognizeImages(recognize func([]byte) (string, error), tick <-chan time.Time, stop <-chan struct{}) {
	wait := 0
	for {
		if wait == 0 && h.recognizePending(recognize, stop) != nil {
			wait = imageTextRetryTicks
		}
		select {
		case <-stop:
			return
		case <-tick:
			wait = max(wait-1, 0)
		}
	}
}

// recognizePending recognizes queued images, newest first, until none remain,
// the feature is disabled or stop closes. Recognition runs without the writer
// lock; a result commits only while its image is still queued and enabled.
func (h *History) recognizePending(recognize func([]byte) (string, error), stop <-chan struct{}) error {
	h.mu.Lock()
	db := h.db
	h.mu.Unlock()
	if db == nil {
		return nil
	}
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		epoch := db.imageTextEpoch.Load()
		var id string
		var png []byte
		err := db.db.QueryRow(`SELECT h.id, c.image FROM history h JOIN history_content c ON c.id=h.id
			WHERE h.ocr_state=? AND (SELECT ocr_enabled FROM settings WHERE id=1)=1
			ORDER BY h.position LIMIT 1`, ocrPending).Scan(&id, &png)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		text, recognizeErr := recognize(png)
		select {
		case <-stop:
			return nil
		default:
		}
		if err := db.saveImageText(id, epoch, text, recognizeErr); err != nil {
			return err
		}
	}
}

func (s *sqliteStore) saveImageText(id string, epoch uint64, text string, recognizeErr error) error {
	return s.transaction(func(tx *sql.Tx) error {
		if epoch != s.imageTextEpoch.Load() {
			return nil // disabled and possibly re-enabled during recognition
		}
		var queued bool
		err := tx.QueryRow(`SELECT 1 FROM history WHERE id=? AND ocr_state=? AND (SELECT ocr_enabled FROM settings WHERE id=1)=1`,
			id, ocrPending).Scan(&queued)
		if errors.Is(err, sql.ErrNoRows) {
			return nil // removed, cleared or disabled while recognizing
		}
		if err != nil {
			return err
		}
		if recognizeErr != nil {
			_, err = tx.Exec("UPDATE history SET ocr_state=? WHERE id=?", ocrFailed, id)
			return err
		}
		text = imageTextResult(text)
		if text == "" {
			_, err = tx.Exec("UPDATE history SET ocr_state=? WHERE id=?", ocrDone, id)
			return err
		}
		title := imageTextTitle(text)
		if _, err := tx.Exec("UPDATE history SET ocr_state=?, title=? WHERE id=?", ocrDone, title, id); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO history_ocr(id,text,search) VALUES(?,?,?)", id, text, strings.ToLower(text+" "+title+" ")); err != nil {
			return err
		}
		return replaceSearchSQL(tx, id)
	})
}

// imageTextDetail derives the preview state from the panel's snapshot.
func imageTextDetail(e *Entry, state int, text string, enabled bool) {
	switch {
	case e.Kind != "image":
	case !imageTextSupported:
		e.ImageTextState = imageTextUnsupported
	case !enabled:
		e.ImageTextState = imageTextDisabled
	case !imageTextEligible(*e):
		e.ImageTextState = imageTextTooLarge
	case state == ocrPending:
		e.ImageTextState = imageTextPending
	case state == ocrFailed:
		e.ImageTextState = imageTextFailed
	default:
		e.ImageTextState, e.ImageText = imageTextReady, text
		if text != "" {
			e.Title = imageTextTitle(text)
		}
	}
}

// SetImageText switches recognition. Disabling deletes every recognized text,
// restores default titles and merges the index so the removed fragments do not
// stay in FTS storage. Enabling queues every image again, including failures.
func (h *History) SetImageText(enabled bool) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.db == nil {
		return errors.New("迁移到 SQLite 后可识别图片文字")
	}
	err := h.db.transaction(func(tx *sql.Tx) error {
		var current bool
		if err := tx.QueryRow("SELECT ocr_enabled FROM settings WHERE id=1").Scan(&current); err != nil || current == enabled {
			return err
		}
		// Invalidate in-flight work under the same writer lock as its save.
		// A failed settings transaction only causes a pending image to retry.
		h.db.imageTextEpoch.Add(1)
		if _, err := tx.Exec("UPDATE settings SET ocr_enabled=? WHERE id=1", enabled); err != nil {
			return err
		}
		if enabled {
			_, err := tx.Exec("UPDATE history SET ocr_state=? WHERE kind='image' AND width*height<=?", ocrPending, maxImageTextPixels)
			return err
		}
		ids := []string{}
		rows, err := tx.Query("SELECT id FROM history_ocr")
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				break
			}
			ids = append(ids, id)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if _, err := tx.Exec("UPDATE history SET title=? WHERE id IN (SELECT id FROM history_ocr)", defaultImageTitle); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM history_ocr"); err != nil {
			return err
		}
		for _, id := range ids {
			if err := replaceSearchSQL(tx, id); err != nil {
				return err
			}
		}
		return purgeSearchSQL(tx)
	})
	if err == nil {
		h.db.imageText.Store(enabled)
	}
	return err
}

func (s *sqliteStore) upgradeImageText() error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("ALTER TABLE history ADD COLUMN " + imageTextStateColumn +
			"; ALTER TABLE settings ADD COLUMN " + imageTextEnabledColumn + ";" + imageTextSchema)
		if err == nil {
			_, err = tx.Exec("UPDATE history SET ocr_state=? WHERE kind='image' AND width*height<=?", ocrPending, maxImageTextPixels)
		}
		if err == nil {
			_, err = tx.Exec("PRAGMA user_version=4")
		}
		return err
	})
}
