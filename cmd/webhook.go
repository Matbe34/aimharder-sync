package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/aimharder-sync/internal/aimharder"
	"github.com/aimharder-sync/internal/config"
	"github.com/aimharder-sync/internal/models"
	"github.com/aimharder-sync/internal/strava"
	"github.com/aimharder-sync/internal/tcx"
)

// WebhookServer handles HTTP triggers for sync
type WebhookServer struct {
	cfg        *config.Config
	port       string
	authToken  string
	syncMutex  sync.Mutex
	lastSync   time.Time
	lastResult *SyncResult
}

// SyncResult holds the result of a sync operation
type SyncResult struct {
	Success     bool      `json:"success"`
	Message     string    `json:"message"`
	Uploaded    int       `json:"uploaded"`
	Skipped     int       `json:"skipped"`
	Errors      int       `json:"errors"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Duration    string    `json:"duration"`
}

// NewWebhookServer creates a new webhook server
func NewWebhookServer(cfg *config.Config, port, authToken string) *WebhookServer {
	return &WebhookServer{
		cfg:       cfg,
		port:      port,
		authToken: authToken,
	}
}

// Start starts the webhook server
func (s *WebhookServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Sync endpoint
	mux.HandleFunc("/sync", s.handleSync)
	mux.HandleFunc("/api/sync", s.handleSync)

	// Status endpoint
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/api/status", s.handleStatus)

	// Health check
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", s.handleHealth)

	server := &http.Server{
		Addr:         ":" + s.port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long timeout for sync
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("🌐 Webhook server listening on port %s\n", s.port)
	fmt.Printf("   POST /sync        - Trigger a sync\n")
	fmt.Printf("   GET  /status      - Get last sync status\n")
	fmt.Printf("   GET  /health      - Health check\n")
	if s.authToken != "" {
		fmt.Printf("   🔒 Authentication required (X-Auth-Token header)\n")
	}

	return server.ListenAndServe()
}

// handleSync handles sync requests
func (s *WebhookServer) handleSync(w http.ResponseWriter, r *http.Request) {
	if s.authToken != "" {
		token := r.Header.Get("X-Auth-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != s.authToken {
			http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	if !s.syncMutex.TryLock() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "sync already in progress",
			"message": "A sync operation is already running. Please wait.",
		})
		return
	}
	defer s.syncMutex.Unlock()

	// Parse days parameter
	days := 1
	if d := r.URL.Query().Get("days"); d != "" {
		fmt.Sscanf(d, "%d", &days)
	}

	// Run sync
	startTime := time.Now()
	result := s.runSync(r.Context(), days)
	result.StartedAt = startTime
	result.CompletedAt = time.Now()
	result.Duration = result.CompletedAt.Sub(startTime).Round(time.Millisecond).String()

	s.lastSync = startTime
	s.lastResult = result

	// Return result
	w.Header().Set("Content-Type", "application/json")
	if result.Success {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	json.NewEncoder(w).Encode(result)
}

// handleStatus returns the last sync status
func (s *WebhookServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.lastResult == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "no_sync_yet",
			"message":   "No sync has been performed yet",
			"last_sync": nil,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"last_sync": s.lastSync,
		"result":    s.lastResult,
	})
}

// handleHealth returns health status
func (s *WebhookServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"service": "aimharder-sync",
		"time":    time.Now(),
	})
}

// runSync performs the actual sync
func (s *WebhookServer) runSync(ctx context.Context, days int) *SyncResult {
	result := &SyncResult{Success: true}

	// Calculate date range
	end := time.Now()
	start := end.AddDate(0, 0, -days)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.Local)
	end = time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, time.Local)

	fmt.Printf("[webhook] 🔄 Syncing workouts from %s to %s\n", start.Format("2006-01-02"), end.Format("2006-01-02"))

	// Login to Aimharder
	ahClient, err := aimharder.NewClient(s.cfg)
	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to create Aimharder client: %v", err)
		return result
	}

	if err := ahClient.Login(); err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to login to Aimharder: %v", err)
		return result
	}

	// Fetch workouts
	workouts, err := ahClient.GetWorkoutHistory(ctx, start, end)
	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to fetch workouts: %v", err)
		return result
	}

	if len(workouts) == 0 {
		result.Message = "No workouts found in date range"
		return result
	}

	// Load sync history
	history := loadSyncHistoryFromFile(s.cfg.Storage.HistoryFile)

	// Filter already synced
	var toSync []models.Workout
	for _, w := range workouts {
		if !isWorkoutSyncedInHistory(history, w.ID) {
			toSync = append(toSync, w)
		}
	}

	if len(toSync) == 0 {
		result.Message = fmt.Sprintf("All %d workouts already synced", len(workouts))
		return result
	}

	// Generate TCX files
	tcxGen := tcx.NewGenerator(s.cfg.Storage.TCXDir)
	tcxFiles, err := tcxGen.GenerateAll(toSync)
	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to generate TCX files: %v", err)
		return result
	}

	// Upload to Strava
	stravaClient, err := strava.NewClient(s.cfg)
	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to create Strava client: %v", err)
		return result
	}

	if err := stravaClient.EnsureValidToken(ctx); err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Strava authentication failed: %v", err)
		return result
	}

	// Fetch existing activities to avoid duplicates
	var minDate, maxDate time.Time
	for _, w := range toSync {
		if minDate.IsZero() || w.Date.Before(minDate) {
			minDate = w.Date
		}
		if maxDate.IsZero() || w.Date.After(maxDate) {
			maxDate = w.Date
		}
	}

	existingActivities, _ := stravaClient.GetActivitiesInRange(ctx, minDate.AddDate(0, 0, -1), maxDate.AddDate(0, 0, 1))

	// Upload each workout
	for i, workout := range toSync {
		if i >= len(tcxFiles) {
			break
		}

		// Check if already exists on Strava
		if existingActivities != nil {
			if existing := stravaClient.ActivityExistsForWorkout(existingActivities, &workout); existing != nil {
				fmt.Printf("[webhook] ⏭️  Skipping %s (exists as %d)\n", workout.Date.Format("2006-01-02"), existing.ID)
				result.Skipped++
				recordSyncInHistory(history, workout.ID, fmt.Sprintf("%d", existing.ID), true, "already_exists")
				continue
			}
		}

		tcxFile := tcxFiles[i]
		fmt.Printf("[webhook] 📤 Uploading %s - %s\n", workout.Date.Format("2006-01-02"), workout.Name)

		uploadResp, err := stravaClient.UploadActivity(ctx, tcxFile, &workout)
		if err != nil {
			fmt.Printf("[webhook] ❌ Upload error: %v\n", err)
			result.Errors++
			recordSyncInHistory(history, workout.ID, "", false, err.Error())
			continue
		}

		status, err := stravaClient.WaitForUpload(ctx, uploadResp.ID, 2*time.Minute)
		if err != nil {
			fmt.Printf("[webhook] ❌ Wait error: %v\n", err)
			result.Errors++
			recordSyncInHistory(history, workout.ID, "", false, err.Error())
			continue
		}

		if status.Error != "" {
			if status.Error == "duplicate" {
				fmt.Printf("[webhook] ⏭️  Duplicate\n")
				result.Skipped++
				recordSyncInHistory(history, workout.ID, "", true, "duplicate")
			} else {
				fmt.Printf("[webhook] ❌ Error: %s\n", status.Error)
				result.Errors++
				recordSyncInHistory(history, workout.ID, "", false, status.Error)
			}
			continue
		}

		fmt.Printf("[webhook] ✅ Created activity %d\n", status.ActivityID)
		result.Uploaded++
		recordSyncInHistory(history, workout.ID, fmt.Sprintf("%d", status.ActivityID), true, "")

		time.Sleep(500 * time.Millisecond)
	}

	// Save history
	saveSyncHistoryToFile(s.cfg.Storage.HistoryFile, history)

	result.Message = fmt.Sprintf("Uploaded %d, skipped %d, errors %d", result.Uploaded, result.Skipped, result.Errors)
	if result.Errors > 0 {
		result.Success = false
	}

	return result
}

// Helper functions for sync history
func loadSyncHistoryFromFile(filepath string) map[string][]models.SyncStatus {
	history := make(map[string][]models.SyncStatus)
	data, err := os.ReadFile(filepath)
	if err != nil {
		return history
	}
	json.Unmarshal(data, &history)
	return history
}

func saveSyncHistoryToFile(filepath string, history map[string][]models.SyncStatus) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath, data, 0644)
}

func isWorkoutSyncedInHistory(history map[string][]models.SyncStatus, workoutID string) bool {
	statuses, ok := history[workoutID]
	if !ok {
		return false
	}
	for _, s := range statuses {
		if s.Success {
			return true
		}
	}
	return false
}

func recordSyncInHistory(history map[string][]models.SyncStatus, workoutID, externalID string, success bool, errorMsg string) {
	status := models.SyncStatus{
		WorkoutID:    workoutID,
		Platform:     "strava",
		ExternalID:   externalID,
		SyncedAt:     time.Now(),
		Success:      success,
		ErrorMessage: errorMsg,
	}
	history[workoutID] = append(history[workoutID], status)
}

// RunWebhookServer is the main entry point for the webhook server
func RunWebhookServer(cfg *config.Config, port, authToken string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n⚠️  Shutting down webhook server...")
		cancel()
	}()

	server := NewWebhookServer(cfg, port, authToken)
	return server.Start(ctx)
}
