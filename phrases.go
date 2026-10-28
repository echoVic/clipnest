package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

const maxPhrases = 500
const maxPhraseText = 64 << 10

// Phrase is text kept for reuse. IDs survive edits and the order is the
// user's own, so a phrase's number only changes when the user moves it.
type Phrase struct {
	ID      string `json:"id"`
	Text    string `json:"text,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

func (p Phrase) summary() Phrase {
	return Phrase{ID: p.ID, Excerpt: shorten(strings.Join(strings.Fields(p.Text), " "), 140)}
}

// Phrases makes a change visible only after it is on disk. A file that cannot
// be read turns the store read-only so user-written text is never replaced.
type Phrases struct {
	mu     sync.Mutex
	path   string
	db     *sqliteStore
	items  []Phrase
	broken error
	write  func(string, any) error // nil uses the production durable writer
}
type phraseArchive struct {
	Version int      `json:"version"`
	Phrases []Phrase `json:"phrases"`
}

var errPhraseGone = errors.New("这条常用语已删除")

func checkPhraseText(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("内容不能为空")
	}
	if !utf8.ValidString(text) {
		return errors.New("内容编码无效")
	}
	if len(text) > maxPhraseText {
		return errors.New("单条常用语最多 64 KB")
	}
	return nil
}

func openPhrases(path string) *Phrases {
	p := &Phrases{path: path, items: []Phrase{}}
	items, err := readPhrases(path)
	if err != nil {
		p.broken = fmt.Errorf("常用语文件无法读取，已停止修改：%s。修好或移走这个文件后重启 Clipnest。", path)
		return p
	}
	p.items = items
	return p
}
func readPhrases(path string) ([]Phrase, error) {
	var a phraseArchive
	err := decodePrivateJSON(path, &a)
	if errors.Is(err, os.ErrNotExist) {
		return []Phrase{}, nil
	}
	if err != nil {
		return nil, err
	}
	if a.Version != 1 || len(a.Phrases) > maxPhrases {
		return nil, errors.New("unsupported phrases file")
	}
	items := []Phrase{}
	seen := map[string]bool{}
	for _, x := range a.Phrases {
		if x.ID == "" || seen[x.ID] {
			return nil, errors.New("missing or duplicate phrase id")
		}
		if err := checkPhraseText(x.Text); err != nil {
			return nil, err
		}
		seen[x.ID] = true
		items = append(items, Phrase{ID: x.ID, Text: x.Text})
	}
	return items, nil
}
func (p *Phrases) index(id string) int {
	return slices.IndexFunc(p.items, func(x Phrase) bool { return x.ID == id })
}

// List returns summaries in the user's order, filtered case-insensitively.
func (p *Phrases) List(q string) ([]Phrase, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken != nil {
		return nil, p.broken
	}
	q = strings.ToLower(strings.TrimSpace(q))
	out := []Phrase{}
	for _, x := range p.items {
		if q == "" || strings.Contains(strings.ToLower(x.Text), q) {
			out = append(out, x.summary())
		}
	}
	return out, nil
}
func (p *Phrases) Get(id string) (Phrase, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken != nil {
		return Phrase{}, p.broken
	}
	i := p.index(id)
	if i < 0 {
		return Phrase{}, errPhraseGone
	}
	return p.items[i], nil
}

// Save appends a new phrase when id is empty and otherwise replaces the text
// of an existing one in place.
func (p *Phrases) Save(id, text string) (Phrase, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken != nil {
		return Phrase{}, p.broken
	}
	if err := checkPhraseText(text); err != nil {
		return Phrase{}, err
	}
	items := slices.Clone(p.items)
	saved := Phrase{ID: id, Text: text}
	if id == "" {
		if len(items) >= maxPhrases {
			return Phrase{}, errors.New("常用语最多 500 条")
		}
		saved.ID = p.newID()
		items = append(items, saved)
	} else if i := p.index(id); i >= 0 {
		items[i] = saved
	} else {
		return Phrase{}, errPhraseGone
	}
	if err := p.commit(items); err != nil {
		return Phrase{}, err
	}
	return saved, nil
}
func (p *Phrases) Delete(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken != nil {
		return p.broken
	}
	i := p.index(id)
	if i < 0 {
		return errPhraseGone
	}
	return p.commit(slices.Delete(slices.Clone(p.items), i, i+1))
}

// Move shifts a phrase one place up (-1) or down (1). Moving past either end
// changes nothing and leaves the file alone.
func (p *Phrases) Move(id string, delta int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken != nil {
		return p.broken
	}
	if delta != -1 && delta != 1 {
		return errors.New("无效的移动")
	}
	i := p.index(id)
	if i < 0 {
		return errPhraseGone
	}
	j := i + delta
	if j < 0 || j >= len(p.items) {
		return nil
	}
	items := slices.Clone(p.items)
	items[i], items[j] = items[j], items[i]
	return p.commit(items)
}

// commit makes items current only after they are safely on disk.
func (p *Phrases) commit(items []Phrase) error {
	if p.db != nil {
		if err := p.db.savePhrases(items); err != nil {
			return fmt.Errorf("常用语保存失败：%w", err)
		}
		p.items = items
		return nil
	}
	write := p.write
	if write == nil {
		write = writePrivateJSON
	}
	if err := write(p.path, phraseArchive{1, items}); err != nil {
		if writeReplaced(err) {
			p.items = items
			p.broken = fmt.Errorf("常用语已经更新，但磁盘确认失败；已停止后续修改，请重新启动后检查：%w", err)
		}
		return fmt.Errorf("常用语保存失败：%w", err)
	}
	p.items = items
	return nil
}
func (p *Phrases) newID() string {
	b := make([]byte, 8)
	for {
		rand.Read(b)
		if id := hex.EncodeToString(b); p.index(id) < 0 {
			return id
		}
	}
}
