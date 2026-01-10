package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application
type Config struct {
	Aimharder AimharderConfig `mapstructure:"aimharder"`
	Strava    StravaConfig    `mapstructure:"strava"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Sync      SyncConfig      `mapstructure:"sync"`
}

// AimharderConfig holds Aimharder-specific config
type AimharderConfig struct {
	Email    string `mapstructure:"email"`
	Password string `mapstructure:"password"`
	BoxName  string `mapstructure:"box_name"` // subdomain, e.g., "valhallatrainingcamp"
	BoxID    string `mapstructure:"box_id"`   // numeric ID, e.g., "9818"
	UserID   string `mapstructure:"user_id"`  // user ID from profile, e.g., "852458"
	FamilyID string `mapstructure:"family_id,omitempty"`
	BaseURL  string `mapstructure:"base_url"` // defaults to aimharder.com
}

// StravaConfig holds Strava OAuth config
type StravaConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURI  string `mapstructure:"redirect_uri"`
	// Tokens are stored separately for security
}

// StorageConfig holds storage paths
type StorageConfig struct {
	DataDir     string `mapstructure:"data_dir"`     // Where to store data files
	TokensFile  string `mapstructure:"tokens_file"`  // OAuth tokens
	HistoryFile string `mapstructure:"history_file"` // Sync history
	TCXDir      string `mapstructure:"tcx_dir"`      // Generated TCX files
}

// SyncConfig holds sync preferences
type SyncConfig struct {
	DefaultDays       int           `mapstructure:"default_days"` // How many days back to sync by default
	RetryAttempts     int           `mapstructure:"retry_attempts"`
	RetryDelay        time.Duration `mapstructure:"retry_delay"`
	ActivityType      string        `mapstructure:"activity_type"`    // Default Strava activity type
	IncludeNoScore    bool          `mapstructure:"include_no_score"` // Sync workouts without scores
	MarkAsCommute     bool          `mapstructure:"mark_as_commute"`
	DefaultVisibility string        `mapstructure:"default_visibility"` // "everyone", "followers_only", "only_me"
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".aimharder-sync")

	return &Config{
		Aimharder: AimharderConfig{
			BaseURL: "https://aimharder.com",
			BoxName: "valhallatrainingcamp",
			BoxID:   "9818",
		},
		Strava: StravaConfig{
			RedirectURI: "http://localhost:8080/callback",
		},
		Storage: StorageConfig{
			DataDir:     dataDir,
			TokensFile:  filepath.Join(dataDir, "tokens.json"),
			HistoryFile: filepath.Join(dataDir, "sync_history.json"),
			TCXDir:      filepath.Join(dataDir, "tcx"),
		},
		Sync: SyncConfig{
			DefaultDays:       30,
			RetryAttempts:     3,
			RetryDelay:        5 * time.Second,
			ActivityType:      "crossfit",
			IncludeNoScore:    true,
			DefaultVisibility: "followers_only",
		},
	}
}

// Load reads config from file and environment
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	v := viper.New()
	v.SetConfigType("yaml")

	// Set defaults from DefaultConfig
	v.SetDefault("aimharder.base_url", cfg.Aimharder.BaseURL)
	v.SetDefault("aimharder.box_name", cfg.Aimharder.BoxName)
	v.SetDefault("aimharder.box_id", cfg.Aimharder.BoxID)
	v.SetDefault("strava.redirect_uri", cfg.Strava.RedirectURI)
	v.SetDefault("storage.data_dir", cfg.Storage.DataDir)
	v.SetDefault("storage.tokens_file", cfg.Storage.TokensFile)
	v.SetDefault("storage.history_file", cfg.Storage.HistoryFile)
	v.SetDefault("storage.tcx_dir", cfg.Storage.TCXDir)
	v.SetDefault("sync.default_days", cfg.Sync.DefaultDays)
	v.SetDefault("sync.retry_attempts", cfg.Sync.RetryAttempts)
	v.SetDefault("sync.retry_delay", cfg.Sync.RetryDelay)
	v.SetDefault("sync.activity_type", cfg.Sync.ActivityType)
	v.SetDefault("sync.include_no_score", cfg.Sync.IncludeNoScore)
	v.SetDefault("sync.default_visibility", cfg.Sync.DefaultVisibility)

	// Environment variables (prefixed with AIMHARDER_)
	v.SetEnvPrefix("AIMHARDER")
	v.AutomaticEnv()

	// Map environment variables
	v.BindEnv("aimharder.email", "AIMHARDER_EMAIL")
	v.BindEnv("aimharder.password", "AIMHARDER_PASSWORD")
	v.BindEnv("aimharder.box_name", "AIMHARDER_BOX_NAME")
	v.BindEnv("aimharder.box_id", "AIMHARDER_BOX_ID")
	v.BindEnv("aimharder.user_id", "AIMHARDER_USER_ID")
	v.BindEnv("aimharder.family_id", "AIMHARDER_FAMILY_ID")
	v.BindEnv("strava.client_id", "STRAVA_CLIENT_ID")
	v.BindEnv("strava.client_secret", "STRAVA_CLIENT_SECRET")
	v.BindEnv("storage.data_dir", "AIMHARDER_STORAGE_DATA_DIR")
	v.BindEnv("storage.tokens_file", "AIMHARDER_STORAGE_TOKENS_FILE")
	v.BindEnv("storage.history_file", "AIMHARDER_STORAGE_HISTORY_FILE")
	v.BindEnv("storage.tcx_dir", "AIMHARDER_STORAGE_TCX_DIR")

	// Try to read config file if it exists
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		// Look for config in standard locations
		v.SetConfigName("config")
		v.AddConfigPath(".")
		v.AddConfigPath(cfg.Storage.DataDir)
		v.AddConfigPath("/etc/aimharder-sync")
	}

	if err := v.ReadInConfig(); err != nil {
		// Config file not found is OK if we have env vars
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config: %w", err)
		}
	}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	return cfg, nil
}

// Validate checks that required config is present
func (c *Config) Validate() error {
	if c.Aimharder.Email == "" {
		return fmt.Errorf("aimharder.email is required (set AIMHARDER_EMAIL)")
	}
	if c.Aimharder.Password == "" {
		return fmt.Errorf("aimharder.password is required (set AIMHARDER_PASSWORD)")
	}
	if c.Aimharder.BoxID == "" {
		return fmt.Errorf("aimharder.box_id is required (set AIMHARDER_BOX_ID)")
	}
	return nil
}

// ValidateStrava checks Strava config
func (c *Config) ValidateStrava() error {
	if c.Strava.ClientID == "" {
		return fmt.Errorf("strava.client_id is required (set STRAVA_CLIENT_ID)")
	}
	if c.Strava.ClientSecret == "" {
		return fmt.Errorf("strava.client_secret is required (set STRAVA_CLIENT_SECRET)")
	}
	return nil
}

// EnsureDirectories creates necessary directories
func (c *Config) EnsureDirectories() error {
	dirs := []string{
		c.Storage.DataDir,
		c.Storage.TCXDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// GetBoxURL returns the full URL for the Aimharder box
func (c *Config) GetBoxURL() string {
	return fmt.Sprintf("https://%s.aimharder.com", c.Aimharder.BoxName)
}
