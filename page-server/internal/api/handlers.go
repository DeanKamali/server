package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/linux/projects/server/page-server/internal/server"
	"github.com/linux/projects/server/page-server/internal/storage"
	"github.com/linux/projects/server/page-server/internal/wal"
	"github.com/linux/projects/server/page-server/pkg/types"
)

// RegisterHandlers registers all HTTP handlers for the Page Server
func RegisterHandlers(pageServer *server.PageServer) {
	// Register HTTP handlers with authentication middleware
	http.HandleFunc("/api/v1/get_page", pageServer.Auth.Middleware(handleGetPage(pageServer)))
	http.HandleFunc("/api/v1/get_pages", pageServer.Auth.Middleware(handleGetPages(pageServer))) // Batch endpoint
	http.HandleFunc("/api/v1/stream_wal", pageServer.Auth.Middleware(handleStreamWAL(pageServer)))
	http.HandleFunc("/api/v1/ping", handlePing()) // Ping doesn't require auth
	http.HandleFunc("/api/v1/metrics", pageServer.Auth.Middleware(handleMetrics(pageServer)))
	
	// Time-travel and snapshot endpoints
	http.HandleFunc("/api/v1/time_travel", pageServer.Auth.Middleware(handleTimeTravel(pageServer)))
	http.HandleFunc("/api/v1/snapshots/create", pageServer.Auth.Middleware(handleCreateSnapshot(pageServer)))
	http.HandleFunc("/api/v1/snapshots/list", pageServer.Auth.Middleware(handleListSnapshots(pageServer)))
	http.HandleFunc("/api/v1/snapshots/get", pageServer.Auth.Middleware(handleGetSnapshot(pageServer)))
	http.HandleFunc("/api/v1/snapshots/restore", pageServer.Auth.Middleware(handleRestoreSnapshot(pageServer)))
}

