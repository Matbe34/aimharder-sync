package tcx

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aimharder-sync/internal/models"
)

// TCX XML structures following Garmin Training Center Database v2 schema
// Reference: https://www8.garmin.com/xmlschemas/TrainingCenterDatabasev2.xsd

// TrainingCenterDatabase is the root element
type TrainingCenterDatabase struct {
	XMLName        xml.Name    `xml:"TrainingCenterDatabase"`
	XSI            string      `xml:"xmlns:xsi,attr"`
	XSD            string      `xml:"xmlns:xsd,attr"`
	NS             string      `xml:"xmlns,attr"`
	NS2            string      `xml:"xmlns:ns2,attr,omitempty"`
	NS3            string      `xml:"xmlns:ns3,attr,omitempty"`
	NS4            string      `xml:"xmlns:ns4,attr,omitempty"`
	NS5            string      `xml:"xmlns:ns5,attr,omitempty"`
	SchemaLocation string      `xml:"xsi:schemaLocation,attr,omitempty"`
	Activities     *Activities `xml:"Activities,omitempty"`
	Author         *Author     `xml:"Author,omitempty"`
}

// Activities container
type Activities struct {
	Activity []Activity `xml:"Activity"`
}

// Activity represents a single workout
type Activity struct {
	Sport   string  `xml:"Sport,attr"`
	ID      string  `xml:"Id"`
	Lap     []Lap   `xml:"Lap"`
	Notes   string  `xml:"Notes,omitempty"`
	Creator *Device `xml:"Creator,omitempty"`
}

// Lap represents a lap within an activity
type Lap struct {
	StartTime           string         `xml:"StartTime,attr"`
	TotalTimeSeconds    float64        `xml:"TotalTimeSeconds"`
	DistanceMeters      float64        `xml:"DistanceMeters"`
	Calories            int            `xml:"Calories,omitempty"`
	AverageHeartRateBpm *HeartRate     `xml:"AverageHeartRateBpm,omitempty"`
	MaximumHeartRateBpm *HeartRate     `xml:"MaximumHeartRateBpm,omitempty"`
	Intensity           string         `xml:"Intensity"`
	TriggerMethod       string         `xml:"TriggerMethod"`
	Track               *Track         `xml:"Track,omitempty"`
	Notes               string         `xml:"Notes,omitempty"`
	Extensions          *LapExtensions `xml:"Extensions,omitempty"`
}

// Track contains trackpoints
type Track struct {
	Trackpoint []Trackpoint `xml:"Trackpoint"`
}

// Trackpoint represents a single data point
type Trackpoint struct {
	Time         string                `xml:"Time"`
	HeartRateBpm *HeartRate            `xml:"HeartRateBpm,omitempty"`
	Cadence      int                   `xml:"Cadence,omitempty"`
	Extensions   *TrackpointExtensions `xml:"Extensions,omitempty"`
}

// HeartRate holds heart rate value
type HeartRate struct {
	Value int `xml:"Value"`
}

// Device information
type Device struct {
	XSIType   string   `xml:"xsi:type,attr"`
	Name      string   `xml:"Name"`
	UnitId    string   `xml:"UnitId"`
	ProductID int      `xml:"ProductID"`
	Version   *Version `xml:"Version,omitempty"`
}

// Version information
type Version struct {
	VersionMajor int `xml:"VersionMajor"`
	VersionMinor int `xml:"VersionMinor"`
	BuildMajor   int `xml:"BuildMajor,omitempty"`
	BuildMinor   int `xml:"BuildMinor,omitempty"`
}

// Author information
type Author struct {
	XSIType    string `xml:"xsi:type,attr"`
	Name       string `xml:"Name"`
	Build      *Build `xml:"Build,omitempty"`
	LangID     string `xml:"LangID,omitempty"`
	PartNumber string `xml:"PartNumber,omitempty"`
}

// Build information
type Build struct {
	Version *Version `xml:"Version"`
}

// LapExtensions for additional lap data
type LapExtensions struct {
	LX *LX `xml:"LX,omitempty"`
}

// LX extensions namespace
type LX struct {
	XMLNS    string  `xml:"xmlns,attr,omitempty"`
	AvgSpeed float64 `xml:"AvgSpeed,omitempty"`
	MaxSpeed float64 `xml:"MaxSpeed,omitempty"`
	AvgWatts int     `xml:"AvgWatts,omitempty"`
	MaxWatts int     `xml:"MaxWatts,omitempty"`
}

