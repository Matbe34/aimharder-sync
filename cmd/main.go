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
	"github.com/aimharder-sync/internal/garmin"
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
		Short: "Sync your Aimharder CrossFit workouts to Strava and Garmin",
		Long: `AimHarder Sync - Export your CrossFit workouts from Aimharder
and upload them to Strava, Garmin Connect, or export as TCX files.

Before using, you need to:
1. Set up your Aimharder credentials (AIMHARDER_EMAIL, AIMHARDER_PASSWORD)
2. Set up Strava API credentials (STRAVA_CLIENT_ID, STRAVA_CLIENT_SECRET)
3. Run 'aimharder-sync auth strava' to authenticate with Strava`,
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
		platforms []string
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync workouts from Aimharder to fitness platforms",
		Long: `Fetch workouts from Aimharder and upload them to Strava and/or Garmin.
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
			return runSync(days, startDate, endDate, force, platforms)
		},
	}

	cmd.Flags().IntVar(&days, "days", 30, "number of days to sync (from today)")
	cmd.Flags().StringVar(&startDate, "start", "", "start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&endDate, "end", "", "end date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&force, "force", false, "force re-sync of already synced workouts")
	cmd.Flags().StringSliceVar(&platforms, "platform", []string{"strava"}, "platforms to sync to (strava, garmin)")

	return cmd
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth [platform]",
		Short: "Authenticate with fitness platforms",
		Long: `Authenticate with Strava or Garmin Connect.

