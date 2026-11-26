package main

import (
	"slices"
	"testing"
)

func TestEntryPinPersistsAndRespectsRetention(t *testing.T) {
	dir := t.TempDir()
	store, err := openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	older := entry(t, "match older")
	newer := entry(t, "match newer")
	for _, item := range []Entry{older, newer} {
		if err := store.History.Add(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.History.SetEntryPinned(older.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = openStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	view, err := store.History.OpenView()
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	for _, query := range []string{"", "ma", "match"} {
		first, err := view.Page(query, "text", 0, 1)
		if err != nil || len(first.Entries) != 1 || first.Entries[0].ID != older.ID || !first.Entries[0].EntryPinned {
			t.Fatalf("first match for %q: %+v, %v", query, first, err)
		}
		second, err := view.Page(query, "text", 1, 1)
		if err != nil || len(second.Entries) != 1 || second.Entries[0].ID != newer.ID {
			t.Fatalf("second match for %q: %+v, %v", query, second, err)
		}
		_, olderIndex, err := sequenceRange(view.ctx, view, query, "text", older.ID)
		if err != nil || olderIndex != 1 {
			t.Fatalf("chronological index for %q: %d, %v", query, olderIndex, err)
		}
	}
	items, err := sequenceItems(view, "match", "text", 0, older.ID)
	if err != nil || !slices.EqualFunc(items, []Entry{older, newer}, func(left, right Entry) bool { return left.ID == right.ID }) {
		t.Fatalf("sequence must follow copy order: %+v, %v", items, err)
	}
	view.Close()
	if err := store.History.SetRetention(RetentionPolicy{MaxEntries: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.History.SetEntryPinned(older.ID, false); err == nil {
		t.Fatal("pin protected an old entry from retention")
	}
}
