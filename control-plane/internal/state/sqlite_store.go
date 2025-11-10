package state

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/linux/projects/server/control-plane/pkg/types"
	_ "github.com/mattn/go-sqlite3"
)

// Ensure SQLiteStore implements StoreInterface
var _ StoreInterface = (*SQLiteStore)(nil)

// SQLiteStore manages persistent state using SQLite (for local testing)
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite state store
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.initSchema(); err != nil {
		return nil, err
	}

	return store, nil
}

// initSchema creates the database schema
func (s *SQLiteStore) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			created_at TEXT NOT NULL,
			config TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS compute_nodes (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			state TEXT NOT NULL,
			address TEXT,
			created_at TEXT NOT NULL,
			last_activity TEXT NOT NULL,
			config TEXT NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_compute_nodes_project_id ON compute_nodes(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_compute_nodes_state ON compute_nodes(state)`,
		// Billing/usage tracking tables (mimics Neon's consumption metrics)
		`CREATE TABLE IF NOT EXISTS compute_usage (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			compute_id TEXT NOT NULL,
			start_time TIMESTAMP NOT NULL,
			end_time TIMESTAMP,
			seconds INTEGER,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(compute_id) WHERE end_time IS NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_compute_usage_project_id ON compute_usage(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_compute_usage_start_time ON compute_usage(start_time)`,
		`CREATE TABLE IF NOT EXISTS storage_usage (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			storage_type TEXT NOT NULL,
			bytes INTEGER NOT NULL,
			recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_storage_usage_project_id ON storage_usage(project_id)`,
	}

	for _, query := range queries {
		if _, err := s.db.Exec(query); err != nil {
			return err
		}
	}

	return nil
}

// CreateProject creates a new project
func (s *SQLiteStore) CreateProject(project *types.Project) error {
	configJSON, err := json.Marshal(project.Config)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		`INSERT INTO projects (id, name, created_at, config) VALUES (?, ?, ?, ?)`,
		project.ID, project.Name, project.CreatedAt.Format(time.RFC3339), configJSON,
	)

	return err
}

// GetProject retrieves a project by ID
func (s *SQLiteStore) GetProject(id string) (*types.Project, error) {
	var project types.Project
	var configJSON []byte
	var createdAtStr string

	err := s.db.QueryRow(
		`SELECT id, name, created_at, config FROM projects WHERE id = ?`,
		id,
	).Scan(&project.ID, &project.Name, &createdAtStr, &configJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	project.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(configJSON, &project.Config); err != nil {
		return nil, err
	}

	return &project, nil
}

// ListProjects lists all projects
func (s *SQLiteStore) ListProjects() ([]*types.Project, error) {
	rows, err := s.db.Query(`SELECT id, name, created_at, config FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*types.Project
	for rows.Next() {
		var project types.Project
		var configJSON []byte
		var createdAtStr string

		if err := rows.Scan(&project.ID, &project.Name, &createdAtStr, &configJSON); err != nil {
			return nil, err
		}

		project.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(configJSON, &project.Config); err != nil {
			return nil, err
		}

		projects = append(projects, &project)
	}

	return projects, rows.Err()
}

// DeleteProject deletes a project
func (s *SQLiteStore) DeleteProject(id string) error {
	_, err := s.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

// CreateComputeNode creates a new compute node
func (s *SQLiteStore) CreateComputeNode(node *types.ComputeNode) error {
	configJSON, err := json.Marshal(node.Config)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		`INSERT INTO compute_nodes (id, project_id, state, address, created_at, last_activity, config)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.ProjectID, string(node.State), node.Address,
		node.CreatedAt.Format(time.RFC3339), node.LastActivity.Format(time.RFC3339), configJSON,
	)

	return err
}

// GetComputeNode retrieves a compute node by ID
func (s *SQLiteStore) GetComputeNode(id string) (*types.ComputeNode, error) {
	var node types.ComputeNode
	var stateStr string
	var configJSON []byte
	var createdAtStr, lastActivityStr string

	err := s.db.QueryRow(
		`SELECT id, project_id, state, address, created_at, last_activity, config
		 FROM compute_nodes WHERE id = ?`,
		id,
	).Scan(&node.ID, &node.ProjectID, &stateStr, &node.Address, &createdAtStr, &lastActivityStr, &configJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	node.State = types.ComputeState(stateStr)
	node.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, err
	}
	node.LastActivity, err = time.Parse(time.RFC3339, lastActivityStr)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(configJSON, &node.Config); err != nil {
		return nil, err
	}

	return &node, nil
}

// GetComputeNodeByProject retrieves the active compute node for a project
func (s *SQLiteStore) GetComputeNodeByProject(projectID string) (*types.ComputeNode, error) {
	var node types.ComputeNode
	var stateStr string
	var configJSON []byte
	var createdAtStr, lastActivityStr string

	err := s.db.QueryRow(
		`SELECT id, project_id, state, address, created_at, last_activity, config
		 FROM compute_nodes WHERE project_id = ? AND state IN ('active', 'suspended', 'resuming')
		 ORDER BY created_at DESC LIMIT 1`,
		projectID,
	).Scan(&node.ID, &node.ProjectID, &stateStr, &node.Address, &createdAtStr, &lastActivityStr, &configJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	node.State = types.ComputeState(stateStr)
	node.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, err
	}
	node.LastActivity, err = time.Parse(time.RFC3339, lastActivityStr)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(configJSON, &node.Config); err != nil {
		return nil, err
	}

	return &node, nil
}

// UpdateComputeNodeState updates the state of a compute node
func (s *SQLiteStore) UpdateComputeNodeState(id string, state types.ComputeState) error {
	_, err := s.db.Exec(
		`UPDATE compute_nodes SET state = ? WHERE id = ?`,
		string(state), id,
	)
	return err
}

// UpdateComputeNodeActivity updates the last activity time
func (s *SQLiteStore) UpdateComputeNodeActivity(id string) error {
	_, err := s.db.Exec(
		`UPDATE compute_nodes SET last_activity = ? WHERE id = ?`,
		time.Now().Format(time.RFC3339), id,
	)
	return err
}

// ListActiveComputeNodes lists all active compute nodes
func (s *SQLiteStore) ListActiveComputeNodes() ([]*types.ComputeNode, error) {
	rows, err := s.db.Query(
		`SELECT id, project_id, state, address, created_at, last_activity, config
		 FROM compute_nodes WHERE state = 'active'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*types.ComputeNode
	for rows.Next() {
		var node types.ComputeNode
		var stateStr string
		var configJSON []byte
		var createdAtStr, lastActivityStr string

		if err := rows.Scan(&node.ID, &node.ProjectID, &stateStr, &node.Address, &createdAtStr, &lastActivityStr, &configJSON); err != nil {
			return nil, err
		}

		node.State = types.ComputeState(stateStr)
		node.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			return nil, err
		}
		node.LastActivity, err = time.Parse(time.RFC3339, lastActivityStr)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(configJSON, &node.Config); err != nil {
			return nil, err
		}

		nodes = append(nodes, &node)
	}

	return nodes, rows.Err()
}

// DeleteComputeNode deletes a compute node
func (s *SQLiteStore) DeleteComputeNode(id string) error {
	_, err := s.db.Exec(`DELETE FROM compute_nodes WHERE id = ?`, id)
	return err
}

// GetDB returns the underlying database connection
func (s *SQLiteStore) GetDB() *sql.DB {
	return s.db
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
