// Package storage — store_sqlite.go
// SQLite WAL backend for DocEngineState, behind the StateManager interface.
//
// Design (improvement plan A3):
//   - One SQLite file per storage dir: docs_state.db, opened with
//     PRAGMA journal_mode=WAL and PRAGMA synchronous=NORMAL. WAL gives real
//     MVCC: concurrent readers (check/status/diff) never block a writer,
//     and writers serialize via real transactions instead of lock files.
//   - Schema: documents (one row per DocSpec target) + sections (one row per
//     (target, section_id)) + meta (top-level SchemaVersion/LastCommit/
//     GeneratedAt that have no natural row home).
//   - Dual-backend, SQLite default: StateManager routes to SQLite unless
//     the env var GMB_DOC_STATE=json forces the legacy JSON path in
//     state.go. JSON stays supported explicitly and as the migration source.
//   - Auto-migration: on the first SQLite open, when docs_state.json exists
//     and the database is fresh (all three tables empty), the JSON state is
//     imported inside a transaction. The check-and-migrate critical section
//     is guarded by FlockForFile (lock.go) so two processes racing first
//     open cannot double-migrate.
//   - Save is a single transaction (DELETE all + INSERT all), i.e. the same
//     full-state-write semantics as the JSON backend, atomically.
//
// Driver: modernc.org/sqlite (pure Go, no cgo), registered under the
// database/sql driver name "sqlite".
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// sqliteFileName is the SQLite state file inside the storage directory.
const sqliteFileName = "docs_state.db"

// sqliteSchema creates the three state tables idempotently.
const sqliteSchema = `
CREATE TABLE IF NOT EXISTS documents(
	target TEXT PRIMARY KEY,
	file_hash TEXT NOT NULL DEFAULT '',
	last_updated_commit TEXT NOT NULL DEFAULT '',
	last_updated_at TEXT NOT NULL DEFAULT '',
	freshness_score INTEGER NOT NULL DEFAULT 0,
	commits_behind INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sections(
	target TEXT NOT NULL,
	section_id TEXT NOT NULL,
	ast_hash TEXT NOT NULL DEFAULT '',
	render_mode TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	token_cost INTEGER NOT NULL DEFAULT 0,
	render_ms INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL DEFAULT '',
	last_rendered_body TEXT NOT NULL DEFAULT '',
	PRIMARY KEY(target, section_id)
);
CREATE TABLE IF NOT EXISTS meta(
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
`

// meta keys for the top-level DocEngineState fields.
const (
	metaSchemaVersion = "schema_version"
	metaLastCommit    = "last_commit"
	metaGeneratedAt   = "generated_at"
)

// UsingSQLite reports whether this StateManager routes to the SQLite backend:
// true unless the env var GMB_DOC_STATE=json forces the legacy JSON backend.
// SQLite is the DEFAULT for new StateManagers (gap A3e behavior change —
// previously JSON was the default for one release); JSON remains available
// explicitly via GMB_DOC_STATE=json and as the auto-migration source.
func (sm *StateManager) UsingSQLite() bool {
	return sm.useSQLite()
}

// useSQLite is the internal backend switch: SQLite WAL unless
// GMB_DOC_STATE=json. Auto-migration from docs_state.json still runs on
// first SQLite open (ensureSQLiteMigrated), so existing JSON state is
// imported, never orphaned.
func (sm *StateManager) useSQLite() bool {
	if os.Getenv("GMB_DOC_STATE") == "json" {
		return false
	}
	return true
}

// ExportStateJSON loads the current state through the StateManager (either
// backend) and returns it as indented JSON. It exists for future wiring of
// `doc export --format state` (cmd/ is out of scope for this change); the
// JSON shape is identical to docs_state.json so interchange tooling keeps
// working after the SQLite migration.
func ExportStateJSON(sm *StateManager) ([]byte, error) {
	if sm == nil {
		return nil, fmt.Errorf("doc_engine: ExportStateJSON with nil StateManager")
	}
	state, err := sm.Load()
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("doc_engine: marshaling exported state: %w", err)
	}
	return data, nil
}

