package safekeeper

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

// APIHandler handles HTTP API requests for Safekeeper
type APIHandler struct {
	safekeeper *Safekeeper
	consensus  *Consensus
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(sk *Safekeeper, consensus *Consensus) *APIHandler {
	return &APIHandler{
		safekeeper: sk,
		consensus:  consensus,
	}
}

// StreamWALRequest represents a WAL streaming request
type StreamWALRequest struct {
	LSN     uint64 `json:"lsn"`
	WALData string `json:"wal_data"` // Base64 encoded
	SpaceID uint32 `json:"space_id,omitempty"`
	PageNo  uint32 `json:"page_no,omitempty"`
}

// StreamWALResponse represents a WAL streaming response
type StreamWALResponse struct {
	Status         string `json:"status"`
	LastAppliedLSN uint64 `json:"last_applied_lsn,omitempty"`
	Error          string `json:"error,omitempty"`
}

// GetWALRequest represents a WAL retrieval request
type GetWALRequest struct {
	LSN uint64 `json:"lsn"`
}

// GetWALResponse represents a WAL retrieval response
type GetWALResponse struct {
	Status   string `json:"status"`
	WALData  string `json:"wal_data,omitempty"` // Base64 encoded
	LSN      uint64 `json:"lsn,omitempty"`
	Error    string `json:"error,omitempty"`
}

// MetricsResponse represents metrics response
type MetricsResponse struct {
	Status  string                 `json:"status"`
	Metrics map[string]interface{} `json:"metrics"`
}

// HandleStreamWAL handles WAL streaming requests
func (h *APIHandler) HandleStreamWAL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req StreamWALRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	// Decode base64 WAL data
	walData, err := base64.StdEncoding.DecodeString(req.WALData)
	if err != nil {
		http.Error(w, "Invalid base64 WAL data", http.StatusBadRequest)
		return
	}
	
	// Store WAL with quorum consensus
	if err := h.safekeeper.StoreWAL(req.LSN, walData, req.SpaceID, req.PageNo); err != nil {
		log.Printf("Error storing WAL record: %v", err)
		resp := StreamWALResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to store WAL: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(resp)
		return
	}
	
	log.Printf("Stored WAL record: LSN=%d space=%d page=%d len=%d",
		req.LSN, req.SpaceID, req.PageNo, len(walData))
	
	resp := StreamWALResponse{
		Status:         "success",
		LastAppliedLSN: req.LSN,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleGetWAL handles WAL retrieval requests
func (h *APIHandler) HandleGetWAL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Get LSN from query parameter
	lsnStr := r.URL.Query().Get("lsn")
	if lsnStr == "" {
		http.Error(w, "Missing lsn parameter", http.StatusBadRequest)
		return
	}
	
	lsn, err := strconv.ParseUint(lsnStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid LSN", http.StatusBadRequest)
		return
	}
	
	// Retrieve WAL record
	walData, err := h.safekeeper.GetWAL(lsn)
	if err != nil {
		resp := GetWALResponse{
			Status: "error",
			Error:  fmt.Sprintf("WAL record not found: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(resp)
		return
	}
	
	// Encode as base64
	walDataBase64 := base64.StdEncoding.EncodeToString(walData)
	
	resp := GetWALResponse{
		Status:  "success",
		WALData: walDataBase64,
		LSN:     lsn,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleGetLatestLSN handles latest LSN retrieval
func (h *APIHandler) HandleGetLatestLSN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	latestLSN := h.safekeeper.GetLatestLSN()
	
	resp := map[string]interface{}{
		"status":     "success",
		"latest_lsn": latestLSN,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleMetrics handles metrics requests
func (h *APIHandler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	metrics := h.safekeeper.GetMetrics()
	
	resp := MetricsResponse{
		Status:  "success",
		Metrics: metrics,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandlePing handles health check requests
func (h *APIHandler) HandlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	resp := map[string]interface{}{
		"status":  "ok",
		"version": "1.0.0",
		"state":   h.safekeeper.GetState().String(),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleReplicateWAL handles WAL replication from peer Safekeepers
func (h *APIHandler) HandleReplicateWAL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req StreamWALRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	// Decode base64 WAL data
	walData, err := base64.StdEncoding.DecodeString(req.WALData)
	if err != nil {
		http.Error(w, "Invalid base64 WAL data", http.StatusBadRequest)
		return
	}
	
	// Store locally (replication from peer)
	// Note: Replicated WAL may already be compressed, but we store it as-is
	// The compression flag will be set based on whether we detect it's compressed
	// For simplicity, assume replicated WAL is already compressed if compression is enabled
	isCompressed := h.safekeeper.compressionEnabled
	if err := h.safekeeper.storeWALLocal(req.LSN, walData, isCompressed); err != nil {
		log.Printf("Error storing replicated WAL: %v", err)
		resp := StreamWALResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to store replicated WAL: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(resp)
		return
	}
	
	resp := StreamWALResponse{
		Status:         "success",
		LastAppliedLSN: req.LSN,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleRequestVote handles vote requests during elections
func (h *APIHandler) HandleRequestVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req struct {
		Term        uint64 `json:"term"`
		CandidateID string `json:"candidate_id"`
		LastLogLSN  uint64 `json:"last_log_lsn"`
		LastLogTerm uint64 `json:"last_log_term"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	// Check if we should grant vote
	// Grant vote if: term is higher, or same term and we haven't voted
	h.safekeeper.stateMu.RLock()
	currentTerm := h.safekeeper.term
	currentState := h.safekeeper.state
	h.safekeeper.stateMu.RUnlock()
	
	voteGranted := false
	if req.Term > currentTerm {
		// Higher term - grant vote and update our term
		h.safekeeper.stateMu.Lock()
		h.safekeeper.term = req.Term
		h.safekeeper.state = StateFollower
		h.safekeeper.stateMu.Unlock()
		voteGranted = true
	} else if req.Term == currentTerm && currentState == StateFollower {
		// Same term and we're a follower - grant vote
		voteGranted = true
	}
	
	resp := map[string]interface{}{
		"term":         req.Term,
		"vote_granted": voteGranted,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleHeartbeat handles heartbeat from leader
func (h *APIHandler) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req struct {
		Term      uint64 `json:"term"`
		LeaderID  string `json:"leader_id"`
		LatestLSN uint64 `json:"latest_lsn"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	if err := h.consensus.ReceiveHeartbeat(req.LeaderID, req.Term, req.LatestLSN); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	h.safekeeper.stateMu.RLock()
	currentTerm := h.safekeeper.term
	h.safekeeper.stateMu.RUnlock()
	
	resp := map[string]interface{}{
		"status": "success",
		"term":   currentTerm,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}


// HandleCreateTimeline handles timeline creation
func (h *APIHandler) HandleCreateTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req struct {
		TimelineID      string `json:"timeline_id"`
		ParentLSN       uint64 `json:"parent_lsn,omitempty"`
		ParentTimelineID string `json:"parent_timeline_id,omitempty"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	if req.TimelineID == "" {
		http.Error(w, "timeline_id is required", http.StatusBadRequest)
		return
	}
	
	timeline, err := h.safekeeper.timelineManager.CreateTimeline(
		req.TimelineID,
		req.ParentLSN,
		req.ParentTimelineID,
	)
	
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	resp := map[string]interface{}{
		"status": "success",
		"timeline": map[string]interface{}{
			"id":                timeline.ID,
			"created_at":        timeline.CreatedAt,
			"parent_lsn":        timeline.ParentLSN,
			"parent_timeline_id": timeline.ParentTimelineID,
			"latest_lsn":        timeline.LatestLSN,
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleListTimelines handles timeline listing
func (h *APIHandler) HandleListTimelines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	timelines := h.safekeeper.timelineManager.ListTimelines()
	
	timelineList := make([]map[string]interface{}, 0, len(timelines))
	for _, timeline := range timelines {
		timelineList = append(timelineList, map[string]interface{}{
			"id":                timeline.ID,
			"created_at":        timeline.CreatedAt,
			"parent_lsn":        timeline.ParentLSN,
			"parent_timeline_id": timeline.ParentTimelineID,
			"latest_lsn":        timeline.LatestLSN,
		})
	}
	
	resp := map[string]interface{}{
		"status":   "success",
		"timelines": timelineList,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleAddPeer handles adding a peer replica (dynamic membership)
func (h *APIHandler) HandleAddPeer(w http.ResponseWriter, r *http.Request) {
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
	
	if err := h.safekeeper.membership.AddPeer(req.PeerEndpoint); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	// Update Safekeeper's peer list
	h.safekeeper.peers = h.safekeeper.membership.GetPeers()
	h.safekeeper.quorumSize = h.safekeeper.membership.GetQuorumSize()
	
	resp := map[string]interface{}{
		"status":      "success",
		"peer_count":  len(h.safekeeper.peers),
		"quorum_size": h.safekeeper.quorumSize,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleRemovePeer handles removing a peer replica (dynamic membership)
func (h *APIHandler) HandleRemovePeer(w http.ResponseWriter, r *http.Request) {
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
	
	if err := h.safekeeper.membership.RemovePeer(req.PeerEndpoint); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	// Update Safekeeper's peer list
	h.safekeeper.peers = h.safekeeper.membership.GetPeers()
	h.safekeeper.quorumSize = h.safekeeper.membership.GetQuorumSize()
	
	resp := map[string]interface{}{
		"status":      "success",
		"peer_count":  len(h.safekeeper.peers),
		"quorum_size": h.safekeeper.quorumSize,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
