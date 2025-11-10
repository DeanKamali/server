package safekeeper

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// HandleRecoverFromPeer handles recovery from a peer Safekeeper
func (h *APIHandler) HandleRecoverFromPeer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PeerEndpoint string `json:"peer_endpoint"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.PeerEndpoint == "" {
		http.Error(w, "peer_endpoint is required", http.StatusBadRequest)
		return
	}

	if err := h.safekeeper.recoveryManager.RecoverFromPeer(req.PeerEndpoint); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("Recovery completed from peer: %s", req.PeerEndpoint),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleRecoverTimeline handles timeline recovery from peers
func (h *APIHandler) HandleRecoverTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TimelineID    string   `json:"timeline_id"`
		PeerEndpoints []string `json:"peer_endpoints"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.TimelineID == "" {
		http.Error(w, "timeline_id is required", http.StatusBadRequest)
		return
	}

	if len(req.PeerEndpoints) == 0 {
		// Use configured peers if not specified
		req.PeerEndpoints = h.safekeeper.peers
	}

	if err := h.safekeeper.recoveryManager.RecoverTimeline(req.TimelineID, req.PeerEndpoints); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"status":  "success",
		"message": fmt.Sprintf("Timeline %s recovered successfully", req.TimelineID),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleGetWALRange handles bulk WAL retrieval for recovery
func (h *APIHandler) HandleGetWALRange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	startLSNStr := r.URL.Query().Get("start_lsn")
	endLSNStr := r.URL.Query().Get("end_lsn")

	if startLSNStr == "" || endLSNStr == "" {
		http.Error(w, "start_lsn and end_lsn parameters are required", http.StatusBadRequest)
		return
	}

	startLSN, err := strconv.ParseUint(startLSNStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid start_lsn parameter", http.StatusBadRequest)
		return
	}

	endLSN, err := strconv.ParseUint(endLSNStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid end_lsn parameter", http.StatusBadRequest)
		return
	}

	if startLSN > endLSN {
		http.Error(w, "start_lsn must be <= end_lsn", http.StatusBadRequest)
		return
	}

	// Limit range to prevent abuse
	if endLSN-startLSN > 1000 {
		http.Error(w, "WAL range too large (max 1000 records)", http.StatusBadRequest)
		return
	}

	// Retrieve WAL records in range
	wals := make([]map[string]interface{}, 0)
	for lsn := startLSN; lsn <= endLSN; lsn++ {
		walData, err := h.safekeeper.GetWAL(lsn)
		if err != nil {
			// Skip missing WAL records
			continue
		}

		wals = append(wals, map[string]interface{}{
			"lsn":      lsn,
			"wal_data": base64.StdEncoding.EncodeToString(walData),
		})
	}

	resp := map[string]interface{}{
		"status": "success",
		"wals":   wals,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleGetTimeline handles retrieval of a specific timeline
func (h *APIHandler) HandleGetTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract timeline ID from URL path (e.g., /api/v1/timelines/{id})
	pathParts := strings.Split(r.URL.Path, "/")
	timelineID := ""
	for i, part := range pathParts {
		if part == "timelines" && i+1 < len(pathParts) {
			timelineID = pathParts[i+1]
			break
		}
	}

	if timelineID == "" {
		http.Error(w, "timeline_id is required", http.StatusBadRequest)
		return
	}

	timeline, err := h.safekeeper.timelineManager.GetTimeline(timelineID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"status": "success",
		"timeline": map[string]interface{}{
			"id":                 timeline.ID,
			"created_at":         timeline.CreatedAt,
			"parent_lsn":         timeline.ParentLSN,
			"parent_timeline_id": timeline.ParentTimelineID,
			"latest_lsn":         timeline.LatestLSN,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
