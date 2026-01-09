package garmin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aimharder-sync/internal/config"
	"github.com/aimharder-sync/internal/models"
)

const (
	// Garmin SSO endpoints
	ssoURL          = "https://sso.garmin.com/sso/signin"
	ssoEmbedURL     = "https://sso.garmin.com/sso/embed"
	connectURL      = "https://connect.garmin.com"
	modernURL       = "https://connect.garmin.com/modern"
	uploadURL       = "https://connect.garmin.com/upload-service/upload/.tcx"
	activityListURL = "https://connect.garmin.com/activitylist-service/activities/search/activities"
)

// Client handles communication with Garmin Connect
type Client struct {
	config     *config.Config
	httpClient *http.Client
	tokens     *GarminTokens
	tokenFile  string
	loggedIn   bool
}

// GarminTokens holds session tokens for Garmin Connect
type GarminTokens struct {
	SessionCookies []*http.Cookie `json:"session_cookies"`
	DisplayName    string         `json:"display_name,omitempty"`
	ExpiresAt      time.Time      `json:"expires_at"`
}

// UploadResponse represents the response from Garmin upload API
type UploadResponse struct {
	DetailedImportResult DetailedImportResult `json:"detailedImportResult"`
}

// DetailedImportResult contains upload details
type DetailedImportResult struct {
	UploadID   int64      `json:"uploadId"`
	UploadUUID string     `json:"uploadUuid"`
	Owner      int64      `json:"owner"`
	FileSize   int64      `json:"fileSize"`
	Successes  []Activity `json:"successes"`
	Failures   []Failure  `json:"failures"`
}

// Activity represents a Garmin activity
type Activity struct {
	InternalID int64  `json:"internalId"`
	ExternalID string `json:"externalId"`
}

// Failure represents an upload failure
type Failure struct {
	InternalID int64  `json:"internalId"`
	ExternalID string `json:"externalId"`
	Messages   []struct {
		Code    int    `json:"code"`
		Content string `json:"content"`
	} `json:"messages"`
}

// ActivityPreview holds preview information for dry-run
type ActivityPreview struct {
	Name        string
	Description string
	StartTime   time.Time
	Duration    time.Duration
	TCXFile     string
	WorkoutID   string
}

// NewClient creates a new Garmin Connect client
func NewClient(cfg *config.Config) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	client := &Client{
		config:    cfg,
		tokenFile: cfg.Storage.TokensFile,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Jar:     jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to 10 redirects
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}

	// Try to load existing session
	if err := client.loadTokens(); err == nil && client.tokens != nil {
		// Check if session is still valid
		if time.Now().Before(client.tokens.ExpiresAt) {
			// Restore cookies to jar
			connectURL, _ := url.Parse(connectURL)
			client.httpClient.Jar.SetCookies(connectURL, client.tokens.SessionCookies)
			client.loggedIn = true
		}
	}

	return client, nil
}

// IsAuthenticated returns true if we have a valid session
func (c *Client) IsAuthenticated() bool {
	return c.loggedIn && c.tokens != nil && time.Now().Before(c.tokens.ExpiresAt)
}

// Login authenticates with Garmin Connect using email/password
func (c *Client) Login(ctx context.Context) error {
	email := c.config.Garmin.Email
	password := c.config.Garmin.Password

	if email == "" || password == "" {
		return fmt.Errorf("garmin email and password are required (set GARMIN_EMAIL and GARMIN_PASSWORD)")
	}

	fmt.Println("🔐 Logging into Garmin Connect...")

	// Step 1: Get the SSO login page to obtain CSRF token
	fmt.Println("  [1/4] Getting SSO login page...")
	csrfToken, err := c.getCSRFToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get CSRF token: %w", err)
	}

	// Step 2: Submit login credentials
	fmt.Println("  [2/4] Submitting credentials...")
	ticket, err := c.submitLogin(ctx, email, password, csrfToken)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Step 3: Exchange ticket for session
	fmt.Println("  [3/4] Exchanging ticket for session...")
	if err := c.exchangeTicket(ctx, ticket); err != nil {
		return fmt.Errorf("failed to exchange ticket: %w", err)
	}

	// Step 4: Verify access to Connect
	fmt.Println("  [4/4] Verifying access...")
	if err := c.verifyAccess(ctx); err != nil {
		return fmt.Errorf("failed to verify access: %w", err)
	}

	c.loggedIn = true
	fmt.Println("  ✓ Login successful")

	// Save session cookies
	if err := c.saveTokens(); err != nil {
		fmt.Printf("  ⚠️  Warning: failed to save session: %v\n", err)
	}

	return nil
}

