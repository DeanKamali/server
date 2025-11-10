package safekeeper

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PeerClient handles HTTP communication with peer Safekeepers
type PeerClient struct {
	client  *http.Client
	timeout time.Duration
}

// NewPeerClient creates a new peer client
func NewPeerClient() *PeerClient {
	return &PeerClient{
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		timeout: 5 * time.Second,
	}
}

// ReplicateWALRequest represents a WAL replication request
type ReplicateWALRequest struct {
	LSN     uint64 `json:"lsn"`
	WALData string `json:"wal_data"` // Base64 encoded
	SpaceID uint32 `json:"space_id,omitempty"`
	PageNo  uint32 `json:"page_no,omitempty"`
}

// ReplicateWALResponse represents a WAL replication response
type ReplicateWALResponse struct {
	Status         string `json:"status"`
	LastAppliedLSN uint64 `json:"last_applied_lsn,omitempty"`
	Error          string `json:"error,omitempty"`
}

// RequestVoteRequest represents a vote request during election
type RequestVoteRequest struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogLSN   uint64 `json:"last_log_lsn"`
	LastLogTerm  uint64 `json:"last_log_term"`
}

// RequestVoteResponse represents a vote response
type RequestVoteResponse struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
}

// HeartbeatRequest represents a heartbeat from leader
type HeartbeatRequest struct {
	Term       uint64 `json:"term"`
	LeaderID   string `json:"leader_id"`
	LatestLSN  uint64 `json:"latest_lsn"`
}

// HeartbeatResponse represents a heartbeat response
type HeartbeatResponse struct {
	Status string `json:"status"`
	Term   uint64 `json:"term"`
}

// SendWALToPeer sends WAL record to a peer Safekeeper
func (pc *PeerClient) SendWALToPeer(peerEndpoint string, lsn uint64, walData []byte, spaceID uint32, pageNo uint32) error {
	url := fmt.Sprintf("%s/api/v1/replicate_wal", peerEndpoint)
	
	walDataBase64 := base64.StdEncoding.EncodeToString(walData)
	reqBody := ReplicateWALRequest{
		LSN:     lsn,
		WALData: walDataBase64,
		SpaceID: spaceID,
		PageNo:  pageNo,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := pc.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("peer returned error: %d - %s", resp.StatusCode, string(body))
	}

	var response ReplicateWALResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Status != "success" {
		return fmt.Errorf("peer replication failed: %s", response.Error)
	}

	return nil
}

// RequestVote requests a vote from a peer during election
func (pc *PeerClient) RequestVote(peerEndpoint string, term uint64, candidateID string, lastLogLSN uint64, lastLogTerm uint64) (bool, uint64, error) {
	url := fmt.Sprintf("%s/api/v1/request_vote", peerEndpoint)
	
	reqBody := RequestVoteRequest{
		Term:        term,
		CandidateID: candidateID,
		LastLogLSN:  lastLogLSN,
		LastLogTerm: lastLogTerm,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return false, 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := pc.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return false, 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, 0, fmt.Errorf("peer returned error: %d - %s", resp.StatusCode, string(body))
	}

	var response RequestVoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return false, 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.VoteGranted, response.Term, nil
}

// SendHeartbeat sends a heartbeat to a peer
func (pc *PeerClient) SendHeartbeat(peerEndpoint string, term uint64, leaderID string, latestLSN uint64) error {
	url := fmt.Sprintf("%s/api/v1/heartbeat", peerEndpoint)
	
	reqBody := HeartbeatRequest{
		Term:      term,
		LeaderID:  leaderID,
		LatestLSN: latestLSN,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := pc.client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("peer returned error: %d - %s", resp.StatusCode, string(body))
	}

	var response HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Term > term {
		// Peer has higher term, we should step down
		return fmt.Errorf("peer has higher term: %d > %d", response.Term, term)
	}

	return nil
}

