package main

import (
	"bytes"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func testImage(t *testing.T, w, h int, shade uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	img.SetGray(0, 0, color.Gray{shade ^ 0xff})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func imageEntry(t *testing.T, shade uint8) Entry {
	t.Helper()
	e, err := newEntry("", testImage(t, 24, 16, shade), "截图")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func imageTextStates(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	rows, err := db.Query("SELECT id, ocr_state FROM history")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	states := map[string]int{}
	for rows.Next() {
		var id string
		var state int
		if err := rows.Scan(&id, &state); err != nil {
			t.Fatal(err)
		}
		states[id] = state
	}
	return states
}

func TestImageTextSchemaQueuesImagesOnNewAndUpgradedDatabases(t *testing.T) {
	path := filepath.Join(t.TempDir(), databaseName)
	s, err := openSQLite(path, true)
	if err != nil {
		t.Fatal(err)
	}
	im, text := imageEntry(t, 40), entry(t, "plain text")
	if err := s.transaction(func(tx *sql.Tx) error { return saveHistorySQL(tx, archive{1, false, []Entry{im, text}}) }); err != nil {
		t.Fatal(err)
	}
	var version, enabled int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		t.Fatal("new database is not version 4", version, err)
	}
	if err := s.db.QueryRow("SELECT ocr_enabled FROM settings").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatal("image text must be enabled by default", enabled, err)
	}
	if got := imageTextStates(t, s.db); got[im.ID] != 0 || got[text.ID] != 1 {
		t.Fatal("new image was not queued", got)
	}
	// Reduce the database to the actual v3 schema, then reopen it.
	_, err = s.db.Exec(`DROP INDEX history_ocr_queue; DROP TABLE history_ocr;
		ALTER TABLE history DROP COLUMN ocr_state; ALTER TABLE settings DROP COLUMN ocr_enabled; PRAGMA user_version=3`)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	s, err = openSQLite(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		t.Fatal("v3 database not upgraded", version, err)
	}
	if err := s.db.QueryRow("SELECT ocr_enabled FROM settings").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatal("upgrade did not enable image text", enabled, err)
	}
	if got := imageTextStates(t, s.db); got[im.ID] != 0 || got[text.ID] != 1 {
		t.Fatal("upgrade did not queue existing images", got)
	}
	if !s.imageText.Load() {
		t.Fatal("enabled setting not cached")
	}
}

func TestImageTextMigrationQueuesImagesAndStillVerifies(t *testing.T) {
	dir := t.TempDir()
	a, _ := seedLegacyStorage(t, dir)
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if status := s.Status(); status.ImageText {
		t.Fatal("JSON storage reported image text as enabled")
	}
	if _, err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	if got := imageTextStates(t, s.History.db.db); got[a.Entries[1].ID] != 0 || got[a.Entries[0].ID] != 1 {
		t.Fatal("migrated image was not queued", got)
	}
	if !s.Status().ImageText {
		t.Fatal("SQLite storage did not report image text")
	}
}

func imageTextStorage(t *testing.T, entries ...Entry) *Storage {
	t.Helper()
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, e := range entries {
		s.History.Add(e)
	}
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	return s
}

// recognizeAll drains the queue with a fixed recognizer.
func recognizeAll(t *testing.T, h *History, recognize func([]byte) (string, error)) {
	t.Helper()
	if err := h.recognizePending(recognize, nil); err != nil {
		t.Fatal(err)
	}
}