// openSQLite opens (creating parents as needed) the state database, applies
// the WAL durability pragmas, and creates the schema. It does NOT run the
// JSON migration; call ensureSQLiteMigrated for that.
//
// Simultaneous cold opens from concurrent runs can fail WAL-mode enablement
// with SQLITE_BUSY before busy_timeout applies, so the whole open sequence
// retries with backoff and only the last error surfaces.
func (sm *StateManager) openSQLite() (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(sm.dbPath), 0755); err != nil {
		return nil, fmt.Errorf("doc_engine: creating storage dir for sqlite: %w", err)
	}
	var db *sql.DB
	var err error
	backoff := 50 * time.Millisecond
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}
		db, err = sm.openSQLiteOnce()
		if err == nil {
			return db, nil
		}
		if !isBusyError(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("doc_engine: opening sqlite state after retries: %w", err)
}

// isBusyError reports whether err is a SQLite contention failure worth
// retrying ("database is locked" / "database table is locked" / SQLITE_BUSY).
func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy")
}

// openSQLiteOnce performs a single open+pragmas+schema attempt.
func (sm *StateManager) openSQLiteOnce() (*sql.DB, error) {
	db, err := sql.Open("sqlite", sm.dbPath)
	if err != nil {
		return nil, fmt.Errorf("doc_engine: opening sqlite state: %w", err)
	}
	// Single-writer state store: one connection avoids SQLITE_BUSY between
	// pooled handles from the same process. Cross-process writers serialize
	// via WAL + busy_timeout below.
	db.SetMaxOpenConns(1)

	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL;`).Scan(&mode); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("doc_engine: enabling sqlite WAL mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA synchronous=NORMAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("doc_engine: setting sqlite synchronous=NORMAL: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("doc_engine: setting sqlite busy_timeout: %w", err)
	}
	if _, err := db.Exec(sqliteSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("doc_engine: creating sqlite schema: %w", err)
	}
	return db, nil
}

// ensureSQLiteMigrated runs the JSON→SQLite auto-migration exactly once: if
// all three tables are empty and docs_state.json exists, the JSON state is
// imported in a transaction. The check-and-migrate section holds
// FlockForFile(dbPath) so concurrent first-opens cannot double-migrate.
func (sm *StateManager) ensureSQLiteMigrated(db *sql.DB) error {
	release, err := FlockForFile(sm.dbPath, filepath.Join(sm.dir, "locks"))
	if err != nil {
		return err
	}
	defer release()

	var docCount, secCount, metaCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM documents;`).Scan(&docCount); err != nil {
		return fmt.Errorf("doc_engine: counting sqlite documents: %w", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sections;`).Scan(&secCount); err != nil {
		return fmt.Errorf("doc_engine: counting sqlite sections: %w", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM meta;`).Scan(&metaCount); err != nil {
		return fmt.Errorf("doc_engine: counting sqlite meta: %w", err)
	}
	if docCount+secCount+metaCount > 0 {
		return nil // already populated — not fresh, nothing to migrate
	}
	if _, err := os.Stat(sm.path); os.IsNotExist(err) {
		return nil // no JSON to migrate from
	} else if err != nil {
		return fmt.Errorf("doc_engine: stating docs_state.json for migration: %w", err)
	}
	legacy, err := sm.loadJSON()
	if err != nil {
		return fmt.Errorf("doc_engine: loading docs_state.json for migration: %w", err)
	}
	if len(legacy.Documents) == 0 && legacy.LastCommit == "" {
		return nil // JSON is empty too — leave the fresh database empty
	}
	if err := writeStateToSQLite(db, legacy); err != nil {
		return fmt.Errorf("doc_engine: migrating docs_state.json to sqlite: %w", err)
	}
	return nil
}

