package types

import (
	"time"
)

// Project represents a database project/tenant
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Config    Config    `json:"config"`
}

// Config holds project configuration
type Config struct {
	PageServerURL  string `json:"page_server_url"`
	SafekeeperURL  string `json:"safekeeper_url"`
	IdleTimeout   int    `json:"idle_timeout"` // seconds
	MaxConnections int    `json:"max_connections"`
}

// ComputeNode represents a MariaDB compute node
type ComputeNode struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"project_id"`
	State        ComputeState   `json:"state"`
	Address      string         `json:"address"` // host:port
	CreatedAt    time.Time      `json:"created_at"`
	LastActivity time.Time      `json:"last_activity"`
	Config       ComputeConfig  `json:"config"`
}

// ComputeState represents the state of a compute node
type ComputeState string

const (
	StateActive     ComputeState = "active"
	StateSuspending ComputeState = "suspending"
	StateSuspended  ComputeState = "suspended"
	StateResuming   ComputeState = "resuming"
	StateTerminated ComputeState = "terminated"
)

// ComputeConfig holds compute node configuration
type ComputeConfig struct {
	PageServerURL  string `json:"page_server_url"`
	SafekeeperURL  string `json:"safekeeper_url"`
	Image          string `json:"image"` // Docker image
	Resources      Resources `json:"resources"`
}

// Resources defines compute node resources
type Resources struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// WakeComputeRequest is the request to wake a compute node
type WakeComputeRequest struct {
	ProjectID string `json:"project_id"`
	Endpoint  string `json:"endpoint"` // endpoint ID or project ID
}

// WakeComputeResponse is the response from wake_compute
type WakeComputeResponse struct {
	Address    string            `json:"address"` // host:port
	ServerName string            `json:"server_name,omitempty"`
	Aux        MetricsAuxInfo    `json:"aux"`
}

// MetricsAuxInfo holds auxiliary metrics information
type MetricsAuxInfo struct {
	ComputeID string `json:"compute_id"`
	ProjectID string `json:"project_id"`
}



