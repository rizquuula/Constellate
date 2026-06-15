package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sqllib "modernc.org/sqlite"

	"github.com/rizquuula/Constellate/internal/hub/domain/project"
)

// sqliteConstraintUnique is the SQLite extended error code for SQLITE_CONSTRAINT_UNIQUE (2067).
const sqliteConstraintUnique = 2067

// ProjectStore implements projects.ProjectStore against the SQLite projects table.
type ProjectStore struct {
	db *sql.DB
}

// NewProjectStore returns a ProjectStore backed by db.
func NewProjectStore(db *sql.DB) *ProjectStore {
	return &ProjectStore{db: db}
}

// Create inserts a new project record.
// Returns project.ErrDuplicatePath (wrapped) on a UNIQUE(machine_id, path) violation.
func (s *ProjectStore) Create(ctx context.Context, p project.Project) error {
	var color sql.NullString
	if p.Color() != "" {
		color = sql.NullString{String: p.Color(), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO projects (id, machine_id, name, path, color, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, p.ID(), p.MachineID(), p.Name(), p.Path(), color, p.CreatedAt())
	if err != nil {
		var sqliteErr *sqllib.Error
		if errors.As(err, &sqliteErr) {
			switch sqliteErr.Code() {
			case sqliteConstraintUnique:
				return fmt.Errorf("sqlite: create project %q: %w", p.ID(), project.ErrDuplicatePath)
			default:
				if sqliteErr.Code()&0xff == 19 {
					return fmt.Errorf("sqlite: create project %q: %w", p.ID(), project.ErrInvalid)
				}
			}
		}
		return fmt.Errorf("sqlite: create project %q: %w", p.ID(), err)
	}
	return nil
}

// ByID returns a single project by its id.
// Returns project.ErrNotFound (wrapped) if no row matches.
func (s *ProjectStore) ByID(ctx context.Context, id string) (project.Project, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, machine_id, name, path, color, created_at
		FROM projects WHERE id = ?
	`, id)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return project.Project{}, fmt.Errorf("sqlite: by id %q: %w", id, project.ErrNotFound)
	}
	if err != nil {
		return project.Project{}, fmt.Errorf("sqlite: by id %q: %w", id, err)
	}
	return p, nil
}

// List returns all project records ordered by created_at.
func (s *ProjectStore) List(ctx context.Context) ([]project.Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, machine_id, name, path, color, created_at
		FROM projects ORDER BY created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list projects: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return collectProjects(rows)
}

// ListByMachine returns all projects for the given machine ordered by created_at.
func (s *ProjectStore) ListByMachine(ctx context.Context, machineID string) ([]project.Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, machine_id, name, path, color, created_at
		FROM projects WHERE machine_id = ? ORDER BY created_at
	`, machineID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list projects by machine %q: %w", machineID, err)
	}
	defer func() { _ = rows.Close() }()

	return collectProjects(rows)
}

// Delete permanently removes a project record.
// Returns project.ErrNotFound (wrapped) if no row matches.
func (s *ProjectStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete project %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: delete project %q: %w", id, project.ErrNotFound)
	}
	return nil
}

func collectProjects(rows *sql.Rows) ([]project.Project, error) {
	var out []project.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan project row: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: projects rows: %w", err)
	}
	return out, nil
}

func scanProject(s scanner) (project.Project, error) {
	var (
		id, machineID, name, path string
		color                     sql.NullString
		createdAt                 int64
	)
	if err := s.Scan(&id, &machineID, &name, &path, &color, &createdAt); err != nil {
		return project.Project{}, err
	}
	return project.Rehydrate(id, machineID, name, path, color.String, createdAt), nil
}
