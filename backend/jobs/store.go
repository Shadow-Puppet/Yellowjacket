package jobs

import (
	"log/slog"
	"time"

	"yellowjacket/backend/database"
)

// Persisted is a job whose paused state outlived the process that
// created it.  The owning subsystem adopts these back into the registry
// during startup, re-attaching the controls needed to resume.
type Persisted struct {
	ID       string `json:"id"`
	Kind     Kind   `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}

// Store persists durable job state to the job_state table.
type Store struct {
	db     *database.DB
	logger *slog.Logger
}

// NewStore creates a job state store backed by the application database.
func NewStore(db *database.DB, logger *slog.Logger) *Store {
	return &Store{db: db, logger: logger}
}

// SetPaused records that a job is paused.
func (s *Store) SetPaused(p Persisted) {
	if s == nil || s.db == nil {
		return
	}

	if _, err := s.db.ExecContext(
		`INSERT OR REPLACE INTO job_state`+
			` (id, kind, title, subtitle, paused_at)`+
			` VALUES (?, ?, ?, ?, ?)`,
		p.ID, string(p.Kind), p.Title, p.Subtitle,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		s.logger.Warn("jobs: could not persist paused job",
			"id", p.ID, "err", err)
	}
}

// ClearPaused removes a job's durable pause record.
func (s *Store) ClearPaused(id string) {
	if s == nil || s.db == nil {
		return
	}

	if _, err := s.db.ExecContext(
		"DELETE FROM job_state WHERE id = ?", id,
	); err != nil {
		s.logger.Warn("jobs: could not clear paused job",
			"id", id, "err", err)
	}
}

// PausedEntries returns every persisted paused job of the given kind.
func (s *Store) PausedEntries(kind Kind) []Persisted {
	if s == nil || s.db == nil {
		return nil
	}

	rows, err := s.db.QueryContext(
		`SELECT id, kind, title, subtitle FROM job_state`+
			` WHERE kind = ? ORDER BY paused_at`,
		string(kind),
	)
	if err != nil {
		s.logger.Warn("jobs: could not read paused jobs",
			"kind", kind, "err", err)

		return nil
	}

	defer func() { _ = rows.Close() }()

	var out []Persisted

	for rows.Next() {
		var (
			p        Persisted
			kindText string
		)

		if scanErr := rows.Scan(
			&p.ID, &kindText, &p.Title, &p.Subtitle,
		); scanErr != nil {
			s.logger.Warn("jobs: could not scan paused job", "err", scanErr)

			continue
		}

		p.Kind = Kind(kindText)
		out = append(out, p)
	}

	return out
}

// IsPaused reports whether the given job ID has a durable pause record.
// Subsystems check this before auto-starting work at launch.
func (s *Store) IsPaused(id string) bool {
	if s == nil || s.db == nil {
		return false
	}

	rows, err := s.db.QueryContext(
		"SELECT 1 FROM job_state WHERE id = ?", id,
	)
	if err != nil {
		return false
	}

	defer func() { _ = rows.Close() }()

	return rows.Next()
}
