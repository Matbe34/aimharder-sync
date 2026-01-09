package strava

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"
)

// OAuthServer handles the OAuth callback
type OAuthServer struct {
	client   *Client
	listener net.Listener
	server   *http.Server
	state    string
	result   chan error
}

// StartOAuthFlow starts the OAuth flow and returns when complete
func (c *Client) StartOAuthFlow(ctx context.Context) error {
	// Generate random state
	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)

	// Create server
	oauthServer := &OAuthServer{
		client: c,
		state:  state,
		result: make(chan error, 1),
	}

	// Start server
	if err := oauthServer.start(); err != nil {
		return err
	}
	defer oauthServer.stop()

	// Get auth URL
	authURL := c.GetAuthURL(state)

	fmt.Println("\n🔐 Strava Authorization Required")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("\nPlease open the following URL in your browser to authorize:")
	fmt.Println()
	fmt.Printf("  👉 %s\n", authURL)
	fmt.Println()
	fmt.Println("Waiting for authorization...")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Wait for result or timeout
	select {
	case err := <-oauthServer.result:
		if err != nil {
			return err
		}
		fmt.Println("\n✅ Successfully authenticated with Strava!")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("authorization timed out after 5 minutes")
	}
}

func (s *OAuthServer) start() error {
	var err error
	s.listener, err = net.Listen("tcp", ":8080")
	if err != nil {
		return fmt.Errorf("failed to start OAuth server: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", s.handleCallback)

	s.server = &http.Server{
		Handler: mux,
	}

	go s.server.Serve(s.listener)

	return nil
}

func (s *OAuthServer) stop() {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(ctx)
	}
}

func (s *OAuthServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Check state
	state := r.URL.Query().Get("state")
	if state != s.state {
		s.result <- fmt.Errorf("invalid state parameter")
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	// Check for errors
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		s.result <- fmt.Errorf("authorization denied: %s", errParam)
		http.Error(w, "Authorization denied", http.StatusBadRequest)
		return
	}

	// Get code
	code := r.URL.Query().Get("code")
	if code == "" {
		s.result <- fmt.Errorf("no authorization code received")
		http.Error(w, "No authorization code", http.StatusBadRequest)
		return
	}

	// Exchange code for tokens
	ctx := r.Context()
	if err := s.client.ExchangeCode(ctx, code); err != nil {
		s.result <- err
		http.Error(w, "Failed to exchange authorization code", http.StatusInternalServerError)
		return
	}

	// Success!
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Authorization Successful</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            margin: 0;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
        }
        .container {
            text-align: center;
            padding: 40px;
            background: rgba(255,255,255,0.1);
            border-radius: 20px;
            backdrop-filter: blur(10px);
        }
        .checkmark {
            font-size: 80px;
            margin-bottom: 20px;
        }
        h1 { margin: 0 0 10px 0; }
        p { opacity: 0.9; }
    </style>
</head>
<body>
    <div class="container">
        <div class="checkmark">✅</div>
        <h1>Authorization Successful!</h1>
        <p>You can close this window and return to the terminal.</p>
        <p>AimHarder Sync is now connected to Strava.</p>
    </div>
</body>
</html>
`))

	s.result <- nil
}
