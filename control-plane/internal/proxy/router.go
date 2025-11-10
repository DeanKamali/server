package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/linux/projects/server/control-plane/internal/compute"
	"github.com/linux/projects/server/control-plane/pkg/types"
)

// Router routes client connections to compute nodes
type Router struct {
	computeManager  *compute.Manager
	controlPlaneURL string
	proxyPort       int
}

// NewRouter creates a new connection router
func NewRouter(computeManager *compute.Manager, controlPlaneURL string, proxyPort int) *Router {
	return &Router{
		computeManager:  computeManager,
		controlPlaneURL: controlPlaneURL,
		proxyPort:       proxyPort,
	}
}

// Start starts the proxy server
func (r *Router) Start() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", r.proxyPort))
	if err != nil {
		return err
	}

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			continue
		}

		go r.handleConnection(clientConn)
	}
}

// handleConnection handles a client connection
// This mimics Neon's proxy connection handling with wake-on-connect
func (r *Router) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	// Extract project ID from MySQL connection (mimics Neon's endpoint extraction)
	// We buffer the handshake packets so we can forward them
	projectID, bufferedData, err := r.extractProjectID(clientConn)
	if err != nil {
		// Send error to client
		r.sendError(clientConn, fmt.Sprintf("Failed to extract project ID: %v", err))
		return
	}

	// Get or wake compute node (mimics Neon's wake_compute flow)
	computeNode, err := r.getOrWakeComputeNode(projectID)
	if err != nil {
		r.sendError(clientConn, fmt.Sprintf("Failed to get/wake compute node: %v", err))
		return
	}

	// Connect to compute node
	computeConn, err := net.DialTimeout("tcp", computeNode.Address, 10*time.Second)
	if err != nil {
		r.sendError(clientConn, fmt.Sprintf("Failed to connect to compute node: %v", err))
		return
	}
	defer computeConn.Close()

	// Forward buffered handshake data to compute node
	if len(bufferedData) > 0 {
		if _, err := computeConn.Write(bufferedData); err != nil {
			r.sendError(clientConn, fmt.Sprintf("Failed to forward handshake: %v", err))
			return
		}
	}

	// Update activity (for suspend scheduler)
	_ = r.computeManager.UpdateComputeNodeActivity(computeNode.ID)

	// Forward traffic bidirectionally (mimics Neon's connection forwarding)
	go io.Copy(computeConn, clientConn)
	io.Copy(clientConn, computeConn)
}

// sendError sends an error message to the client
func (r *Router) sendError(conn net.Conn, message string) {
	// Send MySQL error packet
	errorPacket := r.buildMySQLErrorPacket(1045, "28000", message)
	conn.Write(errorPacket)
}

// extractProjectID extracts project ID from MySQL connection
// Mimics Neon's approach: database name = project ID
// Returns project ID and buffered handshake data
func (r *Router) extractProjectID(conn net.Conn) (string, []byte, error) {
	// Parse MySQL handshake to extract database/project ID
	// This buffers the packets so we can forward them to compute node
	projectID, buffered, err := ExtractProjectIDFromConnection(conn)
	if err != nil {
		return "", nil, fmt.Errorf("failed to extract project ID: %w", err)
	}

	// In our implementation, database name format is: project-<uuid>
	// Or we can use the database name directly as project ID
	return projectID, buffered, nil
}

// getOrWakeComputeNode gets or wakes a compute node for a project
func (r *Router) getOrWakeComputeNode(projectID string) (*types.ComputeNode, error) {
	// Try to get existing compute node
	computeNode, err := r.computeManager.GetComputeNodeByProject(projectID)
	if err == nil {
		// If suspended, resume it
		if computeNode.State == types.StateSuspended {
			computeNode, err = r.computeManager.ResumeComputeNode(computeNode.ID)
			if err != nil {
				return nil, err
			}
		}
		return computeNode, nil
	}

	// If not found, wake compute via control plane API
	// This calls the /wake_compute endpoint which creates/resumes compute nodes
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Call wake_compute API
	wakeResponse, err := r.wakeCompute(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// Get compute node by ID from response
	return r.computeManager.GetComputeNode(wakeResponse.Aux.ComputeID)
}

// wakeCompute calls the control plane wake_compute API
// This mimics Neon's /wake_compute endpoint with retries
func (r *Router) wakeCompute(ctx context.Context, projectID string) (*types.WakeComputeResponse, error) {
	// Call control plane API (mimics Neon's wake_compute endpoint)
	// Format: GET /wake_compute?endpointish=<project-id>
	url := fmt.Sprintf("%s/api/v1/wake_compute?endpointish=%s", r.controlPlaneURL, projectID)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Retry logic (mimics Neon's retry mechanism)
	maxRetries := 3
	var lastErr error
	
	for i := 0; i < maxRetries; i++ {
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if i < maxRetries-1 {
				time.Sleep(time.Duration(i+1) * time.Second) // Exponential backoff
				continue
			}
			return nil, fmt.Errorf("failed to call wake_compute after %d retries: %w", maxRetries, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("wake_compute returned status %d: %s", resp.StatusCode, string(body))
			if i < maxRetries-1 {
				time.Sleep(time.Duration(i+1) * time.Second)
				continue
			}
			return nil, lastErr
		}

		// Parse response
		var wakeResp types.WakeComputeResponse
		if err := json.NewDecoder(resp.Body).Decode(&wakeResp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		return &wakeResp, nil
	}

	return nil, lastErr
}

// buildMySQLErrorPacket builds a MySQL error packet
func (r *Router) buildMySQLErrorPacket(errno uint16, sqlState, message string) []byte {
	// MySQL error packet format:
	// [0xff] [error_code:2] [marker:1] [sql_state:5] [message]
	buf := new(bytes.Buffer)
	buf.WriteByte(0xff) // Error packet marker
	binary.Write(buf, binary.LittleEndian, errno)
	buf.WriteByte(0x23) // Marker '#'
	buf.WriteString(sqlState)
	buf.WriteString(message)
	
	// Prepend packet header (length + sequence)
	packet := buf.Bytes()
	header := make([]byte, 4)
	header[0] = byte(len(packet))
	header[1] = byte(len(packet) >> 8)
	header[2] = byte(len(packet) >> 16)
	header[3] = 0 // Sequence ID
	
	return append(header, packet...)
}
