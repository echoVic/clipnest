package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxItems = 200
const maxBytes = 32 << 20
const maxText = 512 << 10
const maxImage = 8 << 20

// Entry owns immutable content. Panel sessions retain their own snapshot so
// new copies cannot shift the numbers underneath a user's fingers.
type Entry struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
	Source  string `json:"source"`
	Created int64  `json:"created"`
	Text    string `json:"text,omitempty"`
	Image   []byte `json:"image,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
}

func newEntry(text string, png []byte, source string) (Entry, error) {
	e := Entry{Source: source, Created: time.Now().UnixMilli()}
	var payload []byte
	if len(png) > 0 {
		if len(png) > maxImage {
			return e, errors.New("图片超过 8 MB，已跳过记录")
		}
		c, _, err := image.DecodeConfig(bytes.NewReader(png))
		if err != nil || c.Width > 16000 || c.Height > 16000 {
			return e, errors.New("图片无法读取或尺寸过大")
		}
		e.Kind, e.Image, e.Width, e.Height = "image", bytes.Clone(png), c.Width, c.Height
		e.Title, e.Excerpt = "复制的图片", fmt.Sprintf("%d × %d · PNG", c.Width, c.Height)
		payload = png
	} else {
		if strings.TrimSpace(text) == "" {
			return e, errors.New("空内容")
		}
		if len(text) > maxText || !utf8.ValidString(text) {
			return e, errors.New("文本超过 512 KB 或编码无效，已跳过记录")
		}
		e.Kind, e.Text = "text", text
		if u, err := url.Parse(strings.TrimSpace(text)); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && !strings.ContainsAny(strings.TrimSpace(text), "\n\r\t ") {
			e.Kind = "link"
		}
		if strings.Contains(text, "\n") && (strings.Contains(text, "function ") || strings.Contains(text, "const ") || strings.Contains(text, "func ") || strings.Contains(text, "import ") || json.Valid([]byte(text))) {
			e.Kind = "code"
		}
		e.Title = shorten(strings.Split(strings.TrimSpace(text), "\n")[0], 80)
		e.Excerpt = shorten(strings.Join(strings.Fields(text), " "), 140)
		payload = []byte(text)
	}
	h := sha256.New()
	h.Write([]byte(e.Kind))
	h.Write(payload)
	e.ID = hex.EncodeToString(h.Sum(nil))
	return e, nil
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
func (e Entry) size() int      { return len(e.Text) + len(e.Image) }
func (e Entry) summary() Entry { e.Text = ""; e.Image = nil; return e }

type History struct {
	mu      sync.Mutex
	writeMu sync.Mutex
	path    string
	entries []Entry
	paused  bool
	version uint64
	saved   uint64
}
type archive struct {
	Version int     `json:"version"`
	Paused  bool    `json:"paused"`
	Entries []Entry `json:"entries"`
}

func openHistory(path string) (*History, error) {
	h := &History{path: path, entries: []Entry{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var a archive
	// JSON escapes can expand a byte of text to six bytes. Allow the full
	// bounded history to round-trip even when it contains many control chars.
	decoder := json.NewDecoder(io.LimitReader(f, 256<<20))
	if err := decoder.Decode(&a); err != nil {
		return nil, fmt.Errorf("历史文件无法读取：%w", err)
	}
	if a.Version != 1 {
		return nil, errors.New("历史文件版本不受支持")
	}
	h.paused = a.Paused
	// Validate content and recompute IDs instead of trusting disk metadata.
	for i := len(a.Entries) - 1; i >= 0; i-- {
		x := a.Entries[i]
		e, err := newEntry(x.Text, x.Image, shorten(x.Source, 100))
		if err != nil {
			continue
		}
		e.Created = x.Created
		h.addLocked(e)
	}
	h.saved = h.version
	return h, nil
}
func (h *History) addLocked(e Entry) {
	result := []Entry{e}
	size := e.size()
	for _, old := range h.entries {
		if old.ID == e.ID {
			continue
		}
		if len(result) >= maxItems || size+old.size() > maxBytes {
			continue
		}
		result = append(result, old)
		size += old.size()
	}
	h.entries = result
	h.version++
}
func (h *History) Add(e Entry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.paused {
		h.addLocked(e)
	}
}
func (h *History) Snapshot() ([]Entry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Entry{}, h.entries...), h.paused
}
func (h *History) Pause(v bool) { h.mu.Lock(); defer h.mu.Unlock(); h.paused = v; h.version++ }
func (h *History) Remove(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, e := range h.entries {
		if e.ID == id {
			h.entries = append(h.entries[:i:i], h.entries[i+1:]...)
			h.version++
			return
		}
	}
}
func (h *History) Clear() { h.mu.Lock(); defer h.mu.Unlock(); h.entries = []Entry{}; h.version++ }
func (h *History) Save() error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	h.mu.Lock()
	if h.saved == h.version {
		h.mu.Unlock()
		return nil
	}
	a := archive{1, h.paused, append([]Entry{}, h.entries...)}
	version := h.version
	h.mu.Unlock()
	if err := writePrivateJSON(h.path, a); err != nil {
		return err
	}
	h.mu.Lock()
	h.saved = version
	h.mu.Unlock()
	return nil
}

// writePrivateJSON replaces path with v encoded as JSON, readable only by the
// user. Writing a temp file and renaming it means a crash never leaves half a file.
func writePrivateJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		err = json.NewEncoder(f).Encode(v)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