// GetLatestLSN retrieves the latest LSN from a peer
func (pc *PeerClient) GetLatestLSN(peerEndpoint string) (uint64, error) {
	url := fmt.Sprintf("%s/api/v1/get_latest_lsn", peerEndpoint)
	
	resp, err := pc.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("peer returned error: %d - %s", resp.StatusCode, string(body))
	}

	var response struct {
		Status    string `json:"status"`
		LatestLSN uint64 `json:"latest_lsn"`
		Error     string `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Status != "success" {
		return 0, fmt.Errorf("peer returned error: %s", response.Error)
	}

	return response.LatestLSN, nil
}

// GetMetrics retrieves metrics from a peer
func (pc *PeerClient) GetMetrics(peerEndpoint string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/v1/metrics", peerEndpoint)
	
	resp, err := pc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("peer returned error: %d - %s", resp.StatusCode, string(body))
	}

	var response struct {
		Status  string                 `json:"status"`
		Metrics map[string]interface{} `json:"metrics"`
		Error   string                 `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("peer returned error: %s", response.Error)
	}

	return response.Metrics, nil
}

// GetTimelines retrieves all timelines from a peer
func (pc *PeerClient) GetTimelines(peerEndpoint string) ([]*Timeline, error) {
	url := fmt.Sprintf("%s/api/v1/timelines", peerEndpoint)
	
	resp, err := pc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("peer returned error: %d - %s", resp.StatusCode, string(body))
	}

	var response struct {
		Status    string      `json:"status"`
		Timelines []*Timeline `json:"timelines"`
		Error     string      `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("peer returned error: %s", response.Error)
	}

	return response.Timelines, nil
}

// GetTimeline retrieves a specific timeline from a peer
func (pc *PeerClient) GetTimeline(peerEndpoint string, timelineID string) (*Timeline, error) {
	url := fmt.Sprintf("%s/api/v1/timelines/%s", peerEndpoint, timelineID)
	
	resp, err := pc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("peer returned error: %d - %s", resp.StatusCode, string(body))
	}

	var response struct {
		Status   string    `json:"status"`
		Timeline *Timeline `json:"timeline"`
		Error    string    `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("peer returned error: %s", response.Error)
	}

	return response.Timeline, nil
}

// WALRecordForRecovery represents a WAL record for bulk retrieval
type WALRecordForRecovery struct {
	LSN     uint64
	WALData []byte
	SpaceID uint32
	PageNo  uint32
}

// GetWALRange retrieves a range of WAL records from a peer
func (pc *PeerClient) GetWALRange(peerEndpoint string, startLSN uint64, endLSN uint64) ([]WALRecordForRecovery, error) {
	url := fmt.Sprintf("%s/api/v1/get_wal_range?start_lsn=%d&end_lsn=%d", peerEndpoint, startLSN, endLSN)
	
	resp, err := pc.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("peer returned error: %d - %s", resp.StatusCode, string(body))
	}

	var response struct {
		Status string `json:"status"`
		WALs   []struct {
			LSN     uint64 `json:"lsn"`
			WALData string `json:"wal_data"` // Base64 encoded
			SpaceID uint32 `json:"space_id,omitempty"`
			PageNo  uint32 `json:"page_no,omitempty"`
		} `json:"wals"`
		Error string `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Status != "success" {
		return nil, fmt.Errorf("peer returned error: %s", response.Error)
	}

	// Decode WAL records
	records := make([]WALRecordForRecovery, 0, len(response.WALs))
	for _, wal := range response.WALs {
		walData, err := base64.StdEncoding.DecodeString(wal.WALData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode WAL data for LSN %d: %w", wal.LSN, err)
		}

		records = append(records, WALRecordForRecovery{
			LSN:     wal.LSN,
			WALData: walData,
			SpaceID: wal.SpaceID,
			PageNo:  wal.PageNo,
		})
	}

	return records, nil
}

