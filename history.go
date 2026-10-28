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
	"net/url"
	"os"
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
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Excerpt    string `json:"excerpt"`
	Source     string `json:"source"`
	Created    int64  `json:"created"`
	Text       string `json:"text,omitempty"`
	Image      []byte `json:"image,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	searchText string // immutable derived search representation; never persisted
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
	e.searchText = prepareSearch(e)
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
func (e Entry) summary() Entry { e.Text = ""; e.Image = nil; e.searchText = ""; return e }

type History struct {
	mu           sync.Mutex
	writeMu      sync.Mutex
	path         string
	db           *sqliteStore
	saveErr      error
	entries      []Entry
	pending      []historyChange
	pendingBytes int
	paused       bool
	version      uint64
	saved        uint64
}
type archive struct {
	Version int     `json:"version"`
	Paused  bool    `json:"paused"`
	Entries []Entry `json:"entries"`
}

func openHistory(path string) (*History, error) {
	h := &History{path: path, entries: []Entry{}}
	var a archive
	err := decodePrivateJSON(path, &a)
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
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
			return nil, fmt.Errorf("历史条目 %d 无效，原文件保持不变：%w", i+1, err)
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
func (h *History) Add(e Entry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.paused {
		if h.db != nil {
			if h.pendingBytes+e.size() > maxBytes || len(h.pending) >= 1024 {
				return errors.New("待保存内容过多，请检查磁盘空间后重试复制")
			}
			h.pending = append(h.pending, historyChange{entry: e})
			h.pendingBytes += e.size()
			h.version++
		} else {
			h.addLocked(e)
		}
	}
	return nil
}
func (h *History) IsPaused() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.paused }

// Snapshot is the bounded legacy JSON snapshot. SQLite panels use OpenView.
func (h *History) Snapshot() ([]Entry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Entry{}, h.entries...), h.paused
}
func (h *History) Pause(v bool) { h.mu.Lock(); defer h.mu.Unlock(); h.paused = v; h.version++ }
func (h *History) Remove(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.db != nil {
		h.pending = append(h.pending, historyChange{remove: id})
		h.version++
		return
	}
	for i, e := range h.entries {
		if e.ID == id {
			h.entries = append(h.entries[:i:i], h.entries[i+1:]...)
			h.version++
			return
		}
	}
}
func (h *History) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.db != nil {
		h.pending = append(h.pending, historyChange{clear: true})
	} else {
		h.entries = []Entry{}
	}
	h.version++
}
func (h *History) Save() error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return h.saveLocked(nil)
}

// A policy change commits pending copies and the new limits together, so
// relaxing a limit cannot prune those copies under the previous policy.
func (h *History) saveLocked(policy *RetentionPolicy) error {
	h.mu.Lock()
	if h.saveErr != nil {
		h.mu.Unlock()
		return h.saveErr
	}
	if h.saved == h.version && policy == nil {
		h.mu.Unlock()
		return nil
	}
	a := archive{1, h.paused, append([]Entry{}, h.entries...)}
	changes := append([]historyChange{}, h.pending...)
	pendingBytes := h.pendingBytes
	version := h.version
	h.mu.Unlock()
	var err error
	if h.db != nil {
		err = h.db.apply(changes, a.Paused, policy)
	} else {
		err = writePrivateJSON(h.path, a)
	}
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.saved = version
	if h.db != nil {
		if policy != nil {
			h.db.policy.Store(policy)
		}
		clear(h.pending[:len(changes)])
		h.pending = h.pending[len(changes):]
		h.pendingBytes -= pendingBytes
	}
	h.mu.Unlock()
	return nil
}