// TrackpointExtensions for additional trackpoint data
type TrackpointExtensions struct {
	TPX *TPX `xml:"TPX,omitempty"`
}

// TPX extensions
type TPX struct {
	XMLNS string  `xml:"xmlns,attr,omitempty"`
	Speed float64 `xml:"Speed,omitempty"`
	Watts int     `xml:"Watts,omitempty"`
}

// Generator creates TCX files from workouts
type Generator struct {
	outputDir       string
	defaultDuration time.Duration
}

// NewGenerator creates a new TCX generator
func NewGenerator(outputDir string, defaultDuration time.Duration) *Generator {
	if defaultDuration == 0 {
		defaultDuration = 60 * time.Minute
	}
	return &Generator{
		outputDir:       outputDir,
		defaultDuration: defaultDuration,
	}
}

// Generate creates a TCX file from a workout
func (g *Generator) Generate(workout *models.Workout) (string, error) {
	// Ensure output directory exists
	if err := os.MkdirAll(g.outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	tcx := g.workoutToTCX(workout)

	// Generate filename
	filename := g.generateFilename(workout)
	filepath := filepath.Join(g.outputDir, filename)

	// Marshal to XML
	output, err := xml.MarshalIndent(tcx, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal TCX: %w", err)
	}

	// Add XML declaration
	xmlContent := xml.Header + string(output)

	// Write to file
	if err := os.WriteFile(filepath, []byte(xmlContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write TCX file: %w", err)
	}

	return filepath, nil
}

// GenerateAll creates TCX files for multiple workouts
func (g *Generator) GenerateAll(workouts []models.Workout) ([]string, error) {
	var files []string

	for i := range workouts {
		filepath, err := g.Generate(&workouts[i])
		if err != nil {
			fmt.Printf("Warning: failed to generate TCX for workout %s: %v\n", workouts[i].ID, err)
			continue
		}
		files = append(files, filepath)
	}

	return files, nil
}

// workoutToTCX converts a Workout to TCX structure
func (g *Generator) workoutToTCX(workout *models.Workout) *TrainingCenterDatabase {
	// Determine sport type for Strava
	sport := g.mapWorkoutTypeToSport(workout.Type)

	// Calculate start time (combine date with class time)
	startTime := g.getStartTime(workout)
	startTimeStr := startTime.Format(time.RFC3339)

	duration := g.defaultDuration
	if workout.Duration > 0 {
		duration = workout.Duration
	}

	// Build notes/description
	notes := g.buildNotes(workout)

	// Create lap
	lap := Lap{
		StartTime:        startTimeStr,
		TotalTimeSeconds: duration.Seconds(),
		DistanceMeters:   0, // CrossFit typically doesn't track distance
		Intensity:        "Active",
		TriggerMethod:    "Manual",
		Notes:            notes,
	}

	// Add heart rate if available
	if workout.Result != nil {
		if workout.Result.AvgHeartRate > 0 {
			lap.AverageHeartRateBpm = &HeartRate{Value: workout.Result.AvgHeartRate}
		}
		if workout.Result.MaxHeartRate > 0 {
			lap.MaximumHeartRateBpm = &HeartRate{Value: workout.Result.MaxHeartRate}
		}
		if workout.Result.Calories > 0 {
			lap.Calories = workout.Result.Calories
		}
	}

	// Default calories if not set (Strava doesn't always use TCX calories, but worth trying)
	if lap.Calories == 0 {
		lap.Calories = 400
	}

	// Generate trackpoints with simulated heart rate for better Strava calorie calculation
	// CrossFit typical HR: warm-up ~120, working ~155, peaks ~175
	lap.Track = generateTrackpointsWithHR(startTime, duration)

	// Set average/max HR on lap level too
	if lap.AverageHeartRateBpm == nil {
		lap.AverageHeartRateBpm = &HeartRate{Value: 150}
	}
	if lap.MaximumHeartRateBpm == nil {
		lap.MaximumHeartRateBpm = &HeartRate{Value: 175}
	}

	// Build activity
	activity := Activity{
		Sport: sport,
		ID:    startTimeStr,
		Lap:   []Lap{lap},
		Notes: notes,
		Creator: &Device{
			XSIType:   "Device_t",
			Name:      "AimHarder Sync",
			UnitId:    "0",
			ProductID: 0,
			Version: &Version{
				VersionMajor: 1,
				VersionMinor: 0,
			},
		},
	}

	// Build TCX document
	tcx := &TrainingCenterDatabase{
		XSI:            "http://www.w3.org/2001/XMLSchema-instance",
		NS:             "http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2",
		NS2:            "http://www.garmin.com/xmlschemas/UserProfile/v2",
		NS3:            "http://www.garmin.com/xmlschemas/ActivityExtension/v2",
		NS4:            "http://www.garmin.com/xmlschemas/ProfileExtension/v1",
		NS5:            "http://www.garmin.com/xmlschemas/ActivityGoals/v1",
		SchemaLocation: "http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2 http://www.garmin.com/xmlschemas/TrainingCenterDatabasev2.xsd",
		Activities: &Activities{
			Activity: []Activity{activity},
		},
		Author: &Author{
			XSIType: "Application_t",
			Name:    "AimHarder Sync",
			Build: &Build{
				Version: &Version{
					VersionMajor: 1,
					VersionMinor: 0,
				},
			},
			LangID:     "en",
			PartNumber: "000-00000-00",
		},
	}

	return tcx
}

// mapWorkoutTypeToSport maps CrossFit workout type to Strava sport
func (g *Generator) mapWorkoutTypeToSport(workoutType models.WorkoutType) string {
	// Strava recognizes these sports from TCX:
	// Running, Biking, Other
	// For CrossFit, "Other" is most appropriate as Strava will then allow
	// changing to "Crossfit" activity type
	switch workoutType {
	case models.WorkoutTypeStrength:
		return "Other" // Will be set to Weight Training in Strava
	default:
		return "Other" // Will be set to Crossfit in Strava
	}
}

// getStartTime calculates the actual start time of the workout
func (g *Generator) getStartTime(workout *models.Workout) time.Time {
	startTime := workout.Date

	// Parse class time if available (format: "HH:MM" or "HHMM")
	if workout.ClassTime != "" {
		classTime := workout.ClassTime
		// Normalize format
		classTime = strings.ReplaceAll(classTime, ":", "")
		if len(classTime) >= 4 {
			hour := 0
			minute := 0
			fmt.Sscanf(classTime[:2], "%d", &hour)
			fmt.Sscanf(classTime[2:4], "%d", &minute)
			startTime = time.Date(
				workout.Date.Year(),
				workout.Date.Month(),
				workout.Date.Day(),
				hour,
				minute,
				0,
				0,
				workout.Date.Location(),
			)
		}
	}

	return startTime
}

// buildNotes creates a comprehensive notes string for the workout
func (g *Generator) buildNotes(workout *models.Workout) string {
	var parts []string

	// Add workout name
	if workout.Name != "" {
		parts = append(parts, fmt.Sprintf("📋 %s", workout.Name))
	}

	// Add workout type
	if workout.Type != "" && workout.Type != models.WorkoutTypeWOD {
		parts = append(parts, fmt.Sprintf("🏋️ Type: %s", workout.Type))
	}

	// Add description
	if workout.Description != "" {
		parts = append(parts, fmt.Sprintf("\n📝 Workout:\n%s", workout.Description))
	}

	// Add result
	if workout.Result != nil {
		parts = append(parts, "\n🎯 Result:")

		if workout.Result.Time != nil {
			parts = append(parts, fmt.Sprintf("  ⏱️ Time: %s", formatDuration(*workout.Result.Time)))
		}

		if workout.Result.Rounds > 0 {
			if workout.Result.Reps > 0 {
				parts = append(parts, fmt.Sprintf("  🔄 Rounds: %d + %d reps", workout.Result.Rounds, workout.Result.Reps))
			} else {
				parts = append(parts, fmt.Sprintf("  🔄 Rounds: %d", workout.Result.Rounds))
			}
		}

		if workout.Result.Weight > 0 {
			parts = append(parts, fmt.Sprintf("  🏋️ Weight: %.1f kg", workout.Result.Weight))
		}

		if workout.Result.Score != "" && workout.Result.Time == nil && workout.Result.Rounds == 0 {
			parts = append(parts, fmt.Sprintf("  📊 Score: %s", workout.Result.Score))
		}

		// Scaling info
		if workout.Result.RxPlus {
			parts = append(parts, "  ⭐ Rx+")
		} else if workout.Result.Scaled {
			parts = append(parts, "  📉 Scaled")
		} else {
			parts = append(parts, "  ✅ Rx")
		}

		// Notes
		if workout.Result.Notes != "" {
			parts = append(parts, fmt.Sprintf("  💬 Notes: %s", workout.Result.Notes))
		}
	}

	// Add box info
	if workout.BoxName != "" {
		parts = append(parts, fmt.Sprintf("\n🏠 Box: %s", workout.BoxName))
	}

	// Add sync info
	parts = append(parts, "\n📤 Synced via AimHarder-Sync")

	return strings.Join(parts, "\n")
}

// generateFilename creates a unique filename for the TCX file
func (g *Generator) generateFilename(workout *models.Workout) string {
	// Format: YYYY-MM-DD_HHMM_workout-name.tcx
	dateStr := workout.Date.Format("2006-01-02")
	timeStr := strings.ReplaceAll(workout.ClassTime, ":", "")
	if timeStr == "" {
		timeStr = "0000"
	}

	// Sanitize workout name for filename
	name := sanitizeFilename(workout.Name)
	if name == "" {
		name = "workout"
	}

	return fmt.Sprintf("%s_%s_%s.tcx", dateStr, timeStr, name)
}

// formatDuration formats a duration as MM:SS or HH:MM:SS
func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// sanitizeFilename removes/replaces invalid filename characters
func sanitizeFilename(name string) string {
	// Replace invalid characters
	invalid := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|", " "}
	result := name
	for _, char := range invalid {
		result = strings.ReplaceAll(result, char, "-")
	}

	// Remove multiple dashes
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}

	// Trim dashes from ends
	result = strings.Trim(result, "-")

	// Lowercase
	result = strings.ToLower(result)

	// Limit length
	if len(result) > 50 {
		result = result[:50]
	}

	return result
}

// generateTrackpointsWithHR creates trackpoints with simulated heart rate data
// This helps Strava calculate more accurate calories for CrossFit workouts
// Pattern: warm-up (5 min, ~120 bpm) -> working (main, ~155 bpm with peaks) -> cool-down (5 min, ~130 bpm)
func generateTrackpointsWithHR(startTime time.Time, duration time.Duration) *Track {
	trackpoints := []Trackpoint{}

	// Generate a trackpoint every 30 seconds
	totalSeconds := int(duration.Seconds())
	numPoints := totalSeconds / 30
	if numPoints < 4 {
		numPoints = 4 // Minimum 4 points
	}
	if numPoints > 120 {
		numPoints = 120 // Max 1 hour of 30s intervals
	}

	warmupDuration := 5 * 60   // 5 minutes warm-up
	cooldownDuration := 5 * 60 // 5 minutes cool-down

	for i := 0; i <= numPoints; i++ {
		elapsed := i * 30 // seconds elapsed
		pointTime := startTime.Add(time.Duration(elapsed) * time.Second)

		// Calculate heart rate based on workout phase
		var hr int
		if elapsed < warmupDuration {
			// Warm-up: 110 -> 140 bpm
			progress := float64(elapsed) / float64(warmupDuration)
			hr = 110 + int(progress*30)
		} else if elapsed > totalSeconds-cooldownDuration {
			// Cool-down: 160 -> 120 bpm
			cooldownElapsed := elapsed - (totalSeconds - cooldownDuration)
			progress := float64(cooldownElapsed) / float64(cooldownDuration)
			hr = 160 - int(progress*40)
		} else {
			// Working phase: 145-170 bpm with variation
			// Use simple sine wave for natural variation
			phase := float64(elapsed) / 60.0 // cycles per minute
			variation := int(12 * (0.5 + 0.5*sinApprox(phase*2)))
			hr = 148 + variation
		}

		// Add some randomness (+/- 3 bpm)
		hr += (elapsed % 7) - 3

		// Clamp to reasonable range
		if hr < 100 {
			hr = 100
		}
		if hr > 185 {
			hr = 185
		}

		trackpoints = append(trackpoints, Trackpoint{
			Time:         pointTime.Format(time.RFC3339),
			HeartRateBpm: &HeartRate{Value: hr},
		})
	}

	return &Track{Trackpoint: trackpoints}
}

// sinApprox is a simple sine approximation without importing math
func sinApprox(x float64) float64 {
	// Normalize to 0-2π range approximation
	for x > 6.28 {
		x -= 6.28
	}
	for x < 0 {
		x += 6.28
	}
	// Simple Taylor series approximation
	x3 := x * x * x
	x5 := x3 * x * x
	return x - x3/6 + x5/120
}
