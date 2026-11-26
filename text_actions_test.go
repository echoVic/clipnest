package main

import "testing"

func TestCaseConversionPreservesLayoutAndRefusesSensitiveEntries(t *testing.T) {
	item := entry(t, "  Straße\nÉCHO\t  ")
	upper, err := convertEntryCase(item, "upper")
	if err != nil || upper.Text != "  STRASSE\nÉCHO\t  " || upper.ID == item.ID {
		t.Fatalf("uppercase: %q, %v", upper.Text, err)
	}
	lower, err := convertEntryCase(item, "lower")
	if err != nil || lower.Text != "  straße\nécho\t  " {
		t.Fatalf("lowercase: %q, %v", lower.Text, err)
	}
	item.Secret = "secret"
	if _, err := convertEntryCase(item, "upper"); err == nil {
		t.Fatal("converted a masked secret")
	}
}

func TestMergeTextFollowsSelectionOrderAndKeepsWhitespace(t *testing.T) {
	first, second := entry(t, " first  "), entry(t, "second")
	merged, err := mergeTextEntries([]Entry{second, first}, "newline")
	if err != nil || merged.Text != "second\n first  " {
		t.Fatalf("merged: %q, %v", merged.Text, err)
	}
	if _, err := mergeTextEntries([]Entry{first, first}, "newline"); err == nil {
		t.Fatal("merged duplicate IDs")
	}
	second.Secret = "secret"
	if _, err := mergeTextEntries([]Entry{first, second}, "newline"); err == nil {
		t.Fatal("merged a masked secret")
	}
}