// loadFromSQLite implements Load on the SQLite backend: open → migrate once →
// read all rows into a DocEngineState. A missing/empty database yields an
// empty state, mirroring the JSON backend's missing-file behavior.
func (sm *StateManager) loadFromSQLite() (*DocEngineState, error) {
	db, err := sm.openSQLite()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if err := sm.ensureSQLiteMigrated(db); err != nil {
		return nil, err
	}

	state := &DocEngineState{Documents: make(map[string]*DocumentState)}

	var schemaVersion, lastCommit, generatedAt string
	metaRows, err := db.Query(`SELECT key, value FROM meta;`)
	if err != nil {
		return nil, fmt.Errorf("doc_engine: reading sqlite meta: %w", err)
	}
	for metaRows.Next() {
		var k, v string
		if err := metaRows.Scan(&k, &v); err != nil {
			_ = metaRows.Close()
			return nil, fmt.Errorf("doc_engine: scanning sqlite meta: %w", err)
		}
		switch k {
		case metaSchemaVersion:
			schemaVersion = v
		case metaLastCommit:
			lastCommit = v
		case metaGeneratedAt:
			generatedAt = v
		}
	}
	if err := metaRows.Err(); err != nil {
		_ = metaRows.Close()
		return nil, fmt.Errorf("doc_engine: iterating sqlite meta: %w", err)
	}
	_ = metaRows.Close()

	state.SchemaVersion = 1
	if n, err := strconv.Atoi(schemaVersion); err == nil && n > 0 {
		state.SchemaVersion = n
	}
	state.LastCommit = lastCommit
	state.GeneratedAt = parseSQLiteTime(generatedAt)

	docRows, err := db.Query(`SELECT target, file_hash, last_updated_commit, last_updated_at, freshness_score, commits_behind FROM documents;`)
	if err != nil {
		return nil, fmt.Errorf("doc_engine: reading sqlite documents: %w", err)
	}
	for docRows.Next() {
		var target, fileHash, commit, updatedAt string
		var freshness, behind int
		if err := docRows.Scan(&target, &fileHash, &commit, &updatedAt, &freshness, &behind); err != nil {
			_ = docRows.Close()
			return nil, fmt.Errorf("doc_engine: scanning sqlite documents: %w", err)
		}
		state.Documents[target] = &DocumentState{
			FileHash:          fileHash,
			LastUpdatedCommit: commit,
			LastUpdatedAt:     parseSQLiteTime(updatedAt),
			FreshnessScore:    freshness,
			CommitsBehind:     behind,
			Sections:          make(map[string]*SectionState),
		}
	}
	if err := docRows.Err(); err != nil {
		_ = docRows.Close()
		return nil, fmt.Errorf("doc_engine: iterating sqlite documents: %w", err)
	}
	_ = docRows.Close()

	secRows, err := db.Query(`SELECT target, section_id, ast_hash, render_mode, provider, token_cost, render_ms, updated_at, last_rendered_body FROM sections;`)
	if err != nil {
		return nil, fmt.Errorf("doc_engine: reading sqlite sections: %w", err)
	}
	for secRows.Next() {
		var target, sectionID, astHash, renderMode, provider, updatedAt, body string
		var tokenCost int
		var renderMs int64
		if err := secRows.Scan(&target, &sectionID, &astHash, &renderMode, &provider, &tokenCost, &renderMs, &updatedAt, &body); err != nil {
			_ = secRows.Close()
			return nil, fmt.Errorf("doc_engine: scanning sqlite sections: %w", err)
		}
		ds := GetOrCreateDocState(state, target)
		ds.Sections[sectionID] = &SectionState{
			ASTSubgraphHash:  astHash,
			RenderMode:       renderMode,
			Provider:         provider,
			LastTokenCost:    tokenCost,
			LastRenderMs:     renderMs,
			LastUpdatedAt:    parseSQLiteTime(updatedAt),
			LastRenderedBody: body,
		}
	}
	if err := secRows.Err(); err != nil {
		_ = secRows.Close()
		return nil, fmt.Errorf("doc_engine: iterating sqlite sections: %w", err)
	}
	_ = secRows.Close()

	return state, nil
}

