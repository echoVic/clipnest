package main

import "strings"

// searchText belongs to the immutable Entry value. The panel's frozen entries
// keep this representation even if the live history changes. It is never
// serialized or keyed solely by ID, so source metadata cannot go stale.
func prepareSearch(e Entry) string {
	return strings.ToLower(e.searchBody() + " " + e.Title + " " + e.Source)
}

// searchBody is the text a search may match: the masked form of a secret.
func (e Entry) searchBody() string {
	if e.Secret != "" {
		return e.Masked
	}
	return e.Text
}
func queryEntries(entries []Entry, q, kind string) []Entry {
	q = strings.ToLower(strings.TrimSpace(q))
	out := []Entry{}
	for _, e := range entries {
		if kind != "all" && kind != "" && e.Kind != kind {
			continue
		}
		if q != "" {
			haystack := e.searchText
			if haystack == "" {
				haystack = prepareSearch(e)
			}
			if !strings.Contains(haystack, q) {
				continue
			}
		}
		out = append(out, e.summary())
	}
	return out
}
