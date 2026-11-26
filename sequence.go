package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	// The synthetic ⌘V must reach the front app before the key is taken again.
	sequenceRearmDelay = 100 * time.Millisecond
	sequenceIdleLimit  = 10 * time.Minute
	sequenceMaxItems   = 100
)

// The whole queue is held in memory until it is pasted.
var sequenceMaxBytes = 64 << 20

func sequenceItems(v *historyView, q, kind string, index int, id string) ([]Entry, error) {
	selected, err := v.Page(q, kind, index, 1)
	if err != nil {
		return nil, err
	}
	if len(selected.Entries) == 0 || selected.Entries[0].ID != id {
		return nil, errors.New("列表已更新，请重新选择")
	}
	entries, index, err := sequenceRange(v.ctx, v, q, kind, id)
	if err != nil {
		return nil, err
	}
	if index < 0 {
		return nil, fmt.Errorf("顺序粘贴最多 %d 条，请从更近的一条开始", sequenceMaxItems)
	}
	if index == 0 {
		return nil, errors.New("至少需要 2 条才能顺序粘贴")
	}
	items := make([]Entry, 0, index+1)
	size := 0
	for i := index; i >= 0; i-- {
		e, err := v.Detail(entries[i].ID)
		if err != nil {
			return nil, err
		}
		if size += e.size(); size > sequenceMaxBytes {
			return nil, errors.New("内容太大，请从更近的一条开始")
		}
		items = append(items, e)
	}
	return items, nil
}

func sequenceRange(ctx context.Context, view *historyView, q, kind, id string) ([]Entry, int, error) {
	page, err := view.page(ctx, q, kind, 0, sequenceMaxItems, true)
	if err != nil {
		return nil, -1, err
	}
	index := slices.IndexFunc(page.Entries, func(item Entry) bool { return item.ID == id })
	return page.Entries, index, nil
}

// sequenceOps is everything a paste sequence asks of the system. Every call,
// including the callbacks after schedules, happens on the main thread.
type sequenceOps struct {
	register   func(accelerator string, pressed func()) error
	unregister func(accelerator string)
	// write puts an entry on the clipboard so that capture does not record it.
	write func(Entry) error
	// self reports whether one of Clipnest's own windows is in front.
	self func() bool
	// send posts the paste keystroke to the app in front.
	send func() error
	// after runs fn on the main thread once delay has passed. Canceling may
	// come too late to stop it, so callbacks check that they are current.
	after   func(delay time.Duration, fn func()) (cancel func())
	changed func(remaining int, reason string)
}

// pasteSequence holds ⌘V while items remain. Each press releases the key,
// pastes the next item and takes the key again after a short gap.
type pasteSequence struct {
	ops         sequenceOps
	key, label  string
	items       []Entry
	next        int
	registered  bool
	rearmGen    uint64
	idleGen     uint64
	cancelRearm func()
	cancelIdle  func()
}

func newPasteSequence(key, label string, ops sequenceOps) *pasteSequence {
	return &pasteSequence{ops: ops, key: key, label: label}
}

func (s *pasteSequence) active() bool { return s.items != nil }

func (s *pasteSequence) remaining() int {
	if !s.active() {
		return 0
	}
	return len(s.items) - s.next
}

// start replaces any running sequence. It takes ⌘V at once, even during the
// gap after a paste, so the first press already pastes the new queue.
func (s *pasteSequence) start(items []Entry) error {
	if len(items) == 0 {
		return errors.New("没有可以顺序粘贴的内容")
	}
	wasActive := s.active()
	s.cancel(&s.rearmGen, &s.cancelRearm)
	s.cancel(&s.idleGen, &s.cancelIdle)
	if !s.registered {
		if err := s.ops.register(s.key, s.press); err != nil {
			s.items, s.next = nil, 0
			if wasActive {
				s.ops.changed(0, "")
			}
			return fmt.Errorf("%s 被其他应用占用，无法顺序粘贴", s.label)
		}
		s.registered = true
	}
	s.items, s.next = items, 0
	s.idle()
	s.ops.changed(s.remaining(), "")
	return nil
}

func (s *pasteSequence) press() {
	if !s.active() {
		return
	}
	s.releaseKey()
	if s.ops.self() {
		_ = s.ops.send()
		s.rearm()
		return
	}
	if err := s.ops.write(s.items[s.next]); err != nil {
		_ = s.ops.send()
		s.stop("顺序粘贴已停止：" + err.Error())
		return
	}
	if err := s.ops.send(); err != nil {
		s.stop("顺序粘贴已停止：" + err.Error())
		return
	}
	s.next++
	if s.next == len(s.items) {
		s.stop("")
		return
	}
	s.rearm()
	s.idle()
	s.ops.changed(s.remaining(), "")
}

// release lets a paste Clipnest sends for another reason reach the front app.
func (s *pasteSequence) release() {
	if !s.active() {
		return
	}
	s.releaseKey()
	s.rearm()
}

// stop ends the sequence and gives ⌘V back. reason, when set, says why it
// ended early.
func (s *pasteSequence) stop(reason string) {
	if !s.active() {
		return
	}
	s.cancel(&s.rearmGen, &s.cancelRearm)
	s.cancel(&s.idleGen, &s.cancelIdle)
	s.releaseKey()
	s.items, s.next = nil, 0
	s.ops.changed(0, reason)
}

func (s *pasteSequence) releaseKey() {
	if s.registered {
		s.ops.unregister(s.key)
		s.registered = false
	}
}

func (s *pasteSequence) rearm() {
	s.schedule(&s.rearmGen, &s.cancelRearm, sequenceRearmDelay, func() {
		if !s.active() || s.registered {
			return
		}
		if err := s.ops.register(s.key, s.press); err != nil {
			s.stop(s.label + " 被其他应用占用，顺序粘贴已停止")
			return
		}
		s.registered = true
	})
}

func (s *pasteSequence) idle() {
	s.schedule(&s.idleGen, &s.cancelIdle, sequenceIdleLimit, func() { s.stop("") })
}

func (s *pasteSequence) schedule(gen *uint64, cancel *func(), delay time.Duration, fn func()) {
	s.cancel(gen, cancel)
	current := *gen
	*cancel = s.ops.after(delay, func() {
		if *gen == current {
			*cancel = nil
			fn()
		}
	})
}

func (s *pasteSequence) cancel(gen *uint64, cancel *func()) {
	*gen++
	if *cancel != nil {
		(*cancel)()
		*cancel = nil
	}
}
