package state

import (
	"database/sql"
	
	"github.com/linux/projects/server/control-plane/pkg/types"
)

// StoreInterface defines the interface for state storage
type StoreInterface interface {
	// Project operations
	CreateProject(project *types.Project) error
	GetProject(id string) (*types.Project, error)
	ListProjects() ([]*types.Project, error)
	DeleteProject(id string) error

	// Compute node operations
	CreateComputeNode(node *types.ComputeNode) error
	GetComputeNode(id string) (*types.ComputeNode, error)
	GetComputeNodeByProject(projectID string) (*types.ComputeNode, error)
	UpdateComputeNodeState(id string, state types.ComputeState) error
	UpdateComputeNodeActivity(id string) error
	ListActiveComputeNodes() ([]*types.ComputeNode, error)
	DeleteComputeNode(id string) error

	// Cleanup
	Close() error
	
	// GetDB returns the underlying database connection (for billing/usage tracking)
	GetDB() *sql.DB
}

// ErrNotFound is returned when a resource is not found
var ErrNotFound = &NotFoundError{}

type NotFoundError struct{}

func (e *NotFoundError) Error() string {
	return "not found"
}


