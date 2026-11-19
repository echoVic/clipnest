package main

import "time"

// pruneItem is a history entry as retention sees it.
type pruneItem struct {
	created, size int64
	imported      bool
}

// predictPrune counts the entries pruneSQL deletes from items, given in
// position order, and how many of them are imported. It takes pruneSQL's
// steps in turn: age, then count, then the running total of sizes.
func predictPrune(items []pruneItem, p RetentionPolicy, now time.Time) (removed, removedImported int) {
	cutoff := now.Add(-time.Duration(p.KeepDays) * 24 * time.Hour).UnixMilli()
	limit := int64(p.MaxMiB) << 20
	kept := 0
	var used int64
	for _, it := range items {
		drop := p.KeepDays > 0 && it.created < cutoff
		if !drop {
			drop = p.MaxEntries > 0 && kept >= p.MaxEntries
		}
		if !drop {
			// The size step sums the rows the count step left, itself included.
			kept++
			used += it.size
			drop = p.MaxMiB > 0 && used > limit
		}
		if drop {
			removed++
			if it.imported {
				removedImported++
			}
		}
	}
	return removed, removedImported
}
