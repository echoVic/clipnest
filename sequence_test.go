package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeTimer struct {
	delay    time.Duration
	fn       func()
	canceled bool
	fired    bool
}

// fakeSequenceOps records what the sequence asks of the system, in order.
type fakeSequenceOps struct {
	t           *testing.T
	pressed     func()
	registered  bool
	registerErr error
	self        bool
	writeErr    error
	sendErr     error
	log         []string
	timers      []*fakeTimer
	changes     []string
}

func (f *fakeSequenceOps) ops() sequenceOps {
	return sequenceOps{
		register: func(key string, pressed func()) error {
			if key != "Command+V" {
				f.t.Fatalf("registered %q", key)
			}
			if f.registered {
				f.t.Fatal("registered twice")
			}
			if f.registerErr != nil {
				return f.registerErr
			}
			f.registered, f.pressed = true, pressed
			f.log = append(f.log, "register")
			return nil
		},
		unregister: func(key string) {
			if !f.registered {
				f.t.Fatal("unregistered a key that is not registered")
			}
			f.registered = false
			f.log = append(f.log, "unregister")
		},
		write: func(e Entry) error {
			if f.registered {
				f.t.Fatal("wrote while ⌘V was still registered")
			}
			if f.writeErr != nil {
				return f.writeErr
			}
			f.log = append(f.log, "write "+e.Text)
			return nil
		},
		self: func() bool { return f.self },
		send: func() error {
			if f.registered {
				f.t.Fatal("sent ⌘V while it was registered; the sequence would catch it")
			}
			if f.sendErr != nil {
				return f.sendErr
			}
			f.log = append(f.log, "send")
			return nil
		},
		after: func(delay time.Duration, fn func()) func() {
			timer := &fakeTimer{delay: delay, fn: fn}
			f.timers = append(f.timers, timer)
			return func() { timer.canceled = true }
		},
		changed: func(remaining int, reason string) {
			f.changes = append(f.changes, fmt.Sprintf("%d %s", remaining, reason))
		},
	}
}

// fire runs the pending timers with this delay, as if it had passed. A
// canceled timer may still fire: a timer that already queued its callback on
// the main thread cannot be stopped, so the sequence must ignore it.
func (f *fakeSequenceOps) fire(delay time.Duration, includeCanceled bool) int {
	n := 0
	for _, timer := range append([]*fakeTimer(nil), f.timers...) {
		if timer.delay == delay && !timer.fired && (includeCanceled || !timer.canceled) {
			timer.fired = true
			timer.fn()
			n++
		}
	}
	return n
}

func (f *fakeSequenceOps) take() []string {
	log := f.log
	f.log = nil
	return log
}

func sequenceEntries(texts ...string) []Entry {
	entries := make([]Entry, 0, len(texts))
	for _, text := range texts {
		entries = append(entries, Entry{ID: "id-" + text, Kind: "text", Text: text})
	}
	return entries
}

func newFakeSequence(t *testing.T) (*pasteSequence, *fakeSequenceOps) {
	f := &fakeSequenceOps{t: t}
	return newPasteSequence("Command+V", "⌘V", f.ops()), f
}

func TestSequencePastesEachItemInOrder(t *testing.T) {
	s, f := newFakeSequence(t)
	if err := s.start(sequenceEntries("one", "two", "three")); err != nil {
		t.Fatal(err)
	}
	if got := f.take(); !reflect.DeepEqual(got, []string{"register"}) || s.remaining() != 3 {
		t.Fatal("start", got, s.remaining())
	}
	for i, text := range []string{"one", "two"} {
		f.pressed()
		if got := f.take(); !reflect.DeepEqual(got, []string{"unregister", "write " + text, "send"}) {
			t.Fatalf("press %d: %v", i, got)
		}
		if s.remaining() != 2-i {
			t.Fatal("remaining", s.remaining())
		}
		if f.fire(sequenceRearmDelay, false) != 1 || !reflect.DeepEqual(f.take(), []string{"register"}) {
			t.Fatal("⌘V not registered again after the paste")
		}
	}
	f.pressed()
	if got := f.take(); !reflect.DeepEqual(got, []string{"unregister", "write three", "send"}) {
		t.Fatal("last press", got)
	}
	if f.registered || s.remaining() != 0 || f.fire(sequenceRearmDelay, false) != 0 {
		t.Fatal("⌘V kept after the last item")
	}
	if want := []string{"3 ", "2 ", "1 ", "0 "}; !reflect.DeepEqual(f.changes, want) {
		t.Fatal("changes", f.changes)
	}
}

