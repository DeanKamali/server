package api

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/linux/projects/server/control-plane/internal/compute"
	"github.com/linux/projects/server/control-plane/internal/project"
	"github.com/linux/projects/server/control-plane/internal/scheduler"
	"github.com/linux/projects/server/control-plane/pkg/types"
)

// Handler handles HTTP API requests
type Handler struct {
	projectManager *project.Manager
	computeManager *compute.Manager
	suspendScheduler *scheduler.SuspendScheduler
}

// NewHandler creates a new API handler
func NewHandler(
	projectManager *project.Manager,
	computeManager *compute.Manager,
	suspendScheduler *scheduler.SuspendScheduler,
) *Handler {
	return &Handler{
		projectManager:   projectManager,
		computeManager:   computeManager,
		suspendScheduler: suspendScheduler,
	}
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(router *gin.Engine) {
	v1 := router.Group("/api/v1")
	{
		// Project endpoints
		v1.POST("/projects", h.CreateProject)
		v1.GET("/projects", h.ListProjects)
		v1.GET("/projects/:id", h.GetProject)
		v1.DELETE("/projects/:id", h.DeleteProject)

		// Compute node endpoints
		v1.POST("/projects/:id/compute", h.CreateComputeNode)
		v1.GET("/compute/:id", h.GetComputeNode)
		v1.DELETE("/compute/:id", h.DestroyComputeNode)
		v1.POST("/compute/:id/suspend", h.SuspendComputeNode)
		v1.POST("/compute/:id/resume", h.ResumeComputeNode)

		// Wake compute endpoint (used by proxy)
		v1.GET("/wake_compute", h.WakeCompute)
	}
}

// CreateProject creates a new project
func (h *Handler) CreateProject(c *gin.Context) {
	var req struct {
		Name   string        `json:"name" binding:"required"`
		Config types.Config  `json:"config" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	project, err := h.projectManager.CreateProject(req.Name, req.Config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, project)
}

// ListProjects lists all projects
func (h *Handler) ListProjects(c *gin.Context) {
	projects, err := h.projectManager.ListProjects()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, projects)
}

// GetProject retrieves a project
func (h *Handler) GetProject(c *gin.Context) {
	project, err := h.projectManager.GetProject(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	c.JSON(http.StatusOK, project)
}

// DeleteProject deletes a project
func (h *Handler) DeleteProject(c *gin.Context) {
	if err := h.projectManager.DeleteProject(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "project deleted"})
}

// CreateComputeNode creates a new compute node
func (h *Handler) CreateComputeNode(c *gin.Context) {
	projectID := c.Param("id")

	var req struct {
		Config types.ComputeConfig `json:"config"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		// Use default config if not provided
		req.Config = types.ComputeConfig{
			Image: "mariadb:latest",
			Resources: types.Resources{
				CPU:    "500m",
				Memory: "1Gi",
			},
		}
	}

	computeNode, err := h.computeManager.CreateComputeNode(projectID, req.Config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, computeNode)
}

// GetComputeNode retrieves a compute node
func (h *Handler) GetComputeNode(c *gin.Context) {
	computeNode, err := h.computeManager.GetComputeNode(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "compute node not found"})
		return
	}

	c.JSON(http.StatusOK, computeNode)
}

// DestroyComputeNode destroys a compute node
func (h *Handler) DestroyComputeNode(c *gin.Context) {
	if err := h.computeManager.DestroyComputeNode(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "compute node destroyed"})
}

// SuspendComputeNode suspends a compute node
func (h *Handler) SuspendComputeNode(c *gin.Context) {
	if err := h.computeManager.SuspendComputeNode(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "compute node suspended"})
}

// ResumeComputeNode resumes a compute node
func (h *Handler) ResumeComputeNode(c *gin.Context) {
	computeNode, err := h.computeManager.ResumeComputeNode(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, computeNode)
}

// WakeCompute wakes up a compute node (used by proxy)
func (h *Handler) WakeCompute(c *gin.Context) {
	endpoint := c.Query("endpointish")
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint parameter required"})
		return
	}

	// Try to get compute node by project ID (endpoint can be project ID)
	computeNode, err := h.computeManager.GetComputeNodeByProject(endpoint)
	if err != nil {
		// If not found, create a new one
		project, err := h.projectManager.GetProject(endpoint)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}

		computeNode, err = h.computeManager.CreateComputeNode(endpoint, types.ComputeConfig{
			PageServerURL:  project.Config.PageServerURL,
			SafekeeperURL:  project.Config.SafekeeperURL,
			Image:          os.Getenv("MARIADB_PAGESERVER_IMAGE"),
			Resources:      types.Resources{CPU: "100m", Memory: "256Mi"},
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// If suspended, resume it
	if computeNode.State == types.StateSuspended {
		computeNode, err = h.computeManager.ResumeComputeNode(computeNode.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// Update last activity
	_ = h.computeManager.UpdateComputeNodeActivity(computeNode.ID)

	// Return wake compute response
	response := types.WakeComputeResponse{
		Address: computeNode.Address,
		Aux: types.MetricsAuxInfo{
			ComputeID: computeNode.ID,
			ProjectID: computeNode.ProjectID,
		},
	}

	c.JSON(http.StatusOK, response)
}