// getCSRFToken fetches the login page and extracts CSRF token
func (c *Client) getCSRFToken(ctx context.Context) (string, error) {
	params := url.Values{
		"service":                         {"https://connect.garmin.com/modern"},
		"webhost":                         {"https://connect.garmin.com/modern"},
		"source":                          {"https://connect.garmin.com/signin"},
		"redirectAfterAccountLoginUrl":    {"https://connect.garmin.com/modern"},
		"redirectAfterAccountCreationUrl": {"https://connect.garmin.com/modern"},
		"gauthHost":                       {"https://sso.garmin.com/sso"},
		"locale":                          {"en_US"},
		"id":                              {"gauth-widget"},
		"cssUrl":                          {"https://static.garmincdn.com/com.garmin.connect/ui/css/gauth-custom-v1.2-min.css"},
		"privacyStatementUrl":             {"https://www.garmin.com/en-US/privacy/connect/"},
		"clientId":                        {"GarminConnect"},
		"rememberMeShown":                 {"true"},
		"rememberMeChecked":               {"false"},
		"createAccountShown":              {"true"},
		"openCreateAccount":               {"false"},
		"displayNameShown":                {"false"},
		"consumeServiceTicket":            {"false"},
		"initialFocus":                    {"true"},
		"embedWidget":                     {"false"},
		"generateExtraServiceTicket":      {"true"},
		"generateTwoExtraServiceTickets":  {"false"},
		"generateNoServiceTicket":         {"false"},
		"globalOptInShown":                {"true"},
		"globalOptInChecked":              {"false"},
		"mobile":                          {"false"},
		"connectLegalTerms":               {"true"},
		"locationPromptShown":             {"true"},
		"showPassword":                    {"true"},
	}

	reqURL := ssoURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Extract CSRF token from response
	csrfRe := regexp.MustCompile(`name="_csrf"\s+value="([^"]+)"`)
	matches := csrfRe.FindSubmatch(body)
	if len(matches) < 2 {
		// Try alternate pattern
		csrfRe = regexp.MustCompile(`"_csrf"\s*:\s*"([^"]+)"`)
		matches = csrfRe.FindSubmatch(body)
	}

	if len(matches) < 2 {
		return "", fmt.Errorf("CSRF token not found in response")
	}

	return string(matches[1]), nil
}

// submitLogin submits login credentials and returns the service ticket
func (c *Client) submitLogin(ctx context.Context, email, password, csrfToken string) (string, error) {
	params := url.Values{
		"service":  {"https://connect.garmin.com/modern"},
		"webhost":  {"https://connect.garmin.com/modern"},
		"source":   {"https://connect.garmin.com/signin"},
		"clientId": {"GarminConnect"},
	}

	form := url.Values{
		"username": {email},
		"password": {password},
		"embed":    {"false"},
		"_csrf":    {csrfToken},
	}

	reqURL := ssoURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}

	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Check for login errors
	if strings.Contains(string(body), "ACCOUNT_LOCKED") {
		return "", fmt.Errorf("account is locked - please check your email")
	}
	if strings.Contains(string(body), "INVALID_CREDENTIALS") || strings.Contains(string(body), "Invalid credentials") {
		return "", fmt.Errorf("invalid email or password")
	}

	// Extract service ticket from response
	ticketRe := regexp.MustCompile(`ticket=([A-Za-z0-9\-]+)`)
	matches := ticketRe.FindSubmatch(body)
	if len(matches) < 2 {
		// Check if there's a redirect URL with ticket
		respURL := resp.Request.URL.String()
		strMatches := ticketRe.FindStringSubmatch(respURL)
		if len(strMatches) < 2 {
			return "", fmt.Errorf("service ticket not found - login may have failed")
		}
		return strMatches[1], nil
	}

	return string(matches[1]), nil
}

