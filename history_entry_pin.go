package main

import (
	"database/sql"
	"errors"
	"slices"
)

const entryPinnedColumn = "entry_pinned INTEGER NOT NULL DEFAULT 0 CHECK (entry_pinned IN (0, 1))"

const entryPinIndexes = `
CREATE INDEX history_pin_order ON history(entry_pinned DESC, position);
CREATE INDEX history_kind_pin_order ON history(kind, entry_pinned DESC, position);
`

func (s *sqliteStore) upgradeEntryPins() error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("ALTER TABLE history ADD COLUMN " + entryPinnedColumn + ";" + entryPinIndexes + "PRAGMA user_version=7")
		return err
	})
}

func (h *History) SetEntryPinned(id string, pinned bool) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.db != nil {
		if err := h.saveLocked(nil); err != nil {
			return err
		}
		return h.db.transaction(func(tx *sql.Tx) error {
			result, err := tx.Exec("UPDATE history SET entry_pinned=? WHERE id=?", pinned, id)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err == nil && count == 0 {
				return errors.New("这条内容已移除")
			}
			return err
		})
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.saveErr != nil {
		return h.saveErr
	}
	index := slices.IndexFunc(h.entries, func(item Entry) bool { return item.ID == id })
	if index < 0 {
		return errors.New("这条内容已移除")
	}
	if h.entries[index].EntryPinned == pinned {
		return nil
	}
	items := slices.Clone(h.entries)
	items[index].EntryPinned = pinned
	err := writePrivateJSON(h.path, archive{Version: 1, Paused: h.paused, Entries: items})
	if err != nil && !writeReplaced(err) {
		return err
	}
	h.entries = items
	h.version++
	h.saved = h.version
	return err
}

func panelHistoryOrder(chronological bool) string {
	if chronological {
		return " ORDER BY h.position"
	}
	return " ORDER BY h.entry_pinned DESC, h.position"
}