func searchTotal(t *testing.T, h *History, q string) int {
	t.Helper()
	v, err := h.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	p, err := v.Page(q, "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	return p.Total
}

func TestImageTextResultBecomesTitleSearchAndDetail(t *testing.T) {
	im, other := imageEntry(t, 10), entry(t, "unrelated")
	s := imageTextStorage(t, im, other)
	recognized := "\n  部署失败：Error 502  \nnginx 配置 config.yaml\n"
	recognizeAll(t, s.History, func(png []byte) (string, error) {
		if !bytes.Equal(png, im.Image) {
			t.Fatal("recognizer did not receive the stored image")
		}
		return recognized, nil
	})
	if got := imageTextStates(t, s.History.db.db); got[im.ID] != ocrDone {
		t.Fatal("result did not complete the image", got)
	}
	for _, q := range []string{"署", "失败", "error 502", "config.yaml", "nginx 配置 config", "截图"} {
		if n := searchTotal(t, s.History, q); n != 1 {
			t.Fatalf("recognized image not found by %q: %d", q, n)
		}
	}
	// Every fragment of the stored text, including short Chinese and code
	// punctuation, follows the existing substring contract.
	stored := []rune(strings.ToLower(imageTextResult(recognized)))
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	for length := 1; length <= 6; length++ {
		for i := 0; i+length <= len(stored); i++ {
			q := strings.TrimSpace(string(stored[i : i+length]))
			if p, err := v.Page(q, "image", 0, 50); q != "" && (err != nil || p.Total != 1) {
				t.Fatalf("fragment %q of recognized text not found: %v", q, err)
			}
		}
	}
	if p, err := v.Page("复制的图片", "", 0, 50); err != nil || p.Total != 0 {
		t.Fatal("replaced default title still matches", err)
	}
	page, err := v.Page("", "image", 0, 50)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Title != "部署失败：Error 502" {
		t.Fatal("image title is not the first recognized line", page.Entries, err)
	}
	e, err := v.Detail(im.ID)
	if err != nil || e.ImageText != "部署失败：Error 502\nnginx 配置 config.yaml" || e.ImageTextState != imageTextReady {
		t.Fatalf("detail lacks recognized text: %q %q %v", e.ImageText, e.ImageTextState, err)
	}
	if e.Title != "部署失败：Error 502" || !bytes.Equal(e.Image, im.Image) {
		t.Fatal("detail changed image or title", e.Title)
	}
	if summary := e.summary(); summary.ImageText != "" {
		t.Fatal("list summaries must not carry recognized text")
	}
	if text, err := v.Detail(other.ID); err != nil || text.ImageTextState != "" {
		t.Fatal("text entry reported an image text state", text.ImageTextState, err)
	}
}

// 16000×3200 exceeds the recognition pixel limit; a 1-bit palette keeps it small.
func oversizedImage(t *testing.T) Entry {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, 16000, 3200), color.Palette{color.White, color.Black})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	e, err := newEntry("", b.Bytes(), "截图")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func detail(t *testing.T, h *History, id string) Entry {
	t.Helper()
	v, err := h.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	e, err := v.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestImageTextStatesAndFailuresAreNotRetried(t *testing.T) {
	im, big := imageEntry(t, 20), oversizedImage(t)
	s := imageTextStorage(t, im, big)
	if state := detail(t, s.History, im.ID).ImageTextState; state != imageTextPending {
		t.Fatal("new image is not pending", state)
	}
	if state := detail(t, s.History, big.ID).ImageTextState; state != imageTextTooLarge {
		t.Fatal("oversized image is not unavailable", state)
	}
	calls := 0
	recognizeAll(t, s.History, func([]byte) (string, error) {
		if calls++; calls > 1 {
			t.Fatal("failed or oversized images were recognized again")
		}
		return "partial", errors.New("vision failed")
	})
	recognizeAll(t, s.History, func([]byte) (string, error) { t.Fatal("failed image recognized again"); return "", nil })
	e := detail(t, s.History, im.ID)
	if e.ImageTextState != imageTextFailed || e.ImageText != "" || e.Title != "复制的图片" {
		t.Fatal("failure stored text or changed the title", e.ImageTextState, e.ImageText, e.Title)
	}
	if n := searchTotal(t, s.History, "partial"); n != 0 {
		t.Fatal("failed recognition was indexed")
	}
}

func TestImageTextEmptyResultKeepsDefaultTitle(t *testing.T) {
	im := imageEntry(t, 25)
	s := imageTextStorage(t, im)
	recognizeAll(t, s.History, func([]byte) (string, error) { return " \n\t\n", nil })
	e := detail(t, s.History, im.ID)
	if e.ImageTextState != imageTextReady || e.ImageText != "" || e.Title != "复制的图片" {
		t.Fatal("empty result was not a completed image without text", e.ImageTextState, e.ImageText, e.Title)
	}
	var rows int
	if err := s.History.db.db.QueryRow("SELECT count(*) FROM history_ocr").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("empty result stored a text row", rows, err)
	}
}

