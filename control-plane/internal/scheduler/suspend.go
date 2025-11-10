package scheduler

import (
	"time"

	"github.com/linux/projects/server/control-plane/internal/compute"
	"github.com/linux/projects/server/control-plane/internal/state"
)

// SuspendScheduler monitors and suspends idle compute nodes
type SuspendScheduler struct {
	computeManager *compute.Manager
	stateStore     state.StoreInterface
	idleTimeout    time.Duration
	checkInterval  time.Duration
	stopChan       chan struct{}
}

// NewSuspendScheduler creates a new suspend scheduler
func NewSuspendScheduler(
	computeManager *compute.Manager,
	stateStore state.StoreInterface,
	idleTimeout time.Duration,
	checkInterval time.Duration,
) *SuspendScheduler {
	return &SuspendScheduler{
		computeManager: computeManager,
		stateStore:     stateStore,
		idleTimeout:    idleTimeout,
		checkInterval:  checkInterval,
		stopChan:       make(chan struct{}),
	}
}

// Start starts the suspend scheduler
func (s *SuspendScheduler) Start() {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.checkAndSuspend()
		}
	}
}

// Stop stops the suspend scheduler
func (s *SuspendScheduler) Stop() {
	close(s.stopChan)
}

// checkAndSuspend checks active compute nodes and suspends idle ones
func (s *SuspendScheduler) checkAndSuspend() {
	activeNodes, err := s.stateStore.ListActiveComputeNodes()
	if err != nil {
		return
	}

	now := time.Now()
	for _, node := range activeNodes {
		// Check if node has been idle for longer than timeout
		if now.Sub(node.LastActivity) > s.idleTimeout {
			// Suspend the compute node
			go func(computeID string) {
				if err := s.computeManager.SuspendComputeNode(computeID); err != nil {
					// Log error but continue
				}
			}(node.ID)
		}
	}
}