func handleGetPage(pageServer *server.PageServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req types.GetPageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Tier 1: Try memory cache first (hot data)
		pageData, pageLSN, found := pageServer.Cache.Get(req.SpaceID, req.PageNo, req.LSN)

		// If not in cache, load from storage (Tier 2: Disk/LFC, Tier 3: S3)
		if !found {
			var err error
			pageData, pageLSN, err = pageServer.Storage.LoadPage(req.SpaceID, req.PageNo, req.LSN)
			if err != nil {
				resp := types.GetPageResponse{
					Status: "error",
					Error:  fmt.Sprintf("Page not found: space=%d page=%d lsn=%d: %v", req.SpaceID, req.PageNo, req.LSN, err),
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(resp)
				return
			}

			// Store in cache for future requests
			pageServer.Cache.Put(req.SpaceID, req.PageNo, pageLSN, pageData)
		}

		// Base64 encode page data
		pageDataB64 := base64.StdEncoding.EncodeToString(pageData)

		resp := types.GetPageResponse{
			Status:   "success",
			PageData: pageDataB64,
			PageLSN:  pageLSN, // Return actual page LSN
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleGetPages(pageServer *server.PageServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req types.GetPagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Validate request
		if len(req.Pages) == 0 {
			http.Error(w, "No pages requested", http.StatusBadRequest)
			return
		}

		if len(req.Pages) > 1000 {
			http.Error(w, "Too many pages requested (max 1000)", http.StatusBadRequest)
			return
		}

		// Process pages in parallel using goroutines
		responses := make([]types.PageResponse, len(req.Pages))
		var wg sync.WaitGroup
		var mu sync.Mutex
		successCount := 0

		for i, pageReq := range req.Pages {
			wg.Add(1)
			go func(idx int, pr types.PageRequest) {
				defer wg.Done()

				// Try cache first (Tier 1: Memory)
				pageData, pageLSN, found := pageServer.Cache.Get(pr.SpaceID, pr.PageNo, pr.LSN)

				// If not in cache, load from storage (handles Tier 2: Disk/LFC and Tier 3: S3)
				if !found {
					var err error
					pageData, pageLSN, err = pageServer.Storage.LoadPage(pr.SpaceID, pr.PageNo, pr.LSN)
					if err != nil {
						mu.Lock()
						responses[idx] = types.PageResponse{
							SpaceID: pr.SpaceID,
							PageNo:  pr.PageNo,
							Status:  "error",
							Error:   fmt.Sprintf("Page not found: space=%d page=%d lsn=%d", pr.SpaceID, pr.PageNo, pr.LSN),
						}
						mu.Unlock()
						return
					}

					// Store in cache for future requests
					pageServer.Cache.Put(pr.SpaceID, pr.PageNo, pageLSN, pageData)
				}

				// Base64 encode page data
				pageDataB64 := base64.StdEncoding.EncodeToString(pageData)

				mu.Lock()
				responses[idx] = types.PageResponse{
					SpaceID:  pr.SpaceID,
					PageNo:   pr.PageNo,
					Status:   "success",
					PageData: pageDataB64,
					PageLSN:  pageLSN,
				}
				successCount++
				mu.Unlock()
			}(i, pageReq)
		}

		// Wait for all goroutines to complete
		wg.Wait()

		// Determine overall status
		overallStatus := "success"
		if successCount < len(req.Pages) {
			overallStatus = "partial"
		}

		resp := types.GetPagesResponse{
			Pages:  responses,
			Status: overallStatus,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

		log.Printf("Batch request: %d pages requested, %d successful", len(req.Pages), successCount)
	}
}

func handleStreamWAL(pageServer *server.PageServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req types.StreamWALRequest
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

		// Create WAL record
		record := wal.WALRecord{
			LSN:     req.LSN,
			WALData: walData,
			SpaceID: req.SpaceID,
			PageNo:  req.PageNo,
		}

		// Process WAL record (stores and applies to pages)
		if err := pageServer.WALProcessor.ProcessWALRecord(record); err != nil {
			log.Printf("Error processing WAL record: %v", err)
			resp := types.StreamWALResponse{
				Status: "error",
				Error:  fmt.Sprintf("Failed to process WAL: %v", err),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(resp)
			return
		}

		log.Printf("Received and processed WAL record: LSN=%d space=%d page=%d len=%d",
			req.LSN, req.SpaceID, req.PageNo, len(walData))

		resp := types.StreamWALResponse{
			Status:         "success",
			LastAppliedLSN: req.LSN,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handlePing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		resp := types.PingResponse{
			Status:  "ok",
			Version: "1.0.0",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleMetrics(pageServer *server.PageServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cacheStats := pageServer.Cache.Stats()
		latestLSN := pageServer.Storage.GetLatestLSN()

		metrics := map[string]interface{}{
			"cache": cacheStats,
			"storage": map[string]interface{}{
				"latest_lsn": latestLSN,
			},
		}

		// Add hybrid storage statistics if using hybrid storage
		if hybridStorage, ok := pageServer.Storage.(*storage.HybridStorage); ok {
			hybridStats := hybridStorage.GetStats()
			lfcStats := hybridStorage.GetLFC().Stats()
			metrics["tiered_storage"] = map[string]interface{}{
				"tier_1_memory": map[string]interface{}{
					"hits": cacheStats["size"], // Pages in memory cache
				},
				"tier_2_lfc": map[string]interface{}{
					"hits":       hybridStats.LFCHits,
					"misses":     hybridStats.LFCMisses,
					"size_bytes": lfcStats["size_bytes"],
					"max_bytes":  lfcStats["max_size_bytes"],
					"hit_rate":   lfcStats["hit_rate"],
				},
				"tier_3_s3": map[string]interface{}{
					"hits": hybridStats.S3Hits,
				},
				"promotions": hybridStats.Promotions, // Pages promoted to higher tiers
				"demotions":  hybridStats.Demotions, // Pages demoted to lower tiers
			}
			metrics["storage_type"] = "hybrid"
		} else if _, ok := pageServer.Storage.(*storage.S3Storage); ok {
			metrics["storage_type"] = "s3"
		} else {
			metrics["storage_type"] = "file"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metrics)
	}
}

func handleTimeTravel(pageServer *server.PageServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req types.TimeTravelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Load page at the specified LSN (point in time)
		pageData, pageLSN, err := pageServer.Storage.LoadPage(req.SpaceID, req.PageNo, req.LSN)
		if err != nil {
			resp := types.GetPageResponse{
				Status: "error",
				Error:  fmt.Sprintf("Page not found at LSN %d: space=%d page=%d: %v", req.LSN, req.SpaceID, req.PageNo, err),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Base64 encode page data
		pageDataB64 := base64.StdEncoding.EncodeToString(pageData)

		resp := types.GetPageResponse{
			Status:   "success",
			PageData: pageDataB64,
			PageLSN:  pageLSN, // Actual LSN of the page version
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

		log.Printf("Time-travel query: space=%d page=%d requested_lsn=%d actual_lsn=%d",
			req.SpaceID, req.PageNo, req.LSN, pageLSN)
	}
}

func handleCreateSnapshot(pageServer *server.PageServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req types.CreateSnapshotRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Use latest LSN if not specified
		lsn := req.LSN
		if lsn == 0 {
			lsn = pageServer.Storage.GetLatestLSN()
		}

		// Create snapshot
		snapshot, err := pageServer.SnapshotManager.CreateSnapshot(lsn, req.Description)
		if err != nil {
			resp := types.CreateSnapshotResponse{
				Status: "error",
				Error:  fmt.Sprintf("Failed to create snapshot: %v", err),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(resp)
			return
		}

		resp := types.CreateSnapshotResponse{
			Status:   "success",
			Snapshot: snapshot,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

		log.Printf("Snapshot created: id=%s lsn=%d description=%s", snapshot.ID, snapshot.LSN, snapshot.Description)
	}
}

func handleListSnapshots(pageServer *server.PageServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		snapshots := pageServer.SnapshotManager.ListSnapshots()

		resp := types.ListSnapshotsResponse{
			Status:    "success",
			Snapshots: snapshots,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleGetSnapshot(pageServer *server.PageServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get snapshot ID from query parameter
		snapshotID := r.URL.Query().Get("id")
		if snapshotID == "" {
			http.Error(w, "Missing snapshot ID", http.StatusBadRequest)
			return
		}

		snapshot, err := pageServer.SnapshotManager.GetSnapshot(snapshotID)
		if err != nil {
			resp := types.CreateSnapshotResponse{
				Status: "error",
				Error:  err.Error(),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(resp)
			return
		}

		resp := types.CreateSnapshotResponse{
			Status:   "success",
			Snapshot: snapshot,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleRestoreSnapshot(pageServer *server.PageServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req types.RestoreSnapshotRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Get snapshot
		snapshot, err := pageServer.SnapshotManager.GetSnapshot(req.SnapshotID)
		if err != nil {
			resp := map[string]string{
				"status": "error",
				"error":  err.Error(),
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Return snapshot info - actual restore is done by querying pages at snapshot LSN
		resp := map[string]interface{}{
			"status":   "success",
			"message":  "Snapshot restored. Use time-travel queries with LSN to access pages at this point in time.",
			"snapshot": snapshot,
			"usage": map[string]interface{}{
				"lsn": snapshot.LSN,
				"note": "Query pages using get_page or get_pages with lsn=" + fmt.Sprintf("%d", snapshot.LSN),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

		log.Printf("Snapshot restore requested: id=%s lsn=%d", snapshot.ID, snapshot.LSN)
	}
}

