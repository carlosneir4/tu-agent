package memory

import (
	"fmt"
	"path/filepath"
)

// migrateFromJSON imports a legacy memory.json sitting next to the database,
// exactly once. The JSON file is never modified — it remains as the backup.
// The import runs in one transaction: a partial failure leaves the flag
// unset and no rows behind, so the next Open retries cleanly.
func (s *Store) migrateFromJSON(dbPath string) error {
	done, err := s.meta("migrated_from_json")
	if err != nil {
		return fmt.Errorf("memory.migrateFromJSON: %w", err)
	}
	if done != "" {
		return nil
	}
	jsonPath := filepath.Join(filepath.Dir(dbPath), "memory.json")
	lite, err := loadLiteObservations(jsonPath)
	if err != nil {
		return fmt.Errorf("memory.migrateFromJSON: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("memory.migrateFromJSON: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	for _, l := range lite {
		id := l.ID
		if id == "" {
			genID, genErr := newID()
			if genErr != nil {
				return fmt.Errorf("memory.migrateFromJSON: %w", genErr)
			}
			id = genID
		}
		syncID, err := randomSyncID()
		if err != nil {
			return fmt.Errorf("memory.migrateFromJSON: %w", err)
		}
		ts := l.Timestamp.UTC().Format(timeFormat)
		// insertObsSQL columns end with author, sync_id, imported
		if _, err := tx.Exec(insertObsSQL,
			id, "", "project", "", l.Topic, l.Content, "", l.Source, 1, ts, ts, "", syncID, false); err != nil {
			return fmt.Errorf("memory.migrateFromJSON: importing %s: %w", id, err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO metadata(key, value) VALUES('migrated_from_json', 'true')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		return fmt.Errorf("memory.migrateFromJSON: set flag: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memory.migrateFromJSON: commit: %w", err)
	}
	return nil
}
