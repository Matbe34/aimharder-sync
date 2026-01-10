package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/aimharder-sync/internal/aimharder"
	"github.com/aimharder-sync/internal/config"
	"github.com/aimharder-sync/internal/models"
	"github.com/aimharder-sync/internal/strava"
	"github.com/aimharder-sync/internal/tcx"
)

var (
	cfgFile string
	cfg     *config.Config
	verbose bool
	dryRun  bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "aimharder-sync",
		Short: "Sync your Aimharder CrossFit workouts to Strava",
		Long: `AimHarder Sync - Export your CrossFit workouts from Aimharder
and upload them to Strava or export as TCX files.

Before using, you need to:
1. Set up your Aimharder credentials (AIMHARDER_EMAIL, AIMHARDER_PASSWORD)
2. Set up Strava API credentials (STRAVA_CLIENT_ID, STRAVA_CLIENT_SECRET)
3. Run 'aimharder-sync auth' to authenticate with Strava`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Name() == "version" || cmd.Name() == "help" {
				return nil
			}

			var err error
			cfg, err = config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.aimharder-sync/config.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "show what would be done without actually doing it")

	rootCmd.AddCommand(
		newSyncCmd(),
		newAuthCmd(),
		newFetchCmd(),
		newExportCmd(),
		newStatusCmd(),
		newWebhookCmd(),
		newVersionCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newSyncCmd() *cobra.Command {
	var (
		days      int
		startDate string
		endDate   string
		force     bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync workouts from Aimharder to Strava",
		Long: `Fetch workouts from Aimharder and upload them to Strava.
By default, syncs the last 30 days of workouts.

Examples:
  # Sync last 30 days to Strava
  aimharder-sync sync

  # Sync specific date range
  aimharder-sync sync --start 2024-01-01 --end 2024-01-31

  # Sync last 7 days
  aimharder-sync sync --days 7

  # Force re-sync already synced workouts
  aimharder-sync sync --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(days, startDate, endDate, force)
		},
	}

	cmd.Flags().IntVar(&days, "days", 30, "number of days to sync (from today)")
	cmd.Flags().StringVar(&startDate, "start", "", "start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&endDate, "end", "", "end date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&force, "force", false, "force re-sync of already synced workouts")

	return cmd
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with Strava",
		Long: `Authenticate with Strava (opens browser).

Examples:
  # Authenticate with Strava
  aimharder-sync auth`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuth()
		},
	}

	return cmd
}

func newFetchCmd() *cobra.Command {
	var (
		days      int
		startDate string
		endDate   string
		output    string
	)

	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Fetch workouts from Aimharder (without syncing)",
		Long: `Fetch workout data from Aimharder and display or save it locally.
Useful for testing or viewing your workout history.

Examples:
  # Fetch last 7 days and display
  aimharder-sync fetch --days 7

  # Fetch and save to JSON file
  aimharder-sync fetch --days 30 --output workouts.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFetch(days, startDate, endDate, output)
		},
	}

	cmd.Flags().IntVar(&days, "days", 7, "number of days to fetch")
	cmd.Flags().StringVar(&startDate, "start", "", "start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&endDate, "end", "", "end date (YYYY-MM-DD)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (JSON)")

	return cmd
}

func newExportCmd() *cobra.Command {
	var (
		days      int
		startDate string
		endDate   string
		outputDir string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export workouts as TCX files",
		Long: `Export workouts from Aimharder as TCX files that can be
manually uploaded to any fitness platform.

Examples:
  # Export last 30 days to TCX files
  aimharder-sync export --days 30

  # Export to specific directory
  aimharder-sync export --days 30 --output ~/tcx-files`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(days, startDate, endDate, outputDir)
		},
	}

	cmd.Flags().IntVar(&days, "days", 30, "number of days to export")
	cmd.Flags().StringVar(&startDate, "start", "", "start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&endDate, "end", "", "end date (YYYY-MM-DD)")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory")

	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show sync status and configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus()
		},
	}

	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("aimharder-sync v1.0.0")
		},
	}
}