func TestSequencePassesThroughInItsOwnWindow(t *testing.T) {
	s, f := newFakeSequence(t)
	s.start(sequenceEntries("one", "two"))
	f.take()
	f.self = true
	f.pressed()
	if got := f.take(); !reflect.DeepEqual(got, []string{"unregister", "send"}) || s.remaining() != 2 {
		t.Fatal("own window", got, s.remaining())
	}
	f.fire(sequenceRearmDelay, false)
	f.self = false
	f.pressed()
	if got := f.take(); !reflect.DeepEqual(got, []string{"register", "unregister", "write one", "send"}) {
		t.Fatal("did not resume with the first item", got)
	}
}

func TestSequenceStops(t *testing.T) {
	s, f := newFakeSequence(t)
	s.start(sequenceEntries("one", "two", "three"))
	f.pressed()
	f.take()
	s.stop("")
	if f.registered || s.remaining() != 0 || f.fire(sequenceRearmDelay, true) != 1 || f.registered {
		t.Fatal("stop left ⌘V registered or a late re-register took it again")
	}
	s.stop("")
	if got := f.changes[len(f.changes)-1]; got != "0 " || len(f.changes) != 3 {
		t.Fatal("changes", f.changes)
	}
	// Stopping while registered releases ⌘V at once.
	s.start(sequenceEntries("a", "b"))
	f.take()
	s.stop("")
	if got := f.take(); !reflect.DeepEqual(got, []string{"unregister"}) {
		t.Fatal(got)
	}
}

func TestSequenceStopsAfterTenIdleMinutes(t *testing.T) {
	if sequenceIdleLimit != 10*time.Minute {
		t.Fatal(sequenceIdleLimit)
	}
	s, f := newFakeSequence(t)
	s.start(sequenceEntries("one", "two", "three"))
	f.pressed()
	f.fire(sequenceRearmDelay, false)
	// The press replaced the first idle timer; firing it late does nothing.
	var stale *fakeTimer
	for _, timer := range f.timers {
		if timer.delay == sequenceIdleLimit && timer.canceled {
			stale = timer
		}
	}
	if stale == nil {
		t.Fatal("a press did not restart the idle timer")
	}
	stale.fired = true
	stale.fn()
	if s.remaining() != 2 {
		t.Fatal("a stale idle timer stopped the sequence")
	}
	if f.fire(sequenceIdleLimit, false) != 1 || s.remaining() != 0 || f.registered {
		t.Fatal("idle sequence kept ⌘V")
	}
}

func TestSequenceReleaseLetsAnotherPasteThrough(t *testing.T) {
	s, f := newFakeSequence(t)
	s.release() // nothing to release
	s.start(sequenceEntries("one", "two"))
	f.take()
	s.release()
	if f.registered || !reflect.DeepEqual(f.take(), []string{"unregister"}) {
		t.Fatal("release kept ⌘V")
	}
	s.release() // a second paste while released extends the gap
	f.fire(sequenceRearmDelay, true)
	if !f.registered || s.remaining() != 2 {
		t.Fatal("⌘V not registered again after release", f.registered)
	}
	if n := len(f.take()); n != 1 {
		t.Fatal("registered more than once", n)
	}
}

func TestSequenceStartFailsWhenTheKeyIsTaken(t *testing.T) {
	s, f := newFakeSequence(t)
	f.registerErr = errors.New("taken")
	err := s.start(sequenceEntries("one", "two"))
	if err == nil || err.Error() != "⌘V 被其他应用占用，无法顺序粘贴" || s.remaining() != 0 {
		t.Fatal(err, s.remaining())
	}
	if err := s.start(nil); err == nil {
		t.Fatal("started an empty sequence")
	}
}

func TestSequenceStopsWhenTheKeyIsTakenBetweenPastes(t *testing.T) {
	s, f := newFakeSequence(t)
	s.start(sequenceEntries("one", "two"))
	f.pressed()
	f.registerErr = errors.New("taken")
	f.fire(sequenceRearmDelay, false)
	if s.remaining() != 0 || f.changes[len(f.changes)-1] != "0 ⌘V 被其他应用占用，顺序粘贴已停止" {
		t.Fatal(f.changes)
	}
}

func TestSequenceStopsWhenWritingFails(t *testing.T) {
	s, f := newFakeSequence(t)
	s.start(sequenceEntries("one", "two"))
	f.take()
	f.writeErr = errors.New("clipboard busy")
	f.pressed()
	// The user's ⌘V still pastes what the clipboard already holds.
	if got := f.take(); !reflect.DeepEqual(got, []string{"unregister", "send"}) {
		t.Fatal(got)
	}
	if s.remaining() != 0 || f.fire(sequenceRearmDelay, false) != 0 || !strings.HasSuffix(f.changes[len(f.changes)-1], "顺序粘贴已停止：clipboard busy") {
		t.Fatal(f.changes)
	}
}

