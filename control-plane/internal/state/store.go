package state

import (
	"database/sql"
	"encoding/json"

	_ "github.com/lib/pq"
	"github.com/linux/projects/server/control-plane/pkg/types"
)

// Store manages persistent state for projects and compute nodes (PostgreSQL)
type Store struct {
	db *sql.DB
}

// Ensure Store implements StoreInterface
var _ StoreInterface = (*Store)(nil)

// NewStore creates a new state store
func NewStore(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	store := &Store{db: db}
	if err := store.initSchema(); err != nil {
		return nil, err
	}

	return store, nil
}

// initSchema creates the database schema
func (s *Store) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id UUID PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			config JSONB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS compute_nodes (
			id UUID PRIMARY KEY,
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			state VARCHAR(50) NOT NULL,
			address VARCHAR(255),
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			last_activity TIMESTAMP NOT NULL DEFAULT NOW(),
			config JSONB NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_compute_nodes_project_id ON compute_nodes(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_compute_nodes_state ON compute_nodes(state)`,
		// Billing/usage tracking tables (mimics Neon's consumption metrics)
		`CREATE TABLE IF NOT EXISTS compute_usage (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			compute_id UUID NOT NULL,
			start_time TIMESTAMP NOT NULL,
			end_time TIMESTAMP,
			seconds BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(compute_id) WHERE end_time IS NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_compute_usage_project_id ON compute_usage(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_compute_usage_start_time ON compute_usage(start_time)`,
		`CREATE TABLE IF NOT EXISTS storage_usage (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			storage_type VARCHAR(50) NOT NULL,
			bytes BIGINT NOT NULL,
			recorded_at TIMESTAMP NOT NULL DEFAULT NOW()
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
func (s *Store) CreateProject(project *types.Project) error {
	configJSON, err := json.Marshal(project.Config)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		`INSERT INTO projects (id, name, created_at, config) VALUES ($1, $2, $3, $4)`,
		project.ID, project.Name, project.CreatedAt, configJSON,
	)

	return err
}

// GetProject retrieves a project by ID
func (s *Store) GetProject(id string) (*types.Project, error) {
	var project types.Project
	var configJSON []byte

	err := s.db.QueryRow(
		`SELECT id, name, created_at, config FROM projects WHERE id = $1`,
		id,
	).Scan(&project.ID, &project.Name, &project.CreatedAt, &configJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(configJSON, &project.Config); err != nil {
		return nil, err
	}

	return &project, nil
}

// ListProjects lists all projects
func (s *Store) ListProjects() ([]*types.Project, error) {
	rows, err := s.db.Query(`SELECT id, name, created_at, config FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*types.Project
	for rows.Next() {
		var project types.Project
		var configJSON []byte

		if err := rows.Scan(&project.ID, &project.Name, &project.CreatedAt, &configJSON); err != nil {
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
func (s *Store) DeleteProject(id string) error {
	_, err := s.db.Exec(`DELETE FROM projects WHERE id = $1`, id)
	return err
}

// CreateComputeNode creates a new compute node
func (s *Store) CreateComputeNode(node *types.ComputeNode) error {
	configJSON, err := json.Marshal(node.Config)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		`INSERT INTO compute_nodes (id, project_id, state, address, created_at, last_activity, config)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		node.ID, node.ProjectID, string(node.State), node.Address, node.CreatedAt, node.LastActivity, configJSON,
	)

	return err
}

// GetComputeNode retrieves a compute node by ID
func (s *Store) GetComputeNode(id string) (*types.ComputeNode, error) {
	var node types.ComputeNode
	var stateStr string
	var configJSON []byte

	err := s.db.QueryRow(
		`SELECT id, project_id, state, address, created_at, last_activity, config
		 FROM compute_nodes WHERE id = $1`,
		id,
	).Scan(&node.ID, &node.ProjectID, &stateStr, &node.Address, &node.CreatedAt, &node.LastActivity, &configJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	node.State = types.ComputeState(stateStr)
	if err := json.Unmarshal(configJSON, &node.Config); err != nil {
		return nil, err
	}

	return &node, nil
}

// GetComputeNodeByProject retrieves the active compute node for a project
func (s *Store) GetComputeNodeByProject(projectID string) (*types.ComputeNode, error) {
	var node types.ComputeNode
	var stateStr string
	var configJSON []byte

	err := s.db.QueryRow(
		`SELECT id, project_id, state, address, created_at, last_activity, config
		 FROM compute_nodes WHERE project_id = $1 AND state IN ('active', 'suspended', 'resuming')
		 ORDER BY created_at DESC LIMIT 1`,
		projectID,
	).Scan(&node.ID, &node.ProjectID, &stateStr, &node.Address, &node.CreatedAt, &node.LastActivity, &configJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	node.State = types.ComputeState(stateStr)
	if err := json.Unmarshal(configJSON, &node.Config); err != nil {
		return nil, err
	}

	return &node, nil
}

// UpdateComputeNodeState updates the state of a compute node
func (s *Store) UpdateComputeNodeState(id string, state types.ComputeState) error {
	_, err := s.db.Exec(
		`UPDATE compute_nodes SET state = $1 WHERE id = $2`,
		string(state), id,
	)
	return err
}

// UpdateComputeNodeActivity updates the last activity time
func (s *Store) UpdateComputeNodeActivity(id string) error {
	_, err := s.db.Exec(
		`UPDATE compute_nodes SET last_activity = NOW() WHERE id = $1`,
		id,
	)
	return err
}

// ListActiveComputeNodes lists all active compute nodes
func (s *Store) ListActiveComputeNodes() ([]*types.ComputeNode, error) {
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

		if err := rows.Scan(&node.ID, &node.ProjectID, &stateStr, &node.Address, &node.CreatedAt, &node.LastActivity, &configJSON); err != nil {
			return nil, err
		}

		node.State = types.ComputeState(stateStr)
		if err := json.Unmarshal(configJSON, &node.Config); err != nil {
			return nil, err
		}

		nodes = append(nodes, &node)
	}

	return nodes, rows.Err()
}

// DeleteComputeNode deletes a compute node
func (s *Store) DeleteComputeNode(id string) error {
	_, err := s.db.Exec(`DELETE FROM compute_nodes WHERE id = $1`, id)
	return err
}

// GetDB returns the underlying database connection
func (s *Store) GetDB() *sql.DB {
	return s.db
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}
