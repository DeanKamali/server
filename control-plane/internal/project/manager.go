package project

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/linux/projects/server/control-plane/internal/multitenancy"
	"github.com/linux/projects/server/control-plane/internal/state"
	"github.com/linux/projects/server/control-plane/pkg/types"
)

// Manager manages projects
type Manager struct {
	stateStore           state.StoreInterface
	networkPolicyManager *multitenancy.NetworkPolicyManager
}

// NewManager creates a new project manager
func NewManager(stateStore state.StoreInterface) *Manager {
	return &Manager{
		stateStore:           stateStore,
		networkPolicyManager: nil, // Set via SetNetworkPolicyManager
	}
}

// SetNetworkPolicyManager sets the network policy manager for multi-tenancy
func (m *Manager) SetNetworkPolicyManager(npm *multitenancy.NetworkPolicyManager) {
	m.networkPolicyManager = npm
}

// CreateProject creates a new project
func (m *Manager) CreateProject(name string, config types.Config) (*types.Project, error) {
	project := &types.Project{
		ID:        uuid.New().String(),
		Name:      name,
		CreatedAt: time.Now(),
		Config:    config,
	}

	if err := m.stateStore.CreateProject(project); err != nil {
		return nil, err
	}

	// Create network policy for project isolation (mimics Neon's multi-tenancy)
	if m.networkPolicyManager != nil {
		if err := m.networkPolicyManager.CreateProjectNetworkPolicy(
			project.ID,
			project.Config.PageServerURL,
			project.Config.SafekeeperURL,
		); err != nil {
			// Log error but don't fail project creation
			// Network policy creation can be retried later
			fmt.Printf("Warning: failed to create network policy for project %s: %v\n", project.ID, err)
		}
	}

	return project, nil
}

// GetProject retrieves a project by ID
func (m *Manager) GetProject(id string) (*types.Project, error) {
	return m.stateStore.GetProject(id)
}

// ListProjects lists all projects
func (m *Manager) ListProjects() ([]*types.Project, error) {
	return m.stateStore.ListProjects()
}

// DeleteProject deletes a project
func (m *Manager) DeleteProject(id string) error {
	return m.stateStore.DeleteProject(id)
}

