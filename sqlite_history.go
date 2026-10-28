package main

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

type RetentionPolicy struct {
	MaxMiB   int `json:"maxMiB"`
	KeepDays int `json:"keepDays"`
}

func (p RetentionPolicy) validate() error {
	if p.MaxMiB < 0 || p.MaxMiB > 1<<20 || p.KeepDays < 0 || p.KeepDays > 36500 {
		return errors.New("容量须为 0–1048576 MiB，保留天数须为 0–36500；0 表示不限")
	}
	return nil
}

// Only live, published databases use WAL. Staging remains a single file.
// One reader can freeze a panel while a second connection persists copies.
func (s *sqliteStore) enableLive() error {
	if _, err := s.db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	s.db.SetMaxOpenConns(2)
	return nil
}

func insertEntrySQL(tx *sql.Tx, e Entry, position int64) error {
	_, err := tx.Exec(`INSERT INTO history(id,position,source,created,kind,title,excerpt,width,height,byte_size,source_search,search_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,(SELECT coalesce(max(search_id),0)+1 FROM history))`, e.ID, position, e.Source, e.Created, e.Kind, e.Title, e.Excerpt, e.Width, e.Height, e.size(), strings.ToLower(e.Source))
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO history_content(id,text,image,search_prefix) VALUES(?,?,?,?)",
		e.ID, e.Text, e.Image, strings.ToLower(e.Text+" "+e.Title+" "))
	if err != nil {
		return err
	}
	return replaceSearchSQL(tx, e.ID)
}

type historyChange struct {
	entry  Entry
	remove string
	clear  bool
}

func (s *sqliteStore) apply(changes []historyChange, paused bool, policy *RetentionPolicy) error {
	return s.transaction(func(tx *sql.Tx) error {
		indexRemoved := false
		var position int64
		if err := tx.QueryRow("SELECT coalesce(min(position),0) FROM history").Scan(&position); err != nil {
			return err
		}
		for _, change := range changes {
			switch {
			case change.clear:
				if _, err := tx.Exec("INSERT INTO history_search(history_search) VALUES('delete-all')"); err != nil {
					return err
				}
				if _, err := tx.Exec("DELETE FROM history"); err != nil {
					return err
				}
				indexRemoved = false
			case change.remove != "":
				result, err := tx.Exec("DELETE FROM history WHERE id=?", change.remove)
				if err != nil {
					return err
				}
				n, err := result.RowsAffected()
				if err != nil {
					return err
				}
				indexRemoved = indexRemoved || n > 0
			default:
				e := change.entry
				position--
				var source string
				err := tx.QueryRow("SELECT source FROM history WHERE id=?", e.ID).Scan(&source)
				if errors.Is(err, sql.ErrNoRows) {
					if err := insertEntrySQL(tx, e, position); err != nil {
						return err
					}
					continue
				}
				if err != nil {
					return err
				}
				if _, err := tx.Exec("UPDATE history SET position=?,source=?,source_search=?,created=? WHERE id=?",
					position, e.Source, strings.ToLower(e.Source), e.Created, e.ID); err != nil {
					return err
				}
				if source != e.Source {
					if err := replaceSearchSQL(tx, e.ID); err != nil {
						return err
					}
					indexRemoved = true
				}
			}
		}
		if _, err := tx.Exec("UPDATE settings SET paused=? WHERE id=1 AND paused<>?", paused, paused); err != nil {
			return err
		}
		p := *s.policy.Load()
		if policy != nil {
			p = *policy
			if _, err := tx.Exec("UPDATE settings SET max_mib=?,keep_days=? WHERE id=1", p.MaxMiB, p.KeepDays); err != nil {
				return err
			}
		}
		pruned, err := pruneSQL(tx, p, time.Now())
		if err != nil {
			return err
		}
		if indexRemoved || pruned {
			return purgeSearchSQL(tx)
		}
		return nil
	})
}

