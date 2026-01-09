package models

import (
	"fmt"
	"strings"
	"time"
)

// Workout represents a CrossFit workout/WOD from Aimharder
type Workout struct {
	ID          string         `json:"id"`
	Date        time.Time      `json:"date"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Type        WorkoutType    `json:"type"`
	Duration    time.Duration  `json:"duration"`
	Result      *WorkoutResult `json:"result,omitempty"`
	BoxID       string         `json:"box_id"`
	BoxName     string         `json:"box_name"`
	ClassTime   string         `json:"class_time"`
	Synced      bool           `json:"synced"`
	SyncedAt    *time.Time     `json:"synced_at,omitempty"`
	ExternalID  string         `json:"external_id,omitempty"`

	// Detailed workout structure
	Sections  []WorkoutSection `json:"sections,omitempty"`
	Exercises []Exercise       `json:"exercises,omitempty"`
}

// WorkoutSection represents a section of a workout (e.g., "EMOM 12'", "For Time")
type WorkoutSection struct {
	ID              string      `json:"id,omitempty"`
	Name            string      `json:"name"`
	Type            WorkoutType `json:"type"`
	TimeCap         int         `json:"time_cap,omitempty"`
	Rounds          int         `json:"rounds,omitempty"`
	RoundsCompleted int         `json:"rounds_completed,omitempty"`
	RepsAchieved    int         `json:"reps_achieved,omitempty"`
	Time            string      `json:"time,omitempty"`
	RX              bool        `json:"rx"`
	Rank            int         `json:"rank,omitempty"`
	Notes           string      `json:"notes,omitempty"`
	Description     string      `json:"description,omitempty"`
}

// Exercise represents a single exercise in a workout
type Exercise struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	SectionIndex int     `json:"section_index,omitempty"`
	Round        int     `json:"round,omitempty"`
	RepsPerRound int     `json:"reps_per_round,omitempty"`
	Reps         int     `json:"reps,omitempty"`
	Weight       float64 `json:"weight,omitempty"`
	WeightUnit   string  `json:"weight_unit,omitempty"`
	Distance     float64 `json:"distance,omitempty"`
	DistanceUnit string  `json:"distance_unit,omitempty"`
	Calories     int     `json:"calories,omitempty"`
	Time         string  `json:"time,omitempty"`
	ImageURL     string  `json:"image_url,omitempty"`
	VideoID      string  `json:"video_id,omitempty"`
	PR           bool    `json:"pr,omitempty"`
	Notes        string  `json:"notes,omitempty"`
	FormatType   int     `json:"format_type,omitempty"`
}

// FormatDescription generates a human-readable description of the workout
// formatted for clean display in Strava
func (w *Workout) FormatDescription() string {
	var lines []string

	// Process each section
	for i, section := range w.Sections {
		// Add separator between sections (except first)
		if i > 0 {
			lines = append(lines, "")
			lines = append(lines, "─────────────────────────")
			lines = append(lines, "")
		}

		// Section header with emoji
		lines = append(lines, "🏋️ "+section.Name)

		// Section notes (workout instructions)
		if section.Notes != "" {
			notes := cleanHTMLEntities(section.Notes)
			lines = append(lines, notes)
		}

		// Exercises for this section (skip placeholder exercises like "Descanso Rest")
		lines = append(lines, "")
		for _, ex := range w.Exercises {
			if ex.SectionIndex == i && !isPlaceholderExercise(ex.Name) {
				exLine := formatExerciseLine(ex)
				lines = append(lines, exLine)
			}
		}

		// Section result
		lines = append(lines, "")
		if section.RoundsCompleted > 0 && section.RepsAchieved > 0 {
			lines = append(lines, fmt.Sprintf("✅ %dR + %d reps", section.RoundsCompleted, section.RepsAchieved))
		} else if section.RoundsCompleted > 0 {
			lines = append(lines, fmt.Sprintf("✅ %d/%d sets", section.RoundsCompleted, section.RoundsCompleted))
		} else if section.RepsAchieved > 0 {
			lines = append(lines, fmt.Sprintf("✅ %d reps", section.RepsAchieved))
		}
		if section.RX {
			lines = append(lines, "💪 RX")
		}
	}

	// If no sections, show exercises directly
	if len(w.Sections) == 0 && len(w.Exercises) > 0 {
		lines = append(lines, "🏋️ "+w.Name)
		lines = append(lines, "")
		for _, ex := range w.Exercises {
			exLine := formatExerciseLine(ex)
			lines = append(lines, exLine)
		}
	}

	// Add overall result
	if w.Result != nil {
		lines = append(lines, "")
		lines = append(lines, "─────────────────────────")
		lines = append(lines, "")

		if w.Result.Time != nil {
			lines = append(lines, "⏱️ "+formatDuration(*w.Result.Time))
		}
		if w.Result.Rounds > 0 {
			if w.Result.Reps > 0 {
				lines = append(lines, fmt.Sprintf("🔄 %d rounds + %d reps", w.Result.Rounds, w.Result.Reps))
			} else {
				lines = append(lines, fmt.Sprintf("🔄 %d rounds", w.Result.Rounds))
			}
		}
		if w.Result.Weight > 0 {
			lines = append(lines, fmt.Sprintf("🏋️ %.0f kg", w.Result.Weight))
		}

		if w.Result.RxPlus {
			lines = append(lines, "⭐ Rx+")
		} else if !w.Result.Scaled {
			lines = append(lines, "💪 RX")
		} else {
			lines = append(lines, "📉 Scaled")
		}
	}

	return strings.Join(lines, "\n")
}

// formatExerciseLine formats a single exercise line
func formatExerciseLine(ex Exercise) string {
	var parts []string

	// Reps or distance first
	if ex.Distance > 0 {
		unit := ex.DistanceUnit
		if unit == "" {
			unit = "m"
		}
		parts = append(parts, fmt.Sprintf("%.0f%s", ex.Distance, unit))
	} else if ex.RepsPerRound > 0 {
		parts = append(parts, fmt.Sprintf("%d", ex.RepsPerRound))
	} else if ex.Reps > 0 {
		parts = append(parts, fmt.Sprintf("%d", ex.Reps))
	}

	// Exercise name
	parts = append(parts, ex.Name)

	// Weight
	if ex.Weight > 0 {
		unit := ex.WeightUnit
		if unit == "" {
			unit = "kg"
		}
		parts = append(parts, fmt.Sprintf("@ %.0f%s", ex.Weight, unit))
	}

	// Calories
	if ex.Calories > 0 {
		parts = append(parts, fmt.Sprintf("%d cal", ex.Calories))
	}

	line := "→ " + joinNonEmpty(parts, " ")

	if ex.PR {
		line += " 🏆"
	}

	return line
}

// joinNonEmpty joins non-empty strings with separator
func joinNonEmpty(parts []string, sep string) string {
	var result []string
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return strings.Join(result, sep)
}

// cleanHTMLEntities replaces common HTML entities with their characters
func cleanHTMLEntities(s string) string {
	// Replace BR tags with newlines
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")

	// Replace HTML entities
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&nbsp;", " ")

	// Replace Unicode quotes
	s = strings.ReplaceAll(s, "\u2019", "'")
	s = strings.ReplaceAll(s, "\u2018", "'")
	s = strings.ReplaceAll(s, "\u201c", "\"")
	s = strings.ReplaceAll(s, "\u201d", "\"")

	// Clean up multiple newlines
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(s)
}

// isPlaceholderExercise returns true for placeholder exercises that should be filtered out
func isPlaceholderExercise(name string) bool {
	placeholders := []string{
		"Descanso Rest",
		"Rest",
		"Descanso",
	}
	nameLower := strings.ToLower(name)
	for _, p := range placeholders {
		if strings.ToLower(p) == nameLower {
			return true
		}
	}
	return false
}

func formatReps(reps int) string {
	if reps == 1 {
		return "1 rep"
	}
	return fmt.Sprintf("%d reps", reps)
}

func formatWeight(weight float64, unit string) string {
	if weight == float64(int(weight)) {
		return fmt.Sprintf("%.0f%s", weight, unit)
	}
	return fmt.Sprintf("%.1f%s", weight, unit)
}

func formatDistance(dist float64, unit string) string {
	if dist == float64(int(dist)) {
		return fmt.Sprintf("%.0f%s", dist, unit)
	}
	return fmt.Sprintf("%.1f%s", dist, unit)
}

func formatCalories(cals int) string {
	return fmt.Sprintf("%dcal", cals)
}

func formatDuration(d time.Duration) string {
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if mins > 0 {
		return fmt.Sprintf("%d:%02d", mins, secs)
	}
	return fmt.Sprintf("%ds", secs)
}

func formatRoundsReps(rounds, reps int) string {
	if reps > 0 {
		return fmt.Sprintf("%d+%d", rounds, reps)
	}
	return fmt.Sprintf("%d rounds", rounds)
}

// WorkoutType represents the type of CrossFit workout
type WorkoutType string

const (
	WorkoutTypeAMRAP    WorkoutType = "AMRAP"
	WorkoutTypeForTime  WorkoutType = "ForTime"
	WorkoutTypeEMOM     WorkoutType = "EMOM"
	WorkoutTypeTabata   WorkoutType = "Tabata"
	WorkoutTypeStrength WorkoutType = "Strength"
	WorkoutTypeSkill    WorkoutType = "Skill"
	WorkoutTypeWOD      WorkoutType = "WOD"
	WorkoutTypeOpen     WorkoutType = "Open"
	WorkoutTypeHero     WorkoutType = "Hero"
	WorkoutTypeGirl     WorkoutType = "Girl"
	WorkoutTypeCustom   WorkoutType = "Custom"
)

// WorkoutResult holds the athlete's result for a workout
type WorkoutResult struct {
	Time         *time.Duration `json:"time,omitempty"`
	Rounds       int            `json:"rounds,omitempty"`
	Reps         int            `json:"reps,omitempty"`
	Weight       float64        `json:"weight,omitempty"`
	WeightLbs    float64        `json:"weight_lbs,omitempty"`
	Score        string         `json:"score,omitempty"`
	Scaled       bool           `json:"scaled"`
	RxPlus       bool           `json:"rx_plus"`
	Notes        string         `json:"notes,omitempty"`
	AvgHeartRate int            `json:"avg_heart_rate,omitempty"`
	MaxHeartRate int            `json:"max_heart_rate,omitempty"`
	Calories     int            `json:"calories,omitempty"`
}

// Booking represents a class booking from Aimharder
type Booking struct {
	ID        string   `json:"id"`
	Date      string   `json:"date"`
	Time      string   `json:"time"`
	ClassName string   `json:"class_name"`
	BoxID     string   `json:"box_id"`
	BoxName   string   `json:"box_name"`
	Attended  bool     `json:"attended"`
	WOD       *WODInfo `json:"wod,omitempty"`
}

// WODInfo contains the WOD details for a class
type WODInfo struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Type        WorkoutType `json:"type"`
	TimeCap     int         `json:"time_cap,omitempty"`
}

// SyncStatus tracks the sync state for a workout
type SyncStatus struct {
	WorkoutID    string     `json:"workout_id"`
	Platform     string     `json:"platform"`
	ExternalID   string     `json:"external_id"`
	SyncedAt     time.Time  `json:"synced_at"`
	Success      bool       `json:"success"`
	ErrorMessage string     `json:"error_message,omitempty"`
	RetryCount   int        `json:"retry_count"`
	LastRetryAt  *time.Time `json:"last_retry_at,omitempty"`
}

// AimharderSession holds auth session data
type AimharderSession struct {
	Cookies   map[string]string `json:"cookies"`
	UserID    string            `json:"user_id"`
	FamilyID  string            `json:"family_id,omitempty"`
	BoxID     string            `json:"box_id"`
	BoxName   string            `json:"box_name"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// StravaTokens holds OAuth tokens for Strava
type StravaTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	AthleteID    int64     `json:"athlete_id"`
}

// ExportOptions configures what to export/sync
type ExportOptions struct {
	StartDate      time.Time
	EndDate        time.Time
	IncludeNoScore bool
	Platforms      []string
	DryRun         bool
	Force          bool
}