func TestSequenceStopsWhenSendingFails(t *testing.T) {
	s, f := newFakeSequence(t)
	s.start(sequenceEntries("one", "two"))
	f.take()
	f.sendErr = errors.New("no permission")
	f.pressed()
	if got := f.take(); !reflect.DeepEqual(got, []string{"unregister", "write one"}) {
		t.Fatal(got)
	}
	if s.remaining() != 0 || f.registered || f.changes[len(f.changes)-1] != "0 顺序粘贴已停止：no permission" {
		t.Fatal(f.changes)
	}
}

func TestSequenceRestartReplacesTheQueue(t *testing.T) {
	s, f := newFakeSequence(t)
	s.start(sequenceEntries("one", "two", "three"))
	f.pressed()
	f.take()
	// Restarting during the gap registers at once; the old re-register is void.
	if err := s.start(sequenceEntries("x", "y")); err != nil {
		t.Fatal(err)
	}
	if !f.registered || f.fire(sequenceRearmDelay, true) != 1 || s.remaining() != 2 {
		t.Fatal("restart", f.registered, s.remaining())
	}
	f.pressed()
	if got := f.take(); !reflect.DeepEqual(got, []string{"register", "unregister", "write x", "send"}) {
		t.Fatal(got)
	}
	// Restarting while registered keeps the registration.
	f.fire(sequenceRearmDelay, false)
	f.take()
	s.start(sequenceEntries("p", "q"))
	if got := f.take(); len(got) != 0 || !f.registered {
		t.Fatal("re-registered a key it already holds", got)
	}
}

// sequenceView records texts oldest first and opens a panel snapshot.
func sequenceView(t *testing.T, texts ...string) *historyView {
	t.Helper()
	s, err := openStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for _, text := range texts {
		s.History.Add(entry(t, text))
	}
	v, err := s.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	return v
}

func sequenceTexts(entries []Entry) []string {
	texts := []string{}
	for _, e := range entries {
		texts = append(texts, e.Text)
	}
	return texts
}

func TestSequenceItemsFollowCopyOrder(t *testing.T) {
	v := sequenceView(t, "one", "two", "https://example.com/three", "four")
	for _, c := range []struct {
		q, kind string
		index   int
		want    []string
	}{
		{"", "", 2, []string{"two", "https://example.com/three", "four"}},
		{"", "all", 1, []string{"https://example.com/three", "four"}},
		{"o", "text", 2, []string{"one", "two", "four"}},
	} {
		page, err := v.Page(c.q, c.kind, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		items, err := sequenceItems(v, c.q, c.kind, c.index, page.Entries[c.index].ID)
		if err != nil || !reflect.DeepEqual(sequenceTexts(items), c.want) {
			t.Errorf("%q %q %d: %v %v", c.q, c.kind, c.index, sequenceTexts(items), err)
		}
	}
}

func TestSequenceItemsRejectStaleOrOversizedQueues(t *testing.T) {
	v := sequenceView(t, "one", "two", "three")
	page, _ := v.Page("", "", 0, 50)
	for name, c := range map[string]struct {
		index int
		id    string
		want  string
	}{
		"selection moved": {2, page.Entries[1].ID, "列表已更新，请重新选择"},
		"past the end":    {3, page.Entries[2].ID, "列表已更新，请重新选择"},
		"first entry":     {0, page.Entries[0].ID, "至少需要 2 条才能顺序粘贴"},
		"too many":        {sequenceMaxItems, page.Entries[2].ID, "顺序粘贴最多 100 条，请从更近的一条开始"},
	} {
		if _, err := sequenceItems(v, "", "", c.index, c.id); err == nil || err.Error() != c.want {
			t.Errorf("%s: %v", name, err)
		}
	}
	if sequenceMaxItems != 100 || sequenceMaxBytes != 64<<20 {
		t.Fatal(sequenceMaxItems, sequenceMaxBytes)
	}
	defer func(limit int) { sequenceMaxBytes = limit }(sequenceMaxBytes)
	sequenceMaxBytes = len("one") + len("two") + len("three") - 1
	if _, err := sequenceItems(v, "", "", 2, page.Entries[2].ID); err == nil || err.Error() != "内容太大，请从更近的一条开始" {
		t.Fatal(err)
	}
	sequenceMaxBytes++
	if items, err := sequenceItems(v, "", "", 2, page.Entries[2].ID); err != nil || len(items) != 3 {
		t.Fatal(items, err)
	}
}
