package main

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

func TestPredictPrune(t *testing.T) {
	now := time.Now()
	day := func(n int) int64 { return now.Add(-time.Duration(n) * 24 * time.Hour).UnixMilli() }
	items := []pruneItem{
		{created: day(0), size: 600 << 10},
		{created: day(1), size: 600 << 10, imported: true},
		{created: day(2), size: 10},
		{created: day(9), size: 10, imported: true},
	}
	for _, c := range []struct {
		p                 RetentionPolicy
		removed, imported int
	}{
		{RetentionPolicy{}, 0, 0},
		{RetentionPolicy{KeepDays: 5}, 1, 1},
		{RetentionPolicy{MaxEntries: 2}, 2, 1},
		{RetentionPolicy{MaxMiB: 1}, 3, 2}, // the running total passes 1 MiB at the second entry
		{RetentionPolicy{KeepDays: 5, MaxEntries: 1}, 3, 2},
	} {
		removed, imported := predictPrune(items, c.p, now)
		if removed != c.removed || imported != c.imported {
			t.Errorf("%+v: %d, %d", c.p, removed, imported)
		}
	}
}

// The prediction follows pruneSQL on the same rows.
func TestPredictPruneMatchesPruneSQL(t *testing.T) {
	now := time.Now()
	r := rand.New(rand.NewPCG(1, 2))
	for _, p := range []RetentionPolicy{{}, {KeepDays: 3}, {MaxEntries: 7}, {MaxMiB: 1}, {KeepDays: 5, MaxEntries: 12, MaxMiB: 1}} {
		s := transferStorage(t)
		var items []pruneItem
		err := s.History.db.transaction(func(tx *sql.Tx) error {
			for i := range 40 {
				e, err := newEntry(fmt.Sprintf("%d %s", i, strings.Repeat("x", r.IntN(100_000))), nil, "测试")
				if err != nil {
					return err
				}
				e.Created = now.Add(-time.Duration(r.IntN(10*24)) * time.Hour).UnixMilli()
				items = append(items, pruneItem{created: e.Created, size: int64(e.size())})
				if err := insertEntrySQL(tx, e, int64(i)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		removed, _ := predictPrune(items, p, now)
		var before, after int
		err = s.History.db.transaction(func(tx *sql.Tx) error {
			tx.QueryRow("SELECT count(*) FROM history").Scan(&before)
			if _, err := pruneSQL(tx, p, now); err != nil {
				return err
			}
			return tx.QueryRow("SELECT count(*) FROM history").Scan(&after)
		})
		if err != nil || removed != before-after {
			t.Errorf("%+v: predicted %d, pruned %d (%v)", p, removed, before-after, err)
		}
	}
}