// exchangeTicket exchanges the service ticket for a session
func (c *Client) exchangeTicket(ctx context.Context, ticket string) error {
	ticketURL := fmt.Sprintf("%s/?ticket=%s", modernURL, ticket)

	req, err := http.NewRequestWithContext(ctx, "GET", ticketURL, nil)
	if err != nil {
		return err
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		return fmt.Errorf("ticket exchange failed with status %d", resp.StatusCode)
	}

	return nil
}

// verifyAccess verifies we can access Garmin Connect
func (c *Client) verifyAccess(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", modernURL, nil)
	if err != nil {
		return err
	}

	c.setHeaders(req)
	req.Header.Set("NK", "NT") // Required header for Garmin Connect

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("access verification failed with status %d", resp.StatusCode)
	}

	return nil
}

// UploadActivity uploads a TCX file to Garmin Connect
func (c *Client) UploadActivity(ctx context.Context, tcxPath string, workout *models.Workout) (*UploadResponse, error) {
	if !c.loggedIn {
		return nil, fmt.Errorf("not authenticated - run 'auth garmin' first")
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

	writer.Close()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload request: %w", err)
	}

	c.setHeaders(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("NK", "NT")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var uploadResp UploadResponse
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return nil, fmt.Errorf("failed to parse upload response: %w", err)
	}

	// Check for failures
	if len(uploadResp.DetailedImportResult.Failures) > 0 {
		failure := uploadResp.DetailedImportResult.Failures[0]
		if len(failure.Messages) > 0 {
			return nil, fmt.Errorf("upload failed: %s", failure.Messages[0].Content)
		}
		return nil, fmt.Errorf("upload failed with unknown error")
	}

	return &uploadResp, nil
}

// PreviewActivity generates a preview for dry-run mode
func (c *Client) PreviewActivity(workout *models.Workout, tcxPath string) *ActivityPreview {
	return &ActivityPreview{
		Name:        workout.Name,
		Description: workout.FormatDescription(),
		StartTime:   workout.Date,
		Duration:    workout.Duration,
		TCXFile:     tcxPath,
		WorkoutID:   workout.ID,
	}
}

// setHeaders sets common headers for Garmin requests
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
}

// loadTokens loads saved session from file
func (c *Client) loadTokens() error {
	data, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return err
	}

	var allTokens struct {
		Garmin *GarminTokens `json:"garmin"`
	}

	if err := json.Unmarshal(data, &allTokens); err != nil {
		return err
	}

	if allTokens.Garmin == nil {
		return fmt.Errorf("no garmin tokens found")
	}

	c.tokens = allTokens.Garmin
	return nil
}

// saveTokens saves session to file
func (c *Client) saveTokens() error {
	// Get current cookies from the jar
	connectURL, _ := url.Parse(connectURL)
	cookies := c.httpClient.Jar.Cookies(connectURL)

	c.tokens = &GarminTokens{
		SessionCookies: cookies,
		ExpiresAt:      time.Now().Add(7 * 24 * time.Hour), // Session typically lasts about a week
	}

	// Read existing tokens file
	var allTokens map[string]interface{}
	if data, err := os.ReadFile(c.tokenFile); err == nil {
		json.Unmarshal(data, &allTokens)
	}
	if allTokens == nil {
		allTokens = make(map[string]interface{})
	}

	// Update garmin tokens
	allTokens["garmin"] = c.tokens

	// Write back
	data, err := json.MarshalIndent(allTokens, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(c.tokenFile), 0700); err != nil {
		return err
	}

	return os.WriteFile(c.tokenFile, data, 0600)
}
