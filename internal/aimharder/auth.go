package aimharder

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

const (
	baseURL       = "https://aimharder.com"
	loginURL      = "https://login.aimharder.com"
	userAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36"
	authCookieKey = "amhrdrauth"
)

// HTTPClient wraps http.Client with browser-like behavior
type HTTPClient struct {
	client  *http.Client
	verbose bool
}

// NewHTTPClient creates a new HTTP client with cookie jar and browser-like settings
func NewHTTPClient(verbose bool) (*HTTPClient, error) {
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	return &HTTPClient{
		client: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
		verbose: verbose,
	}, nil
}

// GetCookieJar returns the underlying cookie jar
func (h *HTTPClient) GetCookieJar() http.CookieJar {
	return h.client.Jar
}

// GetHTTPClient returns the underlying http.Client for direct use
func (h *HTTPClient) GetHTTPClient() *http.Client {
	return h.client
}

// SetVerbose sets the verbose flag
func (h *HTTPClient) SetVerbose(verbose bool) {
	h.verbose = verbose
}

// DoRequest performs an HTTP request with browser-like headers
func (h *HTTPClient) DoRequest(method, urlStr string, body io.Reader, opts RequestOptions) (*http.Response, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	h.applyHeaders(req, opts)

	if h.verbose {
		h.logRequest(req)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if h.verbose {
		h.logResponse(resp)
	}

	return resp, nil
}

// RequestOptions configures request behavior
type RequestOptions struct {
	Referer     string
	ContentType string
	Origin      string
	IsXHR       bool   // XMLHttpRequest
	Accept      string // Custom Accept header
}

// applyHeaders sets browser-like headers on the request
func (h *HTTPClient) applyHeaders(req *http.Request, opts RequestOptions) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
	req.Header.Set("Connection", "keep-alive")

	if opts.IsXHR {
		if opts.Accept != "" {
			req.Header.Set("Accept", opts.Accept)
		} else {
			req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
		}
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	} else {
		if opts.Accept != "" {
			req.Header.Set("Accept", opts.Accept)
		} else {
			req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
		}
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-User", "?1")
		req.Header.Set("Cache-Control", "max-age=0")
	}

	if opts.Referer != "" {
		req.Header.Set("Referer", opts.Referer)
		// Determine Sec-Fetch-Site based on referer
		refererURL, _ := url.Parse(opts.Referer)
		reqURL := req.URL
		if refererURL != nil && refererURL.Host != reqURL.Host {
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
	} else {
		req.Header.Set("Sec-Fetch-Site", "none")
	}

	if opts.ContentType != "" {
		req.Header.Set("Content-Type", opts.ContentType)
	}

	if opts.Origin != "" {
		req.Header.Set("Origin", opts.Origin)
	}
}

func (h *HTTPClient) logRequest(req *http.Request) {
	fmt.Printf("  → %s %s\n", req.Method, req.URL.String())
}

func (h *HTTPClient) logResponse(resp *http.Response) {
	fmt.Printf("  ← Status: %d\n", resp.StatusCode)
	if location := resp.Header.Get("Location"); location != "" {
		fmt.Printf("  ← Redirect: %s\n", location)
	}
	for _, c := range resp.Cookies() {
		if c.Name == authCookieKey {
			fmt.Printf("  ← Set-Cookie: %s ✓\n", c.Name)
		}
	}
}

// AuthResult contains the result of a successful authentication
type AuthResult struct {
	AuthCookie string
	Cookies    []*http.Cookie
}

// Login performs the complete login flow:
// 1. Visit aimharder.com to establish initial session
// 2. Visit login.aimharder.com (simulates clicking login button)
// 3. POST credentials to login.aimharder.com
// 4. Follow redirect to /home to verify login success
func (h *HTTPClient) Login(email, password string) (*AuthResult, error) {
	if h.verbose {
		fmt.Println("  [1/4] Visiting aimharder.com...")
	}
	resp, err := h.DoRequest("GET", baseURL, nil, RequestOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to visit main page: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("main page returned status %d", resp.StatusCode)
	}

	time.Sleep(300 * time.Millisecond)

	if h.verbose {
		fmt.Println("  [2/4] Visiting login.aimharder.com...")
	}
	resp, err = h.DoRequest("GET", loginURL, nil, RequestOptions{
		Referer: baseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to visit login page: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login page returned status %d", resp.StatusCode)
	}

	time.Sleep(200 * time.Millisecond)

	if h.verbose {
		fmt.Println("  [3/4] Submitting credentials...")
	}
	formData := url.Values{
		"mail":             {email},
		"pw":               {password},
		"loginfingerprint": {"0"},
		"loginiframe":      {"0"},
		"login":            {"Iniciar sesión"},
	}

	resp, err = h.DoRequest("POST", loginURL, strings.NewReader(formData.Encode()), RequestOptions{
		Referer:     loginURL,
		ContentType: "application/x-www-form-urlencoded",
		Origin:      loginURL,
	})
	if err != nil {
		return nil, fmt.Errorf("login request failed: %w", err)
	}

	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	bodyStr := string(body)
	lowerBody := strings.ToLower(bodyStr)
	if strings.Contains(lowerBody, "datos incorrectos") ||
		strings.Contains(lowerBody, "email o contraseña incorrectos") ||
		strings.Contains(lowerBody, "credenciales incorrectas") ||
		strings.Contains(lowerBody, "invalid email") ||
		strings.Contains(lowerBody, "invalid password") {
		return nil, fmt.Errorf("invalid credentials")
	}

	if h.verbose {
		fmt.Println("  [4/4] Verifying authentication...")
	}

	authCookie := h.findAuthCookie()
	if authCookie == "" {
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location := resp.Header.Get("Location")
			if location != "" {
				if h.verbose {
					fmt.Printf("  → Following redirect to: %s\n", location)
				}
				resp, err = h.DoRequest("GET", location, nil, RequestOptions{
					Referer: loginURL,
				})
				if err != nil {
					return nil, fmt.Errorf("failed to follow redirect: %w", err)
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				authCookie = h.findAuthCookie()
			}
		}
	}

	if authCookie == "" {
		if h.verbose {
			fmt.Println("  → Visiting /home to verify session...")
		}
		resp, err = h.DoRequest("GET", baseURL+"/home", nil, RequestOptions{
			Referer: loginURL,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to visit home: %w", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		authCookie = h.findAuthCookie()
	}

	if authCookie == "" {
		return nil, fmt.Errorf("authentication failed: %s cookie not received", authCookieKey)
	}

	if h.verbose {
		fmt.Printf("  ✓ Got %s cookie\n", authCookieKey)
	}

	return &AuthResult{
		AuthCookie: authCookie,
		Cookies:    h.getAllCookies(),
	}, nil
}

// findAuthCookie searches for the auth cookie across all known domains
func (h *HTTPClient) findAuthCookie() string {
	domains := []string{
		baseURL,
		loginURL,
		"https://www.aimharder.com",
	}

	for _, domain := range domains {
		u, err := url.Parse(domain)
		if err != nil {
			continue
		}
		for _, cookie := range h.client.Jar.Cookies(u) {
			if cookie.Name == authCookieKey {
				return cookie.Value
			}
		}
	}
	return ""
}

// getAllCookies returns all cookies from the jar for the main domain
func (h *HTTPClient) getAllCookies() []*http.Cookie {
	u, _ := url.Parse(baseURL)
	return h.client.Jar.Cookies(u)
}

// HasAuthCookie checks if the client has a valid auth cookie
func (h *HTTPClient) HasAuthCookie() bool {
	return h.findAuthCookie() != ""
}

// PrintCookies prints all cookies (for debugging)
func (h *HTTPClient) PrintCookies() {
	domains := []string{baseURL, loginURL}
	for _, domain := range domains {
		u, _ := url.Parse(domain)
		cookies := h.client.Jar.Cookies(u)
		if len(cookies) > 0 {
			fmt.Printf("  Cookies for %s:\n", domain)
			for _, c := range cookies {
				fmt.Printf("    - %s = %s\n", c.Name, c.Value[:min(20, len(c.Value))]+"...")
			}
		}
	}
}