func pruneSQL(tx *sql.Tx, p RetentionPolicy, now time.Time) (bool, error) {
	removed := false
	if p.KeepDays > 0 {
		cutoff := now.Add(-time.Duration(p.KeepDays) * 24 * time.Hour).UnixMilli()
		result, err := tx.Exec("DELETE FROM history WHERE created < ?", cutoff)
		if err != nil {
			return false, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		removed = n > 0
	}
	if p.MaxMiB > 0 {
		var total int64
		if err := tx.QueryRow("SELECT coalesce(sum(byte_size),0) FROM history").Scan(&total); err != nil {
			return false, err
		}
		if total > int64(p.MaxMiB)<<20 {
			result, err := tx.Exec(`DELETE FROM history WHERE id IN (
				SELECT id FROM (SELECT id, sum(byte_size) OVER (ORDER BY position) AS used FROM history)
				WHERE used > ?)`, int64(p.MaxMiB)<<20)
			if err != nil {
				return false, err
			}
			n, err := result.RowsAffected()
			return removed || n > 0, err
		}
	}
	return removed, nil
}

func (h *History) SetRetention(p RetentionPolicy) error {
	if err := p.validate(); err != nil {
		return err
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.db == nil {
		return errors.New("迁移到 SQLite 后可设置自动清理")
	}
	return h.saveLocked(&p)
}

func (h *History) Cleanup() error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.db == nil {
		return nil
	}
	return h.db.transaction(func(tx *sql.Tx) error {
		removed, err := pruneSQL(tx, *h.db.policy.Load(), time.Now())
		if err != nil {
			return err
		}
		if removed {
			return purgeSearchSQL(tx)
		}
		return nil
	})
}

// A v1 database was capped at 200 entries; upgrade reads each payload once to
// derive metadata, validates its content identity, then commits all or nothing.
func (s *sqliteStore) upgradeV1() error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			ALTER TABLE history ADD COLUMN kind TEXT NOT NULL DEFAULT '';
			ALTER TABLE history ADD COLUMN title TEXT NOT NULL DEFAULT '';
			ALTER TABLE history ADD COLUMN excerpt TEXT NOT NULL DEFAULT '';
			ALTER TABLE history ADD COLUMN width INTEGER NOT NULL DEFAULT 0;
			ALTER TABLE history ADD COLUMN height INTEGER NOT NULL DEFAULT 0;
			ALTER TABLE history ADD COLUMN byte_size INTEGER NOT NULL DEFAULT 0;
			ALTER TABLE history ADD COLUMN source_search TEXT NOT NULL DEFAULT '';
			ALTER TABLE history_content ADD COLUMN search_prefix TEXT NOT NULL DEFAULT '';
			ALTER TABLE settings ADD COLUMN max_mib INTEGER NOT NULL DEFAULT 512;
			ALTER TABLE settings ADD COLUMN keep_days INTEGER NOT NULL DEFAULT 0;
			CREATE INDEX history_order ON history(position);
			CREATE INDEX history_kind_order ON history(kind,position);
			CREATE INDEX history_age ON history(created);`)
		if err != nil {
			return err
		}
		var after string
		for {
			var id, text, source string
			var image []byte
			err := tx.QueryRow(`SELECT h.id,c.text,c.image,h.source FROM history h LEFT JOIN history_content c ON h.id=c.id
				WHERE h.id>? ORDER BY h.id LIMIT 1`, after).Scan(&id, &text, &image, &source)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				return err
			}
			e, err := newEntry(text, image, source)
			if err != nil {
				return err
			}
			if e.ID != id {
				return errors.New("历史内容与标识不一致")
			}
			_, err = tx.Exec("UPDATE history SET kind=?,title=?,excerpt=?,width=?,height=?,byte_size=?,source_search=? WHERE id=?",
				e.Kind, e.Title, e.Excerpt, e.Width, e.Height, e.size(), strings.ToLower(source), id)
			if err != nil {
				return err
			}
			if _, err := tx.Exec("UPDATE history_content SET search_prefix=? WHERE id=?", strings.ToLower(e.Text+" "+e.Title+" "), id); err != nil {
				return err
			}
			after = id
		}
		_, err = tx.Exec("PRAGMA user_version=2")
		return err
	})
}