func newWebhookCmd() *cobra.Command {
	var (
		port      string
		authToken string
	)

	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Start webhook server for remote sync triggers",
		Long: `Start an HTTP server that listens for sync triggers.
This allows you to trigger syncs from phone widgets, shortcuts, or other automation tools.

Endpoints:
  POST /sync         - Trigger a sync (optional: ?days=N)
  GET  /status       - Get last sync result
  GET  /health       - Health check

Examples:
  # Start webhook server on default port 8080
  aimharder-sync webhook

  # Start with custom port and auth token
  aimharder-sync webhook --port 9090 --token mysecrettoken

  # Trigger sync from phone (with curl)
  curl -X POST http://your-server:8080/sync -H "X-Auth-Token: mysecrettoken"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunWebhookServer(cfg, port, authToken)
		},
	}

	cmd.Flags().StringVar(&port, "port", "8080", "port to listen on")
	cmd.Flags().StringVar(&authToken, "token", "", "authentication token (optional but recommended)")

	return cmd
}

// Command implementations

func runSync(days int, startDate, endDate string, force bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n⚠️  Cancelling... (press Ctrl+C again to force)")
		cancel()
		<-sigCh
		fmt.Println("\n❌ Forced exit")
		os.Exit(1)
	}()

	if err := cfg.Validate(); err != nil {
		return err
	}

	if err := cfg.EnsureDirectories(); err != nil {
		return err
	}

	start, end, err := parseDateRange(days, startDate, endDate)
	if err != nil {
		return err
	}

	fmt.Printf("🔄 Syncing workouts from %s to %s\n", start.Format("2006-01-02"), end.Format("2006-01-02"))

	ahClient, err := aimharder.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Aimharder client: %w", err)
	}

	fmt.Println("🔐 Logging into Aimharder...")
	if err := ahClient.Login(); err != nil {
		return fmt.Errorf("failed to login to Aimharder: %w", err)
	}
	fmt.Println("✅ Logged into Aimharder")

	fmt.Println("📥 Fetching workouts from Aimharder...")
	workouts, err := ahClient.GetWorkoutHistory(ctx, start, end)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("cancelled")
		}
		return fmt.Errorf("failed to fetch workouts: %w", err)
	}

	if len(workouts) == 0 {
		fmt.Println("ℹ️  No workouts found in the specified date range")
		return nil
	}

	fmt.Printf("📋 Found %d workouts\n", len(workouts))

	history := loadSyncHistory(cfg.Storage.HistoryFile)

	var toSync []models.Workout
	for _, w := range workouts {
		if force || !isWorkoutSynced(history, w.ID) {
			toSync = append(toSync, w)
		}
	}

	if len(toSync) == 0 {
		fmt.Println("✅ All workouts already synced!")
		return nil
	}

	fmt.Printf("🔄 %d workouts to sync\n", len(toSync))

	// Generate TCX files
	fmt.Println("📝 Generating TCX files...")
	tcxGen := tcx.NewGenerator(cfg.Storage.TCXDir)
	tcxFiles, err := tcxGen.GenerateAll(toSync)
	if err != nil {
		return fmt.Errorf("failed to generate TCX files: %w", err)
	}

	if dryRun {
		fmt.Println("\n" + strings.Repeat("━", 70))
		fmt.Println("📋 DRY RUN - Strava Activities that would be created:")
		fmt.Println(strings.Repeat("━", 70))

		var stravaClient *strava.Client
		if err := cfg.ValidateStrava(); err == nil {
			stravaClient, _ = strava.NewClient(cfg)
		}

		for i, w := range toSync {
			tcxFile := ""
			if i < len(tcxFiles) {
				tcxFile = tcxFiles[i]
			}

			fmt.Printf("\n┌─ Activity %d of %d ─────────────────────────────────────────────────\n", i+1, len(toSync))
			fmt.Printf("│\n")
			fmt.Printf("│ 🏃 STRAVA ACTIVITY PREVIEW\n")
			fmt.Printf("│ %s\n", strings.Repeat("─", 50))

			activityName := w.Name
			if activityName == "" {
				activityName = fmt.Sprintf("CrossFit WOD - %s", w.Date.Format("2006-01-02"))
			}

			activityType := "Crossfit"
			if stravaClient != nil {
				preview := stravaClient.PreviewActivity(&w, tcxFile)
				activityType = preview.Type
			}

			fmt.Printf("│\n")
			fmt.Printf("│ 📛 name:           %s\n", activityName)
			fmt.Printf("│ 🏃 type:           %s\n", activityType)
			fmt.Printf("│ 📅 start_date:     %s\n", w.Date.Format("2006-01-02T15:04:05Z"))
			fmt.Printf("│ 🆔 external_id:    %s\n", w.ID)
			fmt.Printf("│ 📄 data_type:      tcx\n")
			if tcxFile != "" {
				fmt.Printf("│ 📁 tcx_file:       %s\n", tcxFile)
			}

			elapsed := ""
			if w.Duration > 0 {
				elapsed = formatDurationForDisplay(w.Duration)
			} else if w.Result != nil && w.Result.Time != nil {
				elapsed = formatDurationForDisplay(*w.Result.Time)
			}
			if elapsed != "" {
				fmt.Printf("│ ⏱️  elapsed_time:   %s\n", elapsed)
			}

			fmt.Printf("│\n")
			fmt.Printf("│ 📝 description:\n")
			fmt.Printf("│ %s\n", strings.Repeat("─", 50))
			if w.Description != "" {
				for _, line := range strings.Split(w.Description, "\n") {
					if line != "" {
						fmt.Printf("│    %s\n", line)
					}
				}
			} else {
				fmt.Printf("│    (no description)\n")
			}

			fmt.Printf("│\n")
			fmt.Printf("│ 📊 WORKOUT DETAILS\n")
			fmt.Printf("│ %s\n", strings.Repeat("─", 50))
			fmt.Printf("│ 🏠 Box:            %s\n", w.BoxName)
			fmt.Printf("│ 🏋️  Workout Type:   %s\n", w.Type)

			if len(w.Sections) > 0 {
				fmt.Printf("│\n│ 📋 Sections:\n")
				for _, s := range w.Sections {
					sectionLine := fmt.Sprintf("│    • %s", s.Name)
					if s.TimeCap > 0 {
						sectionLine += fmt.Sprintf(" (%d min)", s.TimeCap)
					}
					if s.RoundsCompleted > 0 && s.RepsAchieved > 0 {
						sectionLine += fmt.Sprintf(" → %dR + %d reps", s.RoundsCompleted, s.RepsAchieved)
					} else if s.RoundsCompleted > 0 {
						sectionLine += fmt.Sprintf(" → %d rounds", s.RoundsCompleted)
					}
					if s.RX {
						sectionLine += " ✅RX"
					}
					fmt.Println(sectionLine)
				}
			}

			if w.Result != nil {
				fmt.Printf("│\n│ 🎯 Result:\n")
				if w.Result.Time != nil {
					fmt.Printf("│    ⏱️  Time: %s\n", formatDurationForDisplay(*w.Result.Time))
				}
				if w.Result.Rounds > 0 {
					if w.Result.Reps > 0 {
						fmt.Printf("│    🔄 Rounds: %d + %d reps\n", w.Result.Rounds, w.Result.Reps)
					} else {
						fmt.Printf("│    🔄 Rounds: %d\n", w.Result.Rounds)
					}
				}
				if w.Result.Weight > 0 {
					fmt.Printf("│    🏋️  Weight: %.1f kg\n", w.Result.Weight)
				}
				if w.Result.RxPlus {
					fmt.Printf("│    ⭐ Rx+\n")
				} else if w.Result.Scaled {
					fmt.Printf("│    📉 Scaled\n")
				} else {
					fmt.Printf("│    ✅ Rx\n")
				}
			}

			fmt.Printf("└%s\n", strings.Repeat("─", 69))
		}

		fmt.Printf("\n📊 Summary: %d activities would be uploaded to Strava\n", len(toSync))
		fmt.Println("📁 TCX files generated in:", cfg.Storage.TCXDir)
		fmt.Println("\n💡 Run without --dry-run to actually sync these workouts.")
		return nil
	}

	if err := syncToStrava(ctx, cfg, toSync, tcxFiles, history); err != nil {
		fmt.Printf("⚠️  Strava sync error: %v\n", err)
	}

	if err := saveSyncHistory(cfg.Storage.HistoryFile, history); err != nil {
		fmt.Printf("⚠️  Failed to save sync history: %v\n", err)
	}

	fmt.Println("\n✅ Sync complete!")
	return nil
}

func syncToStrava(ctx context.Context, cfg *config.Config, workouts []models.Workout, tcxFiles []string, history map[string][]models.SyncStatus) error {
	if err := cfg.ValidateStrava(); err != nil {
		return err
	}

	stravaClient, err := strava.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Strava client: %w", err)
	}

	if !stravaClient.IsAuthenticated() {
		return fmt.Errorf("not authenticated with Strava - run 'aimharder-sync auth' first")
	}

	// Find the date range of workouts we're syncing
	var minDate, maxDate time.Time
	for _, w := range workouts {
		if minDate.IsZero() || w.Date.Before(minDate) {
			minDate = w.Date
		}
		if maxDate.IsZero() || w.Date.After(maxDate) {
			maxDate = w.Date
		}
	}

	// Fetch existing Strava activities in this date range (with some buffer)
	fmt.Println("🔍 Checking for existing activities in Strava...")
	startRange := minDate.AddDate(0, 0, -1) // 1 day before
	endRange := maxDate.AddDate(0, 0, 1)    // 1 day after
	existingActivities, err := stravaClient.GetActivitiesInRange(ctx, startRange, endRange)
	if err != nil {
		fmt.Printf("⚠️  Warning: Could not fetch existing activities: %v\n", err)
		fmt.Println("   Proceeding anyway (Strava will reject duplicates)...")
		existingActivities = nil
	} else {
		fmt.Printf("   Found %d existing activities in date range\n", len(existingActivities))
	}

	fmt.Println("📤 Uploading to Strava...")

	uploadedCount := 0
	skippedCount := 0

	for i, workout := range workouts {
		if i >= len(tcxFiles) {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if activity already exists in Strava
		if existingActivities != nil {
			if existing := stravaClient.ActivityExistsForWorkout(existingActivities, &workout); existing != nil {
				fmt.Printf("  ⏭️  Skipping: %s - %s (already exists as activity %d)\n",
					workout.Date.Format("2006-01-02"), workout.Name, existing.ID)
				recordSync(history, workout.ID, fmt.Sprintf("%d", existing.ID), true, "already_exists")
				skippedCount++
				continue
			}
		}

		tcxFile := tcxFiles[i]
		fmt.Printf("  📤 Uploading: %s - %s...", workout.Date.Format("2006-01-02"), workout.Name)

		uploadResp, err := stravaClient.UploadActivity(ctx, tcxFile, &workout)
		if err != nil {
			fmt.Printf(" ❌ Error: %v\n", err)
			recordSync(history, workout.ID, "", false, err.Error())
			continue
		}

		status, err := stravaClient.WaitForUpload(ctx, uploadResp.ID, 2*time.Minute)
		if err != nil {
			fmt.Printf(" ❌ Error: %v\n", err)
			recordSync(history, workout.ID, "", false, err.Error())
			continue
		}

		if status.Error != "" {
			if status.Error == "duplicate" || strings.Contains(status.Error, "duplicate") {
				fmt.Printf(" ⏭️  Already exists\n")
				recordSync(history, workout.ID, "", true, "duplicate")
				skippedCount++
			} else {
				fmt.Printf(" ❌ Error: %s\n", status.Error)
				recordSync(history, workout.ID, "", false, status.Error)
			}
			continue
		}

		fmt.Printf(" ✅ Activity ID: %d\n", status.ActivityID)
		recordSync(history, workout.ID, fmt.Sprintf("%d", status.ActivityID), true, "")
		uploadedCount++

		time.Sleep(500 * time.Millisecond)
	}

	fmt.Printf("\n📊 Summary: %d uploaded, %d skipped (already existed)\n", uploadedCount, skippedCount)

	return nil
}

func runAuth() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := cfg.ValidateStrava(); err != nil {
		return err
	}

	stravaClient, err := strava.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Strava client: %w", err)
	}

	return stravaClient.StartOAuthFlow(ctx)
}

func runFetch(days int, startDate, endDate, output string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n⚠️  Cancelling...")
		cancel()
		<-sigCh
		os.Exit(1)
	}()

	if err := cfg.Validate(); err != nil {
		return err
	}

	start, end, err := parseDateRange(days, startDate, endDate)
	if err != nil {
		return err
	}

	fmt.Printf("📥 Fetching workouts from %s to %s\n", start.Format("2006-01-02"), end.Format("2006-01-02"))

	ahClient, err := aimharder.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Aimharder client: %w", err)
	}

	fmt.Println("🔐 Logging into Aimharder...")
	if err := ahClient.Login(); err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	workouts, err := ahClient.GetWorkoutHistory(ctx, start, end)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("cancelled")
		}
		return fmt.Errorf("failed to fetch workouts: %w", err)
	}

	fmt.Printf("📋 Found %d workouts\n\n", len(workouts))

	if output != "" {
		data, err := json.MarshalIndent(workouts, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal workouts: %w", err)
		}
		if err := os.WriteFile(output, data, 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Printf("💾 Saved to %s\n", output)
	} else {
		for _, w := range workouts {
			fmt.Println(strings.Repeat("━", 70))
			if w.Date.Hour() > 0 || w.Date.Minute() > 0 {
				fmt.Printf("📅 %s @ %s - %s\n", w.Date.Format("2006-01-02 (Monday)"), w.Date.Format("15:04"), w.Name)
			} else {
				fmt.Printf("📅 %s - %s\n", w.Date.Format("2006-01-02 (Monday)"), w.Name)
			}
			fmt.Printf("   🏠 %s | 🏋️ %s\n", w.BoxName, w.Type)

			if len(w.Sections) > 0 {
				fmt.Println("\n   📋 Workout Structure:")
				for i, s := range w.Sections {
					line := fmt.Sprintf("      [%d] %s", i+1, s.Name)
					if s.TimeCap > 0 {
						line += fmt.Sprintf(" (%d min cap)", s.TimeCap)
					}
					fmt.Println(line)

					resultLine := "          → "
					hasResult := false

					if s.RoundsCompleted > 0 && s.RepsAchieved > 0 {
						resultLine += fmt.Sprintf("%dR + %d reps", s.RoundsCompleted, s.RepsAchieved)
						hasResult = true
					} else if s.RoundsCompleted > 0 {
						if s.Type == "EMOM" || strings.Contains(strings.ToUpper(s.Name), "EMOM") {
							resultLine += fmt.Sprintf("%d/%d sets", s.RoundsCompleted, s.RoundsCompleted)
						} else {
							resultLine += fmt.Sprintf("%d rounds", s.RoundsCompleted)
						}
						hasResult = true
					} else if s.RepsAchieved > 0 {
						resultLine += fmt.Sprintf("%d reps", s.RepsAchieved)
						hasResult = true
					}

					if s.Time != "" && s.Time != "0" {
						if hasResult {
							resultLine += " in "
						}
						resultLine += s.Time + " min"
						hasResult = true
					}
					if s.RX {
						resultLine += " ✅RX"
						hasResult = true
					}
					if s.Rank > 0 {
						resultLine += fmt.Sprintf(" (rank #%d)", s.Rank)
						hasResult = true
					}
					if hasResult {
						fmt.Println(resultLine)
					}

					if s.Notes != "" {
						notes := strings.ReplaceAll(s.Notes, "&quot;", "\"")
						notes = strings.ReplaceAll(notes, "&#39;", "'")
						notes = strings.ReplaceAll(notes, "\u2019", "'")
						fmt.Printf("          📝 %s\n", notes)
					}

					for _, ex := range w.Exercises {
						if ex.SectionIndex == i {
							exLine := "            • " + ex.Name
							if ex.RepsPerRound > 0 {
								exLine += fmt.Sprintf(" (%d/round)", ex.RepsPerRound)
							} else if ex.Reps > 0 {
								exLine += fmt.Sprintf(" (%d reps)", ex.Reps)
							}
							if ex.Weight > 0 {
								unit := ex.WeightUnit
								if unit == "" {
									unit = "kg"
								}
								exLine += fmt.Sprintf(" @ %.0f%s", ex.Weight, unit)
							}
							if ex.Distance > 0 {
								unit := ex.DistanceUnit
								if unit == "" {
									unit = "m"
								}
								exLine += fmt.Sprintf(" %.0f%s", ex.Distance, unit)
							}
							if ex.Calories > 0 {
								exLine += fmt.Sprintf(" %dcal", ex.Calories)
							}
							if ex.PR {
								exLine += " 🏆PR!"
							}
							fmt.Println(exLine)
						}
					}
				}
			}

			unassigned := false
			for _, ex := range w.Exercises {
				if len(w.Sections) <= 1 || ex.SectionIndex >= len(w.Sections) {
					unassigned = true
					break
				}
			}
			if len(w.Sections) == 0 || (len(w.Sections) == 1 && len(w.Exercises) > 0) || unassigned {
				if len(w.Sections) > 0 && !unassigned {
					// Already shown under section
				} else {
					fmt.Println("\n   💪 Exercises:")
					for _, ex := range w.Exercises {
						line := "      • " + ex.Name
						if ex.RepsPerRound > 0 {
							line += fmt.Sprintf(" (%d/round)", ex.RepsPerRound)
						} else if ex.Reps > 0 {
							line += fmt.Sprintf(" (%d reps)", ex.Reps)
						}
						if ex.Weight > 0 {
							unit := ex.WeightUnit
							if unit == "" {
								unit = "kg"
							}
							line += fmt.Sprintf(" @ %.0f%s", ex.Weight, unit)
						}
						if ex.Distance > 0 {
							unit := ex.DistanceUnit
							if unit == "" {
								unit = "m"
							}
							line += fmt.Sprintf(" %.0f%s", ex.Distance, unit)
						}
						if ex.Calories > 0 {
							line += fmt.Sprintf(" %dcal", ex.Calories)
						}
						if ex.PR {
							line += " 🏆PR!"
						}
						fmt.Println(line)
					}
				}
			}

			if w.Result != nil {
				fmt.Println("\n   🎯 Result:")
				if w.Result.Time != nil {
					fmt.Printf("      ⏱️ Time: %s\n", formatDurationForDisplay(*w.Result.Time))
				}
				if w.Result.Rounds > 0 {
					if w.Result.Reps > 0 {
						fmt.Printf("      🔄 Rounds: %d + %d reps\n", w.Result.Rounds, w.Result.Reps)
					} else {
						fmt.Printf("      🔄 Rounds: %d\n", w.Result.Rounds)
					}
				}
				if w.Result.Weight > 0 {
					fmt.Printf("      🏋️ Weight: %.1fkg\n", w.Result.Weight)
				}
				if w.Result.Score != "" && w.Result.Time == nil && w.Result.Rounds == 0 {
					fmt.Printf("      📊 Score: %s\n", w.Result.Score)
				}
				if w.Result.RxPlus {
					fmt.Println("      ⭐ Rx+")
				} else if !w.Result.Scaled {
					fmt.Println("      ✅ Rx")
				} else {
					fmt.Println("      📉 Scaled")
				}
				if w.Result.Notes != "" {
					fmt.Printf("      💬 %s\n", w.Result.Notes)
				}
			}

			fmt.Println()
		}
		fmt.Println(strings.Repeat("━", 70))
	}

	return nil
}

func runExport(days int, startDate, endDate, outputDir string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n⚠️  Cancelling...")
		cancel()
		<-sigCh
		os.Exit(1)
	}()

	if err := cfg.Validate(); err != nil {
		return err
	}

	start, end, err := parseDateRange(days, startDate, endDate)
	if err != nil {
		return err
	}

	if outputDir == "" {
		outputDir = cfg.Storage.TCXDir
	}

	fmt.Printf("📥 Fetching workouts from %s to %s\n", start.Format("2006-01-02"), end.Format("2006-01-02"))

	ahClient, err := aimharder.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Aimharder client: %w", err)
	}

	fmt.Println("🔐 Logging into Aimharder...")
	if err := ahClient.Login(); err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	workouts, err := ahClient.GetWorkoutHistory(ctx, start, end)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("cancelled")
		}
		return fmt.Errorf("failed to fetch workouts: %w", err)
	}

	if len(workouts) == 0 {
		fmt.Println("ℹ️  No workouts found")
		return nil
	}

	fmt.Printf("📋 Found %d workouts\n", len(workouts))
	fmt.Println("📝 Generating TCX files...")

	tcxGen := tcx.NewGenerator(outputDir)
	files, err := tcxGen.GenerateAll(workouts)
	if err != nil {
		return fmt.Errorf("failed to generate TCX files: %w", err)
	}

	fmt.Printf("\n✅ Exported %d TCX files to %s\n", len(files), outputDir)
	for _, f := range files {
		fmt.Printf("   📄 %s\n", filepath.Base(f))
	}

	return nil
}

func runStatus() error {
	fmt.Println("📊 AimHarder Sync Status")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	fmt.Println("\n🏋️  Aimharder:")
	if cfg.Aimharder.Email != "" {
		fmt.Printf("   Email: %s\n", cfg.Aimharder.Email)
	} else {
		fmt.Println("   ❌ Not configured (set AIMHARDER_EMAIL)")
	}
	fmt.Printf("   Box: %s (ID: %s)\n", cfg.Aimharder.BoxName, cfg.Aimharder.BoxID)

	fmt.Println("\n🏃 Strava:")
	if cfg.Strava.ClientID != "" {
		fmt.Printf("   Client ID: %s\n", cfg.Strava.ClientID)

		stravaClient, err := strava.NewClient(cfg)
		if err == nil && stravaClient.IsAuthenticated() {
			fmt.Println("   ✅ Authenticated")
		} else {
			fmt.Println("   ❌ Not authenticated (run 'auth')")
		}
	} else {
		fmt.Println("   ❌ Not configured (set STRAVA_CLIENT_ID, STRAVA_CLIENT_SECRET)")
	}

	fmt.Println("\n💾 Storage:")
	fmt.Printf("   Data dir: %s\n", cfg.Storage.DataDir)
	fmt.Printf("   TCX dir: %s\n", cfg.Storage.TCXDir)

	history := loadSyncHistory(cfg.Storage.HistoryFile)
	totalSynced := 0
	for _, statuses := range history {
		for _, s := range statuses {
			if s.Success {
				totalSynced++
			}
		}
	}
	fmt.Printf("\n📈 Sync History: %d workouts synced\n", totalSynced)

	return nil
}

// Helper functions

func parseDateRange(days int, startDate, endDate string) (time.Time, time.Time, error) {
	var start, end time.Time
	var err error

	if startDate != "" {
		start, err = time.Parse("2006-01-02", startDate)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start date: %w", err)
		}
	} else {
		start = time.Now().AddDate(0, 0, -days)
	}

	if endDate != "" {
		end, err = time.Parse("2006-01-02", endDate)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end date: %w", err)
		}
	} else {
		end = time.Now()
	}

	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.Local)
	end = time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, time.Local)

	return start, end, nil
}

func loadSyncHistory(filepath string) map[string][]models.SyncStatus {
	history := make(map[string][]models.SyncStatus)

	data, err := os.ReadFile(filepath)
	if err != nil {
		return history
	}

	json.Unmarshal(data, &history)
	return history
}

func saveSyncHistory(filepath string, history map[string][]models.SyncStatus) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0644)
}

func isWorkoutSynced(history map[string][]models.SyncStatus, workoutID string) bool {
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

func recordSync(history map[string][]models.SyncStatus, workoutID, externalID string, success bool, errorMsg string) {
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

func formatDurationForDisplay(d time.Duration) string {
	if d == 0 {
		return "Not specified"
	}

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