// saveToSQLite implements Save on the SQLite backend: open → migrate once
// (so a concurrent JSON write racing first open is not lost) → replace the
// full state in one transaction.
func (sm *StateManager) saveToSQLite(state *DocEngineState) error {
	db, err := sm.openSQLite()
	if err != nil {
		return err
	}
	defer db.Close()

	if err := sm.ensureSQLiteMigrated(db); err != nil {
		return err
	}

	state.GeneratedAt = time.Now().UTC()
	if err := writeStateToSQLite(db, state); err != nil {
		return err
	}

	// Verify: re-read row counts inside a fresh read and compare against the
	// in-memory state (mirrors the JSON backend's post-write SHA256 verify).
	var docCount, secCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM documents;`).Scan(&docCount); err != nil {
		return fmt.Errorf("doc_engine: verifying sqlite documents: %w", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sections;`).Scan(&secCount); err != nil {
		return fmt.Errorf("doc_engine: verifying sqlite sections: %w", err)
	}
	wantDocs := len(state.Documents)
	wantSecs := 0
	for _, ds := range state.Documents {
		wantSecs += len(ds.Sections)
	}
	if docCount != wantDocs || secCount != wantSecs {
		return fmt.Errorf("doc_engine: sqlite integrity check failed (have %d docs/%d sections, want %d/%d)",
			docCount, secCount, wantDocs, wantSecs)
	}
	return nil
}

// writeStateToSQLite replaces the full persisted state in a single
// transaction (DELETE all + INSERT all). Callers must hold any needed
// migration lock; the transaction itself is the atomicity boundary.
func writeStateToSQLite(db *sql.DB, state *DocEngineState) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("doc_engine: beginning sqlite transaction: %w", err)
	}
	// Roll back on any failure; a committed tx makes Rollback a no-op error
	// which we deliberately ignore.
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM sections;`); err != nil {
		return fmt.Errorf("doc_engine: clearing sqlite sections: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM documents;`); err != nil {
		return fmt.Errorf("doc_engine: clearing sqlite documents: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM meta;`); err != nil {
		return fmt.Errorf("doc_engine: clearing sqlite meta: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?);`,
		metaSchemaVersion, strconv.Itoa(state.SchemaVersion)); err != nil {
		return fmt.Errorf("doc_engine: writing sqlite meta schema_version: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?);`, metaLastCommit, state.LastCommit); err != nil {
		return fmt.Errorf("doc_engine: writing sqlite meta last_commit: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?);`, metaGeneratedAt, formatSQLiteTime(state.GeneratedAt)); err != nil {
		return fmt.Errorf("doc_engine: writing sqlite meta generated_at: %w", err)
	}

	for target, ds := range state.Documents {
		if ds == nil {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO documents(target, file_hash, last_updated_commit, last_updated_at, freshness_score, commits_behind) VALUES(?, ?, ?, ?, ?, ?);`,
			target, ds.FileHash, ds.LastUpdatedCommit, formatSQLiteTime(ds.LastUpdatedAt), ds.FreshnessScore, ds.CommitsBehind); err != nil {
			return fmt.Errorf("doc_engine: writing sqlite document %q: %w", target, err)
		}
		for sectionID, ss := range ds.Sections {
			if ss == nil {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO sections(target, section_id, ast_hash, render_mode, provider, token_cost, render_ms, updated_at, last_rendered_body) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?);`,
				target, sectionID, ss.ASTSubgraphHash, ss.RenderMode, ss.Provider,
				ss.LastTokenCost, ss.LastRenderMs, formatSQLiteTime(ss.LastUpdatedAt), ss.LastRenderedBody); err != nil {
				return fmt.Errorf("doc_engine: writing sqlite section %q/%q: %w", target, sectionID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("doc_engine: committing sqlite state: %w", err)
	}
	return nil
}

// formatSQLiteTime renders a time for storage ("" for zero, else RFC3339Nano
// UTC). Empty strings keep omitempty-style fields (e.g. section
// LastUpdatedAt) distinguishable from real timestamps.
func formatSQLiteTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// parseSQLiteTime is the inverse of formatSQLiteTime; unparseable and empty
// values yield the zero time.
func parseSQLiteTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