Examples:
  # Authenticate with Strava (opens browser)
  aimharder-sync auth strava`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuth(args[0])
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

// Command implementations

func runSync(days int, startDate, endDate string, force bool, platforms []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n⚠️  Cancelling... (press Ctrl+C again to force)")
		cancel()
		// Second signal forces exit
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
		if force || !isWorkoutSynced(history, w.ID, platforms) {
			toSync = append(toSync, w)
		}
	}

	if len(toSync) == 0 {
		fmt.Println("✅ All workouts already synced!")
		return nil
	}

	fmt.Printf("🔄 %d workouts to sync\n", len(toSync))

	// Generate TCX files (needed for both dry-run preview and actual sync)
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

		// Create Strava client to get proper activity mapping
		var stravaClient *strava.Client
		if contains(platforms, "strava") {
			if err := cfg.ValidateStrava(); err == nil {
				stravaClient, _ = strava.NewClient(cfg)
			}
		}

		for i, w := range toSync {
			tcxFile := ""
			if i < len(tcxFiles) {
				tcxFile = tcxFiles[i]
			}

			fmt.Printf("\n┌─ Activity %d of %d ─────────────────────────────────────────────────\n", i+1, len(toSync))
			fmt.Printf("│\n")
			fmt.Printf("│ 🔶 STRAVA ACTIVITY PREVIEW\n")
			fmt.Printf("│ %s\n", strings.Repeat("─", 50))

			// Show Strava-specific fields
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
			fmt.Printf("│ 🏃 sport_type:     %s\n", activityType)
			fmt.Printf("│ 📅 start_date:     %s\n", w.Date.Format("2006-01-02T15:04:05Z"))
			fmt.Printf("│ 🆔 external_id:    %s\n", w.ID)
			fmt.Printf("│ 📄 data_type:      tcx\n")
			if tcxFile != "" {
				fmt.Printf("│ 📁 tcx_file:       %s\n", tcxFile)
			}

			// Duration/elapsed time
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

			// Show sections summary
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

			// Show result
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

		// Also show Garmin preview if platform includes garmin
		if contains(platforms, "garmin") {
			showGarminDryRunPreview(cfg, toSync, tcxFiles)
		}

		fmt.Println("\n💡 Run without --dry-run to actually sync these workouts.")
		return nil
	}

	for _, platform := range platforms {
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		default:
		}

		switch platform {
		case "strava":
			if err := syncToStrava(ctx, cfg, toSync, tcxFiles, history); err != nil {
				fmt.Printf("⚠️  Strava sync error: %v\n", err)
			}
		case "garmin":
			if err := syncToGarmin(ctx, cfg, toSync, tcxFiles, history); err != nil {
				fmt.Printf("⚠️  Garmin sync error: %v\n", err)
			}
		default:
			fmt.Printf("⚠️  Unknown platform: %s\n", platform)
		}
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
		return fmt.Errorf("not authenticated with Strava - run 'aimharder-sync auth strava' first")
	}

	fmt.Println("📤 Uploading to Strava...")

	for i, workout := range workouts {
		if i >= len(tcxFiles) {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		tcxFile := tcxFiles[i]
		fmt.Printf("  📤 Uploading: %s - %s...", workout.Date.Format("2006-01-02"), workout.Name)

		uploadResp, err := stravaClient.UploadActivity(ctx, tcxFile, &workout)
		if err != nil {
			fmt.Printf(" ❌ Error: %v\n", err)
			recordSync(history, workout.ID, "strava", "", false, err.Error())
			continue
		}

		status, err := stravaClient.WaitForUpload(ctx, uploadResp.ID, 2*time.Minute)
		if err != nil {
			fmt.Printf(" ❌ Error: %v\n", err)
			recordSync(history, workout.ID, "strava", "", false, err.Error())
			continue
		}

		if status.Error != "" {
			if status.Error == "duplicate" || strings.Contains(status.Error, "duplicate") {
				fmt.Printf(" ⏭️  Already exists\n")
				recordSync(history, workout.ID, "strava", "", true, "duplicate")
			} else {
				fmt.Printf(" ❌ Error: %s\n", status.Error)
				recordSync(history, workout.ID, "strava", "", false, status.Error)
			}
			continue
		}

		fmt.Printf(" ✅ Activity ID: %d\n", status.ActivityID)
		recordSync(history, workout.ID, "strava", fmt.Sprintf("%d", status.ActivityID), true, "")

		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

func syncToGarmin(ctx context.Context, cfg *config.Config, workouts []models.Workout, tcxFiles []string, history map[string][]models.SyncStatus) error {
	if cfg.Garmin.Email == "" || cfg.Garmin.Password == "" {
		return fmt.Errorf("garmin credentials not configured (set GARMIN_EMAIL and GARMIN_PASSWORD)")
	}

	garminClient, err := garmin.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create Garmin client: %w", err)
	}

	// Check if we need to login
	if !garminClient.IsAuthenticated() {
		if err := garminClient.Login(ctx); err != nil {
			return fmt.Errorf("failed to login to Garmin: %w", err)
		}
	}

	if dryRun {
		// Dry run - show preview
		fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("📋 DRY RUN - Garmin Connect Activities that would be uploaded:")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		for i, workout := range workouts {
			tcxFile := ""
			if i < len(tcxFiles) {
				tcxFile = tcxFiles[i]
			}

			preview := garminClient.PreviewActivity(&workout, tcxFile)

			fmt.Printf("\n┌─ Activity %d of %d ─────────────────────────────────────────────────\n", i+1, len(workouts))
			fmt.Println("│")
			fmt.Println("│ 🟠 GARMIN CONNECT ACTIVITY PREVIEW")
			fmt.Println("│ ──────────────────────────────────────────────────")
			fmt.Println("│")
			fmt.Printf("│ 📛 name:           %s\n", preview.Name)
			fmt.Printf("│ 📅 start_date:     %s\n", preview.StartTime.Format("2006-01-02T15:04:05Z"))
			fmt.Printf("│ 🆔 workout_id:     %s\n", preview.WorkoutID)
			fmt.Printf("│ 📄 data_type:      tcx\n")
			fmt.Printf("│ 📁 tcx_file:       %s\n", preview.TCXFile)
			if preview.Duration > 0 {
				fmt.Printf("│ ⏱️  duration:       %s\n", formatDurationForDisplay(preview.Duration))
			}
			fmt.Println("│")
			fmt.Println("│ 📝 description:")
			fmt.Println("│ ──────────────────────────────────────────────────")
			for _, line := range strings.Split(preview.Description, "\n") {
				fmt.Printf("│    %s\n", line)
			}
			fmt.Println("│")
			fmt.Println("└─────────────────────────────────────────────────────────────────────")
		}

		fmt.Printf("\n📊 Summary: %d activities would be uploaded to Garmin Connect\n", len(workouts))
		fmt.Println("\n💡 Run without --dry-run to actually sync these workouts.")
		return nil
	}

	// Actual upload
	fmt.Println("📤 Uploading to Garmin Connect...")

	for i, workout := range workouts {
		if i >= len(tcxFiles) {
			break
		}

		tcxFile := tcxFiles[i]

		fmt.Printf("  📤 Uploading: %s - %s...", workout.Date.Format("2006-01-02"), workout.Name)

		uploadResp, err := garminClient.UploadActivity(ctx, tcxFile, &workout)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "already exists") {
				fmt.Printf(" ⏭️  Already exists\n")
				recordSync(history, workout.ID, "garmin", "", true, "duplicate")
			} else {
				fmt.Printf(" ❌ Error: %v\n", err)
				recordSync(history, workout.ID, "garmin", "", false, err.Error())
			}
			continue
		}

		if len(uploadResp.DetailedImportResult.Successes) > 0 {
			activityID := uploadResp.DetailedImportResult.Successes[0].InternalID
			fmt.Printf(" ✅ Activity ID: %d\n", activityID)
			recordSync(history, workout.ID, "garmin", fmt.Sprintf("%d", activityID), true, "")
		} else {
			fmt.Printf(" ✅ Uploaded (ID: %d)\n", uploadResp.DetailedImportResult.UploadID)
			recordSync(history, workout.ID, "garmin", fmt.Sprintf("%d", uploadResp.DetailedImportResult.UploadID), true, "")
		}

		time.Sleep(1 * time.Second) // Rate limiting for Garmin
	}

	return nil
}

// showGarminDryRunPreview displays a preview of Garmin activities for dry-run mode
func showGarminDryRunPreview(cfg *config.Config, workouts []models.Workout, tcxFiles []string) {
	fmt.Println("\n" + strings.Repeat("━", 70))
	fmt.Println("📋 DRY RUN - Garmin Connect Activities that would be uploaded:")
	fmt.Println(strings.Repeat("━", 70))

	for i, w := range workouts {
		tcxFile := ""
		if i < len(tcxFiles) {
			tcxFile = tcxFiles[i]
		}

		fmt.Printf("\n┌─ Activity %d of %d ─────────────────────────────────────────────────\n", i+1, len(workouts))
		fmt.Printf("│\n")
		fmt.Printf("│ 🟠 GARMIN CONNECT ACTIVITY PREVIEW\n")
		fmt.Printf("│ %s\n", strings.Repeat("─", 50))
		fmt.Printf("│\n")
		fmt.Printf("│ 📛 name:           %s\n", w.Name)
		fmt.Printf("│ 📅 start_date:     %s\n", w.Date.Format("2006-01-02T15:04:05Z"))
		fmt.Printf("│ 🆔 workout_id:     %s\n", w.ID)
		fmt.Printf("│ 📄 data_type:      tcx\n")
		fmt.Printf("│ 📁 tcx_file:       %s\n", tcxFile)
		if w.Duration > 0 {
			fmt.Printf("│ ⏱️  duration:       %s\n", formatDurationForDisplay(w.Duration))
		}
		fmt.Printf("│\n")
		fmt.Printf("│ 📝 description:\n")
		fmt.Printf("│ %s\n", strings.Repeat("─", 50))
		for _, line := range strings.Split(w.FormatDescription(), "\n") {
			fmt.Printf("│    %s\n", line)
		}
		fmt.Printf("└%s\n", strings.Repeat("─", 69))
	}

	fmt.Printf("\n📊 Summary: %d activities would be uploaded to Garmin Connect\n", len(workouts))
}

func runAuth(platform string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	switch platform {
	case "strava":
		if err := cfg.ValidateStrava(); err != nil {
			return err
		}

		stravaClient, err := strava.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create Strava client: %w", err)
		}

		return stravaClient.StartOAuthFlow(ctx)

	case "garmin":
		if cfg.Garmin.Email == "" || cfg.Garmin.Password == "" {
			return fmt.Errorf("garmin email and password are required (set GARMIN_EMAIL and GARMIN_PASSWORD)")
		}

		garminClient, err := garmin.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create Garmin client: %w", err)
		}

		return garminClient.Login(ctx)

	default:
		return fmt.Errorf("unknown platform: %s (supported: strava, garmin)", platform)
	}
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
			// Show date with time if available
			if w.Date.Hour() > 0 || w.Date.Minute() > 0 {
				fmt.Printf("📅 %s @ %s - %s\n", w.Date.Format("2006-01-02 (Monday)"), w.Date.Format("15:04"), w.Name)
			} else {
				fmt.Printf("📅 %s - %s\n", w.Date.Format("2006-01-02 (Monday)"), w.Name)
			}
			fmt.Printf("   🏠 %s | 🏋️ %s\n", w.BoxName, w.Type)

			// Show sections with full details
			if len(w.Sections) > 0 {
				fmt.Println("\n   📋 Workout Structure:")
				for i, s := range w.Sections {
					line := fmt.Sprintf("      [%d] %s", i+1, s.Name)
					if s.TimeCap > 0 {
						line += fmt.Sprintf(" (%d min cap)", s.TimeCap)
					}
					fmt.Println(line)

					// Section result - show rounds/reps completed
					resultLine := "          → "
					hasResult := false

					// For AMRAP: show "5R + 10 reps" format
					if s.RoundsCompleted > 0 && s.RepsAchieved > 0 {
						resultLine += fmt.Sprintf("%dR + %d reps", s.RoundsCompleted, s.RepsAchieved)
						hasResult = true
					} else if s.RoundsCompleted > 0 {
						// For EMOM/other: show "4/4 sets" or just rounds
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

					// Section notes
					if s.Notes != "" {
						// Clean up HTML entities
						notes := strings.ReplaceAll(s.Notes, "&quot;", "\"")
						notes = strings.ReplaceAll(notes, "&#39;", "'")
						notes = strings.ReplaceAll(notes, "\u2019", "'")
						fmt.Printf("          📝 %s\n", notes)
					}

					// Show exercises for this section
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

			// Show exercises without section or if only one section (group all together)
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

			// Show result
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
			fmt.Println("   ❌ Not authenticated (run 'auth strava')")
		}
	} else {
		fmt.Println("   ❌ Not configured (set STRAVA_CLIENT_ID, STRAVA_CLIENT_SECRET)")
	}

	fmt.Println("\n⌚ Garmin:")
	fmt.Println("   ⚠️  Not yet implemented")

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

func isWorkoutSynced(history map[string][]models.SyncStatus, workoutID string, platforms []string) bool {
	statuses, ok := history[workoutID]
	if !ok {
		return false
	}

	for _, platform := range platforms {
		synced := false
		for _, s := range statuses {
			if s.Platform == platform && s.Success {
				synced = true
				break
			}
		}
		if !synced {
			return false
		}
	}

	return true
}

func recordSync(history map[string][]models.SyncStatus, workoutID, platform, externalID string, success bool, errorMsg string) {
	status := models.SyncStatus{
		WorkoutID:    workoutID,
		Platform:     platform,
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

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func mapWorkoutTypeForDisplay(workoutType models.WorkoutType) string {
	switch workoutType {
	case models.WorkoutTypeStrength:
		return "Weight Training"
	case models.WorkoutTypeAMRAP:
		return "CrossFit (AMRAP)"
	case models.WorkoutTypeForTime:
		return "CrossFit (For Time)"
	case models.WorkoutTypeEMOM:
		return "CrossFit (EMOM)"
	case models.WorkoutTypeTabata:
		return "CrossFit (Tabata)"
	case models.WorkoutTypeHero:
		return "CrossFit (Hero WOD)"
	case models.WorkoutTypeGirl:
		return "CrossFit (Benchmark)"
	case models.WorkoutTypeOpen:
		return "CrossFit (Open)"
	case models.WorkoutTypeSkill:
		return "CrossFit (Skill)"
	default:
		return "CrossFit"
	}
}
