package main

import (
	"strings"
	"testing"
)

func TestLinkTargetOpensOnlyHTTPLinks(t *testing.T) {
	for _, text := range []string{"https://example.com/a?b=c", "  http://example.com/\n"} {
		e, err := newEntry(text, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := linkTarget(e); err != nil || got != strings.TrimSpace(text) {
			t.Fatalf("linkTarget(%q) = %q, %v", text, got, err)
		}
	}
	for _, text := range []string{"see https://example.com", "javascript:alert(1)", "file:///etc/passwd", "plain text"} {
		e, err := newEntry(text, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := linkTarget(e); err == nil || err.Error() != "只有链接可以打开" {
			t.Fatalf("linkTarget(%q) = %q, %v; want refusal", text, got, err)
		}
	}
	// The kind travels through the bridge; a forged one cannot smuggle in a scheme.
	if _, err := linkTarget(Entry{Kind: "link", Text: "javascript:alert(1)"}); err == nil {
		t.Fatal("forged link kind opened a non-http URL")
	}
}
