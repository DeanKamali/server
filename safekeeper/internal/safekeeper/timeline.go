package safekeeper

import (
	"fmt"
	"sync"
	"time"
)

// Timeline represents a database timeline (like Neon's timeline concept)
type Timeline struct {
	ID          string
	CreatedAt   time.Time
	ParentLSN   uint64
	ParentTimelineID string
	LatestLSN   uint64
	mu          sync.RWMutex
}

// TimelineManager manages multiple database timelines
type TimelineManager struct {
	timelines map[string]*Timeline
	mu        sync.RWMutex
}

// NewTimelineManager creates a new timeline manager
func NewTimelineManager() *TimelineManager {
	return &TimelineManager{
		timelines: make(map[string]*Timeline),
	}
}

// CreateTimeline creates a new timeline
func (tm *TimelineManager) CreateTimeline(timelineID string, parentLSN uint64, parentTimelineID string) (*Timeline, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.timelines[timelineID]; exists {
		return nil, fmt.Errorf("timeline %s already exists", timelineID)
	}

	timeline := &Timeline{
		ID:              timelineID,
		CreatedAt:       time.Now(),
		ParentLSN:       parentLSN,
		ParentTimelineID: parentTimelineID,
		LatestLSN:       parentLSN,
	}

	tm.timelines[timelineID] = timeline
	return timeline, nil
}

// GetTimeline retrieves a timeline by ID
func (tm *TimelineManager) GetTimeline(timelineID string) (*Timeline, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	timeline, exists := tm.timelines[timelineID]
	if !exists {
		return nil, fmt.Errorf("timeline %s not found", timelineID)
	}

	return timeline, nil
}

// UpdateTimelineLSN updates the latest LSN for a timeline
func (tm *TimelineManager) UpdateTimelineLSN(timelineID string, lsn uint64) error {
	tm.mu.RLock()
	timeline, exists := tm.timelines[timelineID]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("timeline %s not found", timelineID)
	}

	timeline.mu.Lock()
	if lsn > timeline.LatestLSN {
		timeline.LatestLSN = lsn
	}
	timeline.mu.Unlock()

	return nil
}

// ListTimelines returns all timelines
func (tm *TimelineManager) ListTimelines() []*Timeline {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	timelines := make([]*Timeline, 0, len(tm.timelines))
	for _, timeline := range tm.timelines {
		timelines = append(timelines, timeline)
	}

	return timelines
}

// BranchTimeline creates a new timeline branching from an existing one
func (tm *TimelineManager) BranchTimeline(newTimelineID string, fromTimelineID string, atLSN uint64) (*Timeline, error) {
	tm.mu.RLock()
	_, exists := tm.timelines[fromTimelineID]
	tm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("parent timeline %s not found", fromTimelineID)
	}

	return tm.CreateTimeline(newTimelineID, atLSN, fromTimelineID)
}

// DeleteTimeline removes a timeline
func (tm *TimelineManager) DeleteTimeline(timelineID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.timelines[timelineID]; !exists {
		return fmt.Errorf("timeline %s not found", timelineID)
	}

	delete(tm.timelines, timelineID)
	return nil
}

