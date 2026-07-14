package memory

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Session is one explicit work session.
type Session struct {
	ID        string
	Project   string
	StartedAt time.Time
	EndedAt   time.Time // zero value while active
	Summary   string
}

// SessionStart opens a new session for project. Any still-open session for the
// project is auto-ended first (composed summary) so at most one is ever active.
// Returns the new session and the previous (now most-recent ended) summary.
func (s *Store) SessionStart(project string) (Session, string, error) {
	if active, found, err := s.activeSession(project); err != nil {
		return Session{}, "", err
	} else if found {
		if _, err := s.endSession(active.ID, project, ""); err != nil {
			return Session{}, "", err
		}
	}
	prev, err := s.LastSummary(project)
	if err != nil {
		return Session{}, "", err
	}
	now := time.Now().UTC()
	id, err := newID()
	if err != nil {
		return Session{}, "", fmt.Errorf("memory.Store.SessionStart: %w", err)
	}
	sess := Session{ID: id, Project: project, StartedAt: now}
	if _, err := s.db.Exec(
		`INSERT INTO sessions(id, project, started_at, ended_at, summary) VALUES(?,?,?,NULL,'')`,
		sess.ID, sess.Project, now.Format(timeFormat)); err != nil {
		return Session{}, "", fmt.Errorf("memory.Store.SessionStart: %w", err)
	}
	return sess, prev, nil
}

// SessionEnd closes the active session for project, composing a summary from the
// session's observations when summary is empty. Errors if no session is active.
func (s *Store) SessionEnd(project, summary string) (Session, error) {
	active, found, err := s.activeSession(project)
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, fmt.Errorf("memory.Store.SessionEnd: no active session for %q", project)
	}
	return s.endSession(active.ID, project, summary)
}

func (s *Store) endSession(id, project, summary string) (Session, error) {
	var startedStr string
	if err := s.db.QueryRow(`SELECT started_at FROM sessions WHERE id = ?`, id).Scan(&startedStr); err != nil {
		return Session{}, fmt.Errorf("memory.Store.endSession: load: %w", err)
	}
	started, _ := time.Parse(timeParseFormat, startedStr)
	now := time.Now().UTC()
	if summary == "" {
		summary = s.composeSummary(started, now)
	}
	if _, err := s.db.Exec(`UPDATE sessions SET ended_at = ?, summary = ? WHERE id = ?`,
		now.Format(timeFormat), summary, id); err != nil {
		return Session{}, fmt.Errorf("memory.Store.endSession: update: %w", err)
	}
	return Session{ID: id, Project: project, StartedAt: started, EndedAt: now, Summary: summary}, nil
}

// composeSummary builds a deterministic summary from observations created in
// [start, end]. The memory DB is repo-local, so no project filter is needed.
func (s *Store) composeSummary(start, end time.Time) string {
	rows, err := s.db.Query(`SELECT topic_key, title FROM observations
		WHERE deleted_at IS NULL AND created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC, id DESC LIMIT 10`,
		start.Format(timeFormat), end.Format(timeFormat))
	if err != nil {
		return "(no observations recorded)"
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var tk, title string
		if err := rows.Scan(&tk, &title); err != nil {
			continue
		}
		label := tk
		if label == "" {
			label = title
		}
		if label != "" {
			items = append(items, label)
		}
	}
	if len(items) == 0 {
		return "(no observations recorded)"
	}
	return fmt.Sprintf("%d observation(s): %s", len(items), strings.Join(items, "; "))
}

func (s *Store) activeSession(project string) (Session, bool, error) {
	var sess Session
	var started string
	err := s.db.QueryRow(`SELECT id, project, started_at FROM sessions
		WHERE project = ? AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1`, project).
		Scan(&sess.ID, &sess.Project, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("memory.Store.activeSession: %w", err)
	}
	sess.StartedAt, _ = time.Parse(timeParseFormat, started)
	return sess, true, nil
}

// LastSummary returns the most recent ended session's summary for project ("" if none).
func (s *Store) LastSummary(project string) (string, error) {
	var summary string
	err := s.db.QueryRow(`SELECT summary FROM sessions
		WHERE project = ? AND ended_at IS NOT NULL ORDER BY ended_at DESC LIMIT 1`, project).Scan(&summary)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("memory.Store.LastSummary: %w", err)
	}
	return summary, nil
}

// SessionList returns the most recent n sessions for project, newest first.
func (s *Store) SessionList(project string, n int) ([]Session, error) {
	if n <= 0 {
		n = 10
	}
	rows, err := s.db.Query(`SELECT id, project, started_at, ended_at, summary FROM sessions
		WHERE project = ? ORDER BY started_at DESC, id DESC LIMIT ?`, project, n)
	if err != nil {
		return nil, fmt.Errorf("memory.Store.SessionList: %w", err)
	}
	defer rows.Close()
	out := make([]Session, 0, n)
	for rows.Next() {
		var sess Session
		var started string
		var ended sql.NullString
		if err := rows.Scan(&sess.ID, &sess.Project, &started, &ended, &sess.Summary); err != nil {
			return nil, fmt.Errorf("memory.Store.SessionList: scan: %w", err)
		}
		sess.StartedAt, _ = time.Parse(timeParseFormat, started)
		if ended.Valid && ended.String != "" {
			sess.EndedAt, _ = time.Parse(timeParseFormat, ended.String)
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}