func TestImageTextUnavailableWithJSONStorage(t *testing.T) {
	h, err := openHistory(filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	im := imageEntry(t, 30)
	h.Add(im)
	recognizeAll(t, h, func([]byte) (string, error) { t.Fatal("JSON storage recognized an image"); return "", nil })
	if state := detail(t, h, im.ID).ImageTextState; state != imageTextNeedsSQLite {
		t.Fatal("JSON image text state", state)
	}
}

func TestImageTextResultIsTrimmedAndCappedOnUTF8Boundary(t *testing.T) {
	long := strings.Repeat("字", maxText/3+10)
	got := imageTextResult(" a \n\n" + long + "\n")
	if len(got) > maxText || len(got) < maxText-3 || !utf8.ValidString(got) || !strings.HasPrefix(got, "a\n字") {
		t.Fatal("recognized text not capped on a character boundary", len(got))
	}
	if got := imageTextResult(" \n\t\n"); got != "" {
		t.Fatalf("whitespace result kept: %q", got)
	}
	if got := imageTextTitle(strings.Repeat("长", 100) + "\nsecond"); got != strings.Repeat("长", 80)+"…" {
		t.Fatal("title is not the shortened first line", got)
	}
}

func TestImageTextSurvivesRecopyWithNewSource(t *testing.T) {
	im := imageEntry(t, 35)
	s := imageTextStorage(t, im)
	recognizeAll(t, s.History, func([]byte) (string, error) { return "invoice INV-2026-0042", nil })
	recopy := im
	recopy.Source, recopy.Created = "预览", im.Created+1
	s.History.Add(recopy)
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{"inv-2026-0042": 1, "预览": 1, "截图": 0} {
		if n := searchTotal(t, s.History, q); n != want {
			t.Fatalf("recopy search %q: %d", q, n)
		}
	}
	if e := detail(t, s.History, im.ID); e.ImageText != "invoice INV-2026-0042" || e.Source != "预览" {
		t.Fatal("recopy lost recognized text", e.ImageText, e.Source)
	}
}

func imageTextRows(t *testing.T, h *History) int {
	t.Helper()
	var n int
	if err := h.db.db.QueryRow("SELECT count(*) FROM history_ocr").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestImageTextDiscardsResultsForRemovedOrDisabledImages(t *testing.T) {
	for _, change := range []string{"remove", "clear", "expire", "disable"} {
		t.Run(change, func(t *testing.T) {
			im := imageEntry(t, 40)
			im.Created = time.Now().Add(-72 * time.Hour).UnixMilli()
			s := imageTextStorage(t, im)
			recognizeAll(t, s.History, func([]byte) (string, error) {
				var err error
				switch change {
				case "remove":
					s.History.Remove(im.ID)
					err = s.History.Save()
				case "clear":
					s.History.Clear()
					err = s.History.Save()
				case "expire":
					err = s.History.SetRetention(RetentionPolicy{0, 1})
				case "disable":
					err = s.History.SetImageText(false)
				}
				if err != nil {
					t.Fatal(err)
				}
				return "late private text", nil
			})
			if imageTextRows(t, s.History) != 0 || searchTotal(t, s.History, "private") != 0 {
				t.Fatal("result committed after the image changed")
			}
		})
	}
}

func TestImageTextStopDiscardsInFlightResult(t *testing.T) {
	a, b := imageEntry(t, 50), imageEntry(t, 51)
	s := imageTextStorage(t, a, b)
	stop := make(chan struct{})
	calls := 0
	if err := s.History.recognizePending(func([]byte) (string, error) { calls++; close(stop); return "late", nil }, stop); err != nil {
		t.Fatal(err)
	}
	if got := imageTextStates(t, s.History.db.db); calls != 1 || got[a.ID] != ocrPending || got[b.ID] != ocrPending || imageTextRows(t, s.History) != 0 {
		t.Fatal("stop did not end recognition before saving", calls, got)
	}
	recognizeAll(t, s.History, func([]byte) (string, error) { return "", nil })
	if got := imageTextStates(t, s.History.db.db); got[a.ID] != ocrDone || got[b.ID] != ocrDone {
		t.Fatal("stopped images were not recognized later", got)
	}
}

func TestImageTextReenableDiscardsPreviousRecognition(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{{"success", nil}, {"failure", errors.New("previous recognition failed")}} {
		t.Run(c.name, func(t *testing.T) {
			im := imageEntry(t, 55)
			s := imageTextStorage(t, im)
			calls := 0
			recognizeAll(t, s.History, func([]byte) (string, error) {
				calls++
				if calls == 1 {
					if err := s.History.SetImageText(false); err != nil {
						t.Fatal(err)
					}
					if err := s.History.SetImageText(true); err != nil {
						t.Fatal(err)
					}
					return "previous private text", c.err
				}
				if calls > 2 {
					t.Fatal("fresh result did not complete recognition")
				}
				return "new recognition", nil
			})
			if e := detail(t, s.History, im.ID); calls != 2 || e.ImageText != "new recognition" || e.ImageTextState != imageTextReady {
				t.Fatalf("previous recognition survived reenable: calls=%d state=%s text=%q", calls, e.ImageTextState, e.ImageText)
			}
		})
	}
}

// U+0001 makes the private fragment's index term the first on its FTS leaf,
// so it is stored whole and visible in the shadow table until merged away.
func TestImageTextDisableRemovesTextTitlesAndIndexThenEnableRequeues(t *testing.T) {
	im, failed := imageEntry(t, 60), imageEntry(t, 61)
	s := imageTextStorage(t, failed, im)
	recognizeAll(t, s.History, func(png []byte) (string, error) {
		if bytes.Equal(png, failed.Image) {
			return "", errors.New("unreadable")
		}
		return "\x01 私密 secret", nil
	})
	indexed := func() bool {
		var n int
		err := s.History.db.db.QueryRow("SELECT count(*) FROM history_search_data WHERE instr(block,CAST(? AS BLOB))>0", "g000001").Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
	if !indexed() || searchTotal(t, s.History, "私密") != 1 {
		t.Fatal("fixture did not index recognized text")
	}
	if err := s.History.SetImageText(false); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := s.History.db.db.QueryRow("SELECT title FROM history WHERE id=?", im.ID).Scan(&title); err != nil || title != defaultImageTitle {
		t.Fatal("disable did not restore the title", title, err)
	}
	if imageTextRows(t, s.History) != 0 || indexed() || searchTotal(t, s.History, "私密") != 0 || searchTotal(t, s.History, "复制的图片") != 2 {
		t.Fatal("disable left recognized text in storage or search")
	}
	if e := detail(t, s.History, im.ID); e.ImageTextState != imageTextDisabled || e.ImageText != "" || e.Title != defaultImageTitle {
		t.Fatal("disabled detail", e.ImageTextState, e.ImageText, e.Title)
	}
	if s.Status().ImageText {
		t.Fatal("status still enabled")
	}
	s.History.Add(imageEntry(t, 62))
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	recognizeAll(t, s.History, func([]byte) (string, error) { t.Fatal("recognized while disabled"); return "", nil })
	if err := s.History.SetImageText(false); err != nil {
		t.Fatal("repeated disable failed", err)
	}
	if err := s.History.SetImageText(true); err != nil {
		t.Fatal(err)
	}
	calls := 0
	recognizeAll(t, s.History, func([]byte) (string, error) { calls++; return "again 再次", nil })
	if calls != 3 || searchTotal(t, s.History, "再次") != 3 || !s.Status().ImageText {
		t.Fatal("enable did not recognize every image, including the failed one", calls)
	}
	if err := s.History.SetImageText(true); err != nil {
		t.Fatal(err)
	}
	recognizeAll(t, s.History, func([]byte) (string, error) { t.Fatal("repeated enable requeued images"); return "", nil })
}

func TestImageTextSettingPersistsAndNeedsSQLite(t *testing.T) {
	dir := t.TempDir()
	s, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.History.SetImageText(false); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Status().ImageText {
		t.Fatal("disabled setting lost on restart")
	}
	h, err := openHistory(filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetImageText(true); err == nil {
		t.Fatal("JSON storage accepted the image text setting")
	}
}

// Copying recognized text delivers a text entry whose ID capture would compute
// for that clipboard text, so the copy is not recorded as new history.
func TestImageTextCopyIsTheTextCaptureWouldSkip(t *testing.T) {
	im := imageEntry(t, 80)
	s := imageTextStorage(t, im)
	recognizeAll(t, s.History, func([]byte) (string, error) { return "第一行\nsecond line", nil })
	e, err := imageTextCopy(detail(t, s.History, im.ID))
	if err != nil {
		t.Fatal(err)
	}
	captured, _ := newEntry("第一行\nsecond line", nil, "截图")
	if e.Kind != "text" || e.Text != "第一行\nsecond line" || e.ID != captured.ID {
		t.Fatal("copied entry differs from the captured text", e.Kind, e.Text)
	}
	for _, image := range []Entry{imageEntry(t, 81), {Kind: "text", Text: "plain"}} {
		if _, err := imageTextCopy(image); err == nil {
			t.Fatal("copied text from an entry without recognized text", image.Kind)
		}
	}
}

// The worker drains the queue at start and on every tick until stopped.
func TestImageTextWorkerRecognizesNewImagesUntilStopped(t *testing.T) {
	s := imageTextStorage(t, imageEntry(t, 70))
	tick, stop, done := make(chan time.Time), make(chan struct{}), make(chan struct{})
	seen := make(chan []byte)
	go func() {
		defer close(done)
		s.History.recognizeImages(func(png []byte) (string, error) { seen <- png; return "worker 后台", nil }, tick, stop)
	}()
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not drain the queue at start")
	}
	select {
	case tick <- time.Time{}: // the first drain has finished
	case <-time.After(5 * time.Second):
		t.Fatal("worker does not wait for ticks")
	}
	later := imageEntry(t, 71)
	s.History.Add(later)
	if err := s.History.Save(); err != nil {
		t.Fatal(err)
	}
	for png := []byte(nil); !bytes.Equal(png, later.Image); {
		select {
		case tick <- time.Time{}:
		case png = <-seen:
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not recognize the new image")
		}
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	if searchTotal(t, s.History, "后台") != 2 {
		t.Fatal("worker results not saved")
	}
}

// A result that cannot be saved stays queued; the worker waits
// imageTextRetryTicks ticks instead of recognizing it again on every tick.
func TestImageTextWorkerBacksOffAfterStorageErrors(t *testing.T) {
	e := imageEntry(t, 72)
	s := imageTextStorage(t, e)
	if _, err := s.History.db.db.Exec("DROP TABLE history_ocr"); err != nil {
		t.Fatal(err)
	}
	tick, stop, done := make(chan time.Time), make(chan struct{}), make(chan struct{})
	calls := make(chan struct{})
	go func() {
		defer close(done)
		s.History.recognizeImages(func([]byte) (string, error) { calls <- struct{}{}; return "unsaved", nil }, tick, stop)
	}()
	select {
	case <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not drain the queue at start")
	}
	// A recognition blocks the worker until it is received here.
	for i := 1; i <= imageTextRetryTicks; i++ {
		select {
		case tick <- time.Time{}:
		case <-calls:
			t.Fatal("worker retried after", i-1, "ticks")
		}
	}
	select {
	case <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not retry")
	}
	close(stop)
	<-done
	if imageTextStates(t, s.History.db.db)[e.ID] != ocrPending {
		t.Fatal("unsaved image left the queue")
	}
}
