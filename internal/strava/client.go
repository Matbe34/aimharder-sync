package strava

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"

	"github.com/aimharder-sync/internal/config"
	"github.com/aimharder-sync/internal/models"
)

const (
	authURL    = "https://www.strava.com/oauth/authorize"
	tokenURL   = "https://www.strava.com/oauth/token"
	apiBaseURL = "https://www.strava.com/api/v3"
	uploadURL  = apiBaseURL + "/uploads"
)

// Client handles communication with Strava API
type Client struct {
	config      *config.Config
	httpClient  *http.Client
	oauthConfig *oauth2.Config
	tokens      *models.StravaTokens
	tokenFile   string
}

// NewClient creates a new Strava client
func NewClient(cfg *config.Config) (*Client, error) {
	oauthConfig := &oauth2.Config{
		ClientID:     cfg.Strava.ClientID,
		ClientSecret: cfg.Strava.ClientSecret,
		RedirectURL:  cfg.Strava.RedirectURI,
		Scopes:       []string{"activity:write", "activity:read_all"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  authURL,
			TokenURL: tokenURL,
		},
	}

	client := &Client{
		config:      cfg,
		oauthConfig: oauthConfig,
		tokenFile:   cfg.Storage.TokensFile,
		httpClient:  &http.Client{Timeout: 60 * time.Second},
	}

	// Try to load existing tokens
	if err := client.loadTokens(); err != nil {
		// Tokens not found is OK - user will need to authenticate
		fmt.Printf("Note: No existing Strava tokens found. Run 'auth strava' to authenticate.\n")
	}

	return client, nil
}

// IsAuthenticated returns true if we have valid tokens
func (c *Client) IsAuthenticated() bool {
	return c.tokens != nil && c.tokens.AccessToken != ""
}

// NeedsRefresh returns true if the token needs to be refreshed
func (c *Client) NeedsRefresh() bool {
	if c.tokens == nil {
		return true
	}
	// Refresh if token expires in less than 5 minutes
	return time.Until(c.tokens.ExpiresAt) < 5*time.Minute
}

// GetAuthURL returns the URL for OAuth authorization
// Note: Strava requires comma-separated scopes, not space-separated
func (c *Client) GetAuthURL(state string) string {
	// Build URL manually because Strava needs comma-separated scopes
	return fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
		authURL,
		c.oauthConfig.ClientID,
		c.oauthConfig.RedirectURL,
		"activity:write,activity:read_all",
		state,
	)
}

// ExchangeCode exchanges an authorization code for tokens
func (c *Client) ExchangeCode(ctx context.Context, code string) error {
	token, err := c.oauthConfig.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("failed to exchange code: %w", err)
	}

	// Extract athlete ID from token response (Strava includes it)
	var athleteID int64
	if extra := token.Extra("athlete"); extra != nil {
		if athlete, ok := extra.(map[string]interface{}); ok {
			if id, ok := athlete["id"].(float64); ok {
				athleteID = int64(id)
			}
		}
	}

	c.tokens = &models.StravaTokens{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.Expiry,
		AthleteID:    athleteID,
	}

	return c.saveTokens()
}

// RefreshTokens refreshes the access token using the refresh token
func (c *Client) RefreshTokens(ctx context.Context) error {
	if c.tokens == nil || c.tokens.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	// Create token source from existing tokens
	token := &oauth2.Token{
		AccessToken:  c.tokens.AccessToken,
		RefreshToken: c.tokens.RefreshToken,
		Expiry:       c.tokens.ExpiresAt,
	}

	tokenSource := c.oauthConfig.TokenSource(ctx, token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	c.tokens.AccessToken = newToken.AccessToken
	c.tokens.ExpiresAt = newToken.Expiry
	if newToken.RefreshToken != "" {
		c.tokens.RefreshToken = newToken.RefreshToken
	}

	return c.saveTokens()
}

// EnsureValidToken ensures we have a valid access token
func (c *Client) EnsureValidToken(ctx context.Context) error {
	if !c.IsAuthenticated() {
		return fmt.Errorf("not authenticated with Strava - run 'auth strava' first")
	}

	if c.NeedsRefresh() {
		if err := c.RefreshTokens(ctx); err != nil {
			return fmt.Errorf("failed to refresh Strava token: %w", err)
		}
	}

	return nil
}

// UploadActivity uploads a TCX file to Strava
func (c *Client) UploadActivity(ctx context.Context, tcxPath string, workout *models.Workout) (*UploadResponse, error) {
	if err := c.EnsureValidToken(ctx); err != nil {
		return nil, err
	}

	// Open the TCX file
	file, err := os.Open(tcxPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open TCX file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file
	part, err := writer.CreateFormFile("file", filepath.Base(tcxPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file content: %w", err)
	}

	// Add metadata
	writer.WriteField("data_type", "tcx")

	// Activity type - Strava uses specific strings
	activityType := c.mapWorkoutType(workout.Type)
	writer.WriteField("activity_type", activityType)

	// Name
	if workout.Name != "" {
		writer.WriteField("name", workout.Name)
	} else {
		writer.WriteField("name", fmt.Sprintf("CrossFit WOD - %s", workout.Date.Format("2006-01-02")))
	}

	// Description is added in the TCX notes, but we can also add here
	if workout.Description != "" {
		writer.WriteField("description", workout.Description)
	}

	// External ID to prevent duplicates
	writer.WriteField("external_id", workout.ID)

	writer.Close()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.tokens.AccessToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var uploadResp UploadResponse
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return nil, fmt.Errorf("failed to parse upload response: %w", err)
	}

	return &uploadResp, nil
}

// CheckUploadStatus checks the status of an upload
func (c *Client) CheckUploadStatus(ctx context.Context, uploadID int64) (*UploadStatus, error) {
	if err := c.EnsureValidToken(ctx); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%d", uploadURL, uploadID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.tokens.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var status UploadStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}

	return &status, nil
}

// WaitForUpload waits for an upload to complete
func (c *Client) WaitForUpload(ctx context.Context, uploadID int64, timeout time.Duration) (*UploadStatus, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		status, err := c.CheckUploadStatus(ctx, uploadID)
		if err != nil {
			return nil, err
		}

		if status.Error != "" {
			return status, fmt.Errorf("upload failed: %s", status.Error)
		}

		if status.ActivityID != 0 {
			return status, nil // Upload complete
		}

		if status.Status == "Your activity is ready." {
			return status, nil
		}

		// Wait before checking again
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	return nil, fmt.Errorf("upload timed out")
}

// UpdateActivity updates an existing activity
func (c *Client) UpdateActivity(ctx context.Context, activityID int64, updates map[string]interface{}) error {
	if err := c.EnsureValidToken(ctx); err != nil {
		return err
	}

	url := fmt.Sprintf("%s/activities/%d", apiBaseURL, activityID)

	body, err := json.Marshal(updates)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.tokens.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// GetAthleteActivities gets the athlete's recent activities
func (c *Client) GetAthleteActivities(ctx context.Context, page, perPage int) ([]Activity, error) {
	if err := c.EnsureValidToken(ctx); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/athlete/activities?page=%d&per_page=%d", apiBaseURL, page, perPage)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.tokens.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var activities []Activity
	if err := json.Unmarshal(body, &activities); err != nil {
		return nil, err
	}

	return activities, nil
}

// mapWorkoutType maps internal workout type to Strava activity type
func (c *Client) mapWorkoutType(workoutType models.WorkoutType) string {
	// Strava activity types: https://developers.strava.com/docs/reference/#api-models-ActivityType
	switch workoutType {
	case models.WorkoutTypeStrength:
		return "WeightTraining"
	case models.WorkoutTypeAMRAP, models.WorkoutTypeForTime, models.WorkoutTypeEMOM,
		models.WorkoutTypeTabata, models.WorkoutTypeWOD, models.WorkoutTypeHero,
		models.WorkoutTypeGirl, models.WorkoutTypeOpen:
		return "Crossfit"
	default:
		return "Crossfit"
	}
}

// loadTokens loads tokens from file
func (c *Client) loadTokens() error {
	data, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return err
	}

	var allTokens struct {
		Strava *models.StravaTokens `json:"strava"`
	}

	if err := json.Unmarshal(data, &allTokens); err != nil {
		return err
	}

	c.tokens = allTokens.Strava
	return nil
}

// saveTokens saves tokens to file
func (c *Client) saveTokens() error {
	// Ensure directory exists
	dir := filepath.Dir(c.tokenFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Load existing tokens file if it exists
	var allTokens struct {
		Strava *models.StravaTokens `json:"strava"`
		Garmin interface{}          `json:"garmin,omitempty"`
	}

	if data, err := os.ReadFile(c.tokenFile); err == nil {
		json.Unmarshal(data, &allTokens)
	}

	allTokens.Strava = c.tokens

	data, err := json.MarshalIndent(allTokens, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.tokenFile, data, 0600)
}

// Response types

// UploadResponse represents the initial upload response
type UploadResponse struct {
	ID         int64  `json:"id"`
	ExternalID string `json:"external_id"`
	Error      string `json:"error,omitempty"`
	Status     string `json:"status"`
	ActivityID int64  `json:"activity_id,omitempty"`
}

// UploadStatus represents the status of an upload
type UploadStatus struct {
	ID         int64  `json:"id"`
	ExternalID string `json:"external_id"`
	Error      string `json:"error,omitempty"`
	Status     string `json:"status"`
	ActivityID int64  `json:"activity_id,omitempty"`
}

// Activity represents a Strava activity
type Activity struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Type               string    `json:"type"`
	SportType          string    `json:"sport_type"`
	StartDate          time.Time `json:"start_date"`
	StartDateLocal     time.Time `json:"start_date_local"`
	ElapsedTime        int       `json:"elapsed_time"`
	MovingTime         int       `json:"moving_time"`
	Distance           float64   `json:"distance"`
	TotalElevationGain float64   `json:"total_elevation_gain"`
	ExternalID         string    `json:"external_id"`
}

// ActivityPreview represents what would be uploaded to Strava (for dry-run)
type ActivityPreview struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	SportType   string `json:"sport_type"`
	StartDate   string `json:"start_date"`
	Description string `json:"description"`
	ExternalID  string `json:"external_id"`
	DataType    string `json:"data_type"`
	TCXFile     string `json:"tcx_file,omitempty"`
	ElapsedTime string `json:"elapsed_time,omitempty"`
	WorkoutType string `json:"workout_type,omitempty"`
}

// PreviewActivity creates a preview of what would be uploaded without actually uploading
func (c *Client) PreviewActivity(workout *models.Workout, tcxPath string) *ActivityPreview {
	activityType := c.mapWorkoutType(workout.Type)

	name := workout.Name
	if name == "" {
		name = fmt.Sprintf("CrossFit WOD - %s", workout.Date.Format("2006-01-02"))
	}

	elapsed := ""
	if workout.Duration > 0 {
		elapsed = formatDuration(workout.Duration)
	} else if workout.Result != nil && workout.Result.Time != nil {
		elapsed = formatDuration(*workout.Result.Time)
	}

	return &ActivityPreview{
		Name:        name,
		Type:        activityType,
		SportType:   activityType,
		StartDate:   workout.Date.Format(time.RFC3339),
		Description: workout.Description,
		ExternalID:  workout.ID,
		DataType:    "tcx",
		TCXFile:     tcxPath,
		ElapsedTime: elapsed,
		WorkoutType: string(workout.Type),
	}
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
