package aimharder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aimharder-sync/internal/config"
	"github.com/aimharder-sync/internal/models"
)

// Client handles communication with Aimharder
type Client struct {
	http     *HTTPClient
	config   *config.Config
	boxURL   string
	loggedIn bool
	userID   string
	familyID string
	verbose  bool
}

// NewClient creates a new Aimharder client
func NewClient(cfg *config.Config) (*Client, error) {
	httpClient, err := NewHTTPClient(true)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	client := &Client{
		http:     httpClient,
		config:   cfg,
		boxURL:   cfg.GetBoxURL(),
		userID:   cfg.Aimharder.UserID,
		familyID: cfg.Aimharder.FamilyID,
		verbose:  true,
	}

	return client, nil
}

// Login authenticates with Aimharder using the auth module
func (c *Client) Login() error {
	c.http.SetVerbose(c.verbose)

	result, err := c.http.Login(c.config.Aimharder.Email, c.config.Aimharder.Password)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if c.verbose {
		fmt.Printf("  ✓ Authentication successful (cookie: %s...)\n", result.AuthCookie[:min(10, len(result.AuthCookie))])
	}

	// User ID should be set from config
	if c.userID == "" {
		return fmt.Errorf("user ID not configured - set AIMHARDER_USER_ID in .env")
	}

	c.loggedIn = true
	if c.verbose {
		fmt.Println("  ✓ Login successful")
	}
	return nil
}

// doAPIRequest performs an API request with proper headers
func (c *Client) doAPIRequest(ctx context.Context, method, urlStr string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Apply API headers
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Referer", baseURL)

	return c.http.GetHTTPClient().Do(req)
}

// doPageRequest performs a page request with proper headers
func (c *Client) doPageRequest(ctx context.Context, urlStr, referer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Apply page headers
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Cache-Control", "max-age=0")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	return c.http.GetHTTPClient().Do(req)
}

// fetchUserID fetches the user ID from the schedule or profile page
func (c *Client) fetchUserID() (string, error) {
	ctx := context.Background()

	resp, err := c.doPageRequest(ctx, c.boxURL+"/schedule", c.boxURL)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	bodyStr := string(body)

	// Look for userId patterns
	patterns := []string{
		`userId['":\s]+(\d+)`,
		`userID['":\s]+(\d+)`,
		`user_id['":\s]+(\d+)`,
		`"id"\s*:\s*(\d+).*?"type"\s*:\s*"user"`,
		`data-userid="(\d+)"`,
		`var\s+userId\s*=\s*(\d+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(bodyStr); len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("user ID not found")
}

// GetBookings fetches the user's logged workouts for a specific date
func (c *Client) GetBookings(ctx context.Context, date time.Time) ([]models.Booking, error) {
	if !c.loggedIn {
		return nil, fmt.Errorf("not logged in")
	}

	// Use the /api/activity endpoint which is what the profile page uses to fetch logged WODs
	// Parameters: timeLineFormat=0 (new), timeLineContent=2 (workouts), userID=<user_id>
	apiURL := fmt.Sprintf("%s/api/activity?timeLineFormat=0&timeLineContent=2&userID=%s&_=%d",
		baseURL,
		c.userID,
		time.Now().UnixMilli(),
	)

	resp, err := c.doAPIRequest(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if c.verbose {
		fmt.Printf("\n  → Activity API (%d): %s\n", resp.StatusCode, string(body)[:min(1000, len(body))])
	}

	if resp.StatusCode != http.StatusOK {
		return nil, nil // No results
	}

	return c.parseActivityAsBookings(body, date)
}

// parseActivityAsBookings parses the activity API response into bookings
func (c *Client) parseActivityAsBookings(body []byte, targetDate time.Time) ([]models.Booking, error) {
	var bookings []models.Booking

	// The activity API returns an array of activity items
	var activities []map[string]interface{}
	if err := json.Unmarshal(body, &activities); err != nil {
		return nil, nil
	}

	targetDateStr := targetDate.Format("2006-01-02")

	for _, activity := range activities {
		// Check activity type - we want workout activities
		actType, _ := activity["type"].(float64)

		// Type 1 seems to be workout activity based on profile JS
		if actType != 1 {
			continue
		}

		// Extract the date from recordDate or publishDate
		var activityDate string
		if recordDate, ok := activity["recordDate"].(string); ok {
			activityDate = recordDate[:10] // Extract YYYY-MM-DD part
		} else if publishDate, ok := activity["publishDate"].(string); ok {
			activityDate = publishDate[:10]
		}

		// Filter by target date if specified
		if targetDateStr != "" && activityDate != targetDateStr {
			continue
		}

		booking := &models.Booking{
			Date:     strings.ReplaceAll(activityDate, "-", ""),
			BoxID:    c.config.Aimharder.BoxID,
			BoxName:  c.config.Aimharder.BoxName,
			Attended: true, // If it's in activity, user logged it
		}

		// Extract session ID (SEID)
		if seid, ok := activity["sessionId"].(float64); ok {
			booking.ID = strconv.FormatFloat(seid, 'f', 0, 64)
		} else if seid, ok := activity["SEID"].(float64); ok {
			booking.ID = strconv.FormatFloat(seid, 'f', 0, 64)
		} else if id, ok := activity["id"].(float64); ok {
			booking.ID = strconv.FormatFloat(id, 'f', 0, 64)
		}

		// Extract workout name
		if name, ok := activity["wodName"].(string); ok && name != "" {
			booking.ClassName = name
		} else if name, ok := activity["name"].(string); ok && name != "" {
			booking.ClassName = name
		} else if name, ok := activity["title"].(string); ok && name != "" {
			booking.ClassName = name
		}

		if booking.ID != "" {
			bookings = append(bookings, *booking)
		}
	}

	return bookings, nil
}

// parseResultsAsBookings parses the results API response into bookings (legacy)
func (c *Client) parseResultsAsBookings(body []byte, dateStr string) ([]models.Booking, error) {
	var bookings []models.Booking

	// Try parsing as array first
	var results []map[string]interface{}
	if err := json.Unmarshal(body, &results); err != nil {
		// Try as object with results array
		var response map[string]interface{}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, nil
		}

		// Look for results in various keys
		for _, key := range []string{"results", "wods", "workouts", "data"} {
			if arr, ok := response[key].([]interface{}); ok {
				for _, item := range arr {
					if m, ok := item.(map[string]interface{}); ok {
						results = append(results, m)
					}
				}
				break
			}
		}
	}

	for _, result := range results {
		booking := &models.Booking{
			Date:     dateStr,
			BoxID:    c.config.Aimharder.BoxID,
			BoxName:  c.config.Aimharder.BoxName,
			Attended: true, // If it's in results, user logged it
		}

		// Extract ID
		if id, ok := result["id"].(string); ok {
			booking.ID = id
		} else if id, ok := result["id"].(float64); ok {
			booking.ID = strconv.FormatFloat(id, 'f', 0, 64)
		}

		// Extract class/workout name
		for _, key := range []string{"name", "nombre", "className", "wodName", "titulo"} {
			if name, ok := result[key].(string); ok && name != "" {
				booking.ClassName = name
				break
			}
		}

		// Extract time
		for _, key := range []string{"time", "hora", "startTime"} {
			if t, ok := result[key].(string); ok && t != "" {
				booking.Time = t
				break
			}
		}

		if booking.ClassName != "" || booking.ID != "" {
			bookings = append(bookings, *booking)
		}
	}

	return bookings, nil
}

// GetWOD fetches the WOD for a specific date and class
func (c *Client) GetWOD(ctx context.Context, date time.Time, classID string) (*models.WODInfo, error) {
	if !c.loggedIn {
		return nil, fmt.Errorf("not logged in")
	}

	dateStr := date.Format("20060102")

	endpoints := []string{
		fmt.Sprintf("%s/api/wod?day=%s&classId=%s&box=%s", c.boxURL, dateStr, classID, c.config.Aimharder.BoxID),
		fmt.Sprintf("%s/api/workout?day=%s&classId=%s&box=%s", c.boxURL, dateStr, classID, c.config.Aimharder.BoxID),
	}

	for _, apiURL := range endpoints {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := c.doAPIRequest(ctx, "GET", apiURL, nil)
		if err != nil {
			continue
		}

		if resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			wod, err := c.parseWODResponse(body)
			if err == nil && wod != nil {
				return wod, nil
			}
		}
		resp.Body.Close()
	}

	return nil, nil
}

// GetWorkoutHistory fetches historical workout data with progress
func (c *Client) GetWorkoutHistory(ctx context.Context, startDate, endDate time.Time) ([]models.Workout, error) {
	if !c.loggedIn {
		return nil, fmt.Errorf("not logged in")
	}

	fmt.Printf("  📥 Fetching your logged workouts from %s to %s...\n", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	// Fetch all activities from the user's profile
	allActivities, err := c.fetchAllActivities(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch activities: %w", err)
	}

	fmt.Printf("  → Found %d total activities\n", len(allActivities))

	// Filter activities by date range
	var allWorkouts []models.Workout
	for _, activity := range allActivities {
		// Parse date and optional time
		var activityDate time.Time
		var err error
		if activity.LoggedAt != "" {
			activityDate, err = time.Parse("2006-01-02 15:04:05", activity.Date+" "+activity.LoggedAt)
		} else {
			activityDate, err = time.Parse("2006-01-02", activity.Date)
		}
		if err != nil {
			continue
		}

		// Check if activity is within date range (compare date only)
		activityDateOnly := time.Date(activityDate.Year(), activityDate.Month(), activityDate.Day(), 0, 0, 0, 0, activityDate.Location())
		if activityDateOnly.Before(startDate) || activityDateOnly.After(endDate) {
			continue
		}

		workout := c.activityToWorkout(activity, activityDate)
		allWorkouts = append(allWorkouts, workout)
	}

	fmt.Printf("  ✅ Found %d workouts in date range\n", len(allWorkouts))

	return allWorkouts, nil
}

// fetchAllActivities fetches all user activities from the profile API
func (c *Client) fetchAllActivities(ctx context.Context) ([]ActivityItem, error) {
	var allActivities []ActivityItem
	var lastLoaded int64 = 0

	for {
		select {
		case <-ctx.Done():
			return allActivities, ctx.Err()
		default:
		}

		apiURL := fmt.Sprintf("%s/api/activity?timeLineFormat=0&timeLineContent=2&userID=%s&_=%d",
			baseURL,
			c.userID,
			time.Now().UnixMilli(),
		)

		if lastLoaded > 0 {
			apiURL = fmt.Sprintf("%s/api/activity?timeLineFormat=2&timeLineContent=2&userID=%s&loadAfter=%d&_=%d",
				baseURL,
				c.userID,
				lastLoaded,
				time.Now().UnixMilli(),
			)
		}

		resp, err := c.doAPIRequest(ctx, "GET", apiURL, nil)
		if err != nil {
			return nil, err
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if c.verbose && lastLoaded == 0 {
			fmt.Printf("  → Activity API URL: %s\n", apiURL)
			fmt.Printf("  → Activity API response (%d bytes): %s\n", len(body), string(body)[:min(500, len(body))])
		}

		if resp.StatusCode != http.StatusOK {
			break
		}

		activities, newLastLoaded := c.parseActivityResponse(body)
		if len(activities) == 0 {
			break
		}

		allActivities = append(allActivities, activities...)

		if newLastLoaded == 0 || newLastLoaded == lastLoaded {
			break
		}
		lastLoaded = newLastLoaded

		fmt.Printf("\r  → Loaded %d activities...", len(allActivities))
		time.Sleep(500 * time.Millisecond) // Be nice to the server
	}

	fmt.Println()
	return allActivities, nil
}

// ActivityItem represents a parsed activity from the API
type ActivityItem struct {
	ID        string
	SessionID string
	Date      string
	LoggedAt  string // Full timestamp when logged (HH:MM:SS)
	Name      string
	Type      int
	Time      string
	Score     string
	RX        bool
	BoxName   string

	// Detailed data
	Sections  []SectionItem
	Exercises []ExerciseItem
}

// SectionItem represents a workout section (EMOM, AMRAP, etc.)
type SectionItem struct {
	ID              string
	Title           string
	Type            int
	Time            string
	TimeCap         int
	Rounds          int
	RoundsCompleted int // From 'res' field - actual rounds/sets completed
	Reps            int // Reps achieved (for AMRAP results)
	RX              bool
	// Rank            int
	Notes string
}

// ExerciseItem represents a single exercise with details
type ExerciseItem struct {
	ID           string
	Name         string
	SectionIndex int // Which TIPOWODs section (tipoWOD field)
	Round        int
	RoundReps    int
	Reps         int
	Weight       float64
	WeightUnit   string
	Distance     float64
	DistanceUnit string
	Calories     int
	Time         string
	ImageURL     string
	VideoID      string
	WodName      string
	PR           bool
	FormatType   int // formaReg: 2=distance, 3=count, 4=weight+reps
}

// parseActivityResponse parses the activity API JSON response
func (c *Client) parseActivityResponse(body []byte) ([]ActivityItem, int64) {
	var response struct {
		Elements    []map[string]interface{} `json:"elements"`
		FirstLoaded int64                    `json:"firstLoaded"`
		LastLoaded  int64                    `json:"lastLoaded"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, 0
	}

	var items []ActivityItem

	for _, act := range response.Elements {
		item := ActivityItem{}

		// Extract ID
		if id, ok := act["id"].(float64); ok {
			item.ID = strconv.FormatFloat(id, 'f', 0, 64)
		}

		// Extract date from "when" field (format: YYYYMMDDHHMMSS)
		if when, ok := act["when"].(string); ok && len(when) >= 8 {
			item.Date = fmt.Sprintf("%s-%s-%s", when[0:4], when[4:6], when[6:8])
			// Extract time if available (HHMMSS)
			if len(when) >= 14 {
				item.LoggedAt = fmt.Sprintf("%s:%s:%s", when[8:10], when[10:12], when[12:14])
			}
		} else if day, ok := act["day"].(string); ok {
			item.Date = c.parseActivityDate(day)
		}

		// Extract box name
		if box, ok := act["box"].(string); ok {
			item.BoxName = box
		}

		// Parse TIPOWODs (workout sections like EMOM, AMRAP, etc.)
		if tipowods, ok := act["TIPOWODs"].([]interface{}); ok {
			for _, tw := range tipowods {
				if twMap, ok := tw.(map[string]interface{}); ok {
					section := SectionItem{}

					// ID
					if id, ok := twMap["id"].(string); ok {
						section.ID = id
					}

					// Title
					if title, ok := twMap["title"].(string); ok {
						section.Title = title
					}

					// Type (2=AMRAP, 3=EMOM, 5=ForTime, etc.)
					if t, ok := twMap["type"].(string); ok {
						section.Type, _ = strconv.Atoi(t)
					}

					// TimeCap in minutes
					if tc, ok := twMap["timecap"].(string); ok {
						section.TimeCap, _ = strconv.Atoi(tc)
					}

					// Notes (detailed workout instructions) - parse early for EMOM naming
					if notes, ok := twMap["notes"].(string); ok {
						section.Notes = notes

						// Try to extract actual time cap from notes if timecap field is unreliable
						// Look for patterns like "TC 40'" or "Time Cap: 40 min"
						if section.TimeCap <= 1 && notes != "" {
							if extractedTC := parseTimecapFromNotes(notes); extractedTC > 0 {
								section.TimeCap = extractedTC
							}
						}
					}

					// For EMOM (type 3), try to build a better name from notes
					// e.g., "EVERY 2'30" x 4 SETS" -> "E2:30MO2:30M"
					if section.Type == 3 && section.Title == "EMOM" && section.Notes != "" {
						if betterName := parseEMOMName(section.Notes, section.TimeCap); betterName != "" {
							section.Title = betterName
						}
					}

					// Time completed
					if t, ok := twMap["time"].(string); ok && t != "" && t != "0" {
						section.Time = t
						if item.Time == "" {
							item.Time = t
						}
					} else if t, ok := twMap["time"].(float64); ok && t > 0 {
						section.Time = strconv.FormatFloat(t, 'f', 0, 64)
						if item.Time == "" {
							item.Time = section.Time
						}
					}

					// Rounds (from rondas field - total rounds in workout)
					if r, ok := twMap["rondas"].(string); ok {
						section.Rounds, _ = strconv.Atoi(r)
					}

					// Rounds completed (from res field - actual rounds/sets done)
					if res, ok := twMap["res"].(string); ok {
						section.RoundsCompleted, _ = strconv.Atoi(res)
					}

					// Reps achieved (for AMRAP - extra reps beyond full rounds)
					if reps, ok := twMap["reps"].(string); ok {
						section.Reps, _ = strconv.Atoi(reps)
					}

					// RX
					if rx, ok := twMap["rx"].(string); ok {
						section.RX = rx == "1"
						item.RX = item.RX || section.RX
					} else if rx, ok := twMap["rx"].(float64); ok {
						section.RX = rx == 1
						item.RX = item.RX || section.RX
					}

					// Rank
					// if rank, ok := twMap["rank"].(string); ok {
					// 	section.Rank, _ = strconv.Atoi(rank)
					// }

					// If title is empty, try to extract name from notes or use workout type name
					if section.Title == "" {
						section.Title = parseSectionTitle(section.Notes, section.Type, section.TimeCap)
					}

					if section.Title != "" {
						item.Sections = append(item.Sections, section)
					}
				}
			}

			// Build workout name from sections
			if len(item.Sections) > 0 {
				var names []string
				for _, s := range item.Sections {
					if s.Title != "" {
						names = append(names, s.Title)
					}
				}
				if len(names) > 0 {
					item.Name = strings.Join(names, " + ")
				}
			}
		}

		// Parse ejerRate (exercises with details)
		if exercises, ok := act["ejerRate"].([]interface{}); ok {
			for _, ex := range exercises {
				if exMap, ok := ex.(map[string]interface{}); ok {
					exercise := ExerciseItem{}

					// Basic info
					if id, ok := exMap["ejerId"].(string); ok {
						exercise.ID = id
					}
					if name, ok := exMap["ejerName"].(string); ok {
						exercise.Name = name
					}
					if pic, ok := exMap["ejerPic"].(string); ok {
						exercise.ImageURL = pic
					}
					if vid, ok := exMap["ejerVideo"].(string); ok {
						exercise.VideoID = vid
					}

					// Section association (tipoWOD indicates which section this exercise belongs to)
					if tw, ok := exMap["tipoWOD"].(float64); ok {
						exercise.SectionIndex = int(tw)
					}

					// Rounds and reps
					if round, ok := exMap["round"].(string); ok {
						exercise.Round, _ = strconv.Atoi(round)
					}
					if roundReps, ok := exMap["roundrepeat"].(string); ok {
						exercise.RoundReps, _ = strconv.Atoi(roundReps)
					}

					// FormatType (formaReg): 2=distance, 3=count, 4=weight+reps
					if fmt, ok := exMap["formaReg"].(string); ok {
						exercise.FormatType, _ = strconv.Atoi(fmt)
					}

					// valor1 contains the primary value (reps, distance, etc.)
					if valor1, ok := exMap["valor1"].([]interface{}); ok && len(valor1) > 0 {
						if v, ok := valor1[0].(string); ok {
							val, _ := strconv.ParseFloat(v, 64)
							switch exercise.FormatType {
							case 2: // Distance
								exercise.Distance = val
								exercise.DistanceUnit = "m"
							case 3: // Count/Reps
								exercise.Reps = int(val)
							case 4: // Weight + Reps
								exercise.Reps = int(val)
							default:
								exercise.Reps = int(val)
							}
						}
					}

					// valor2 contains weight (for formaReg=4)
					if valor2, ok := exMap["valor2"].(string); ok && exercise.FormatType == 4 {
						exercise.Weight, _ = strconv.ParseFloat(valor2, 64)
						exercise.WeightUnit = "kg"
					}

					// Fallback to explicit reps field
					if exercise.Reps == 0 {
						if reps, ok := exMap["reps"].(string); ok {
							exercise.Reps, _ = strconv.Atoi(reps)
						} else if reps, ok := exMap["reps"].(float64); ok {
							exercise.Reps = int(reps)
						}
					}

					// Fallback weight fields
					if exercise.Weight == 0 {
						if weight, ok := exMap["weight"].(string); ok {
							exercise.Weight, _ = strconv.ParseFloat(weight, 64)
						} else if weight, ok := exMap["weight"].(float64); ok {
							exercise.Weight = weight
						}
					}
					if exercise.WeightUnit == "" {
						if unit, ok := exMap["unit"].(string); ok {
							exercise.WeightUnit = unit
						}
					}

					// Distance fallback
					if exercise.Distance == 0 {
						if dist, ok := exMap["distance"].(string); ok {
							exercise.Distance, _ = strconv.ParseFloat(dist, 64)
						} else if dist, ok := exMap["distance"].(float64); ok {
							exercise.Distance = dist
						}
					}
					if exercise.DistanceUnit == "" {
						if distUnit, ok := exMap["distanceUnit"].(string); ok {
							exercise.DistanceUnit = distUnit
						}
					}

					// Other fields
					if cals, ok := exMap["cals"].(float64); ok {
						exercise.Calories = int(cals)
					}
					if t, ok := exMap["time"].(string); ok {
						exercise.Time = t
					}
					if wodName, ok := exMap["wodName"].(string); ok && wodName != "" {
						exercise.WodName = wodName
						if item.Name == "" || item.Name == exercise.Name {
							item.Name = wodName
						}
					}
					if pr, ok := exMap["pr"].(float64); ok {
						exercise.PR = pr == 1
					}

					if exercise.Name != "" {
						item.Exercises = append(item.Exercises, exercise)
					}
				}
			}

			// If no name from sections, use exercise names
			if item.Name == "" && len(item.Exercises) > 0 {
				var names []string
				seen := make(map[string]bool)
				for _, ex := range item.Exercises {
					if ex.Name != "" && !seen[ex.Name] {
						names = append(names, ex.Name)
						seen[ex.Name] = true
					}
				}
				if len(names) > 3 {
					names = names[:3]
				}
				if len(names) > 0 {
					item.Name = strings.Join(names, ", ")
				}
			}
		}

		if item.ID != "" && item.Date != "" {
			items = append(items, item)
		}
	}

	return items, response.LastLoaded
}

// parseActivityDate parses various date formats from the activity API
func (c *Client) parseActivityDate(day string) string {
	// Try MM-DD-YYYY format first (e.g., "12-29-2025")
	if len(day) == 10 && day[2] == '-' && day[5] == '-' {
		parts := strings.Split(day, "-")
		if len(parts) == 3 {
			return fmt.Sprintf("%s-%s-%s", parts[2], parts[0], parts[1])
		}
	}
	// For formats like "Jan 8th", we'd need more complex parsing
	// For now, return empty and rely on "when" field
	return ""
}

// activityToWorkout converts an ActivityItem to a Workout
func (c *Client) activityToWorkout(activity ActivityItem, date time.Time) models.Workout {
	workout := models.Workout{
		ID:      activity.ID,
		Date:    date,
		Name:    activity.Name,
		BoxName: c.config.Aimharder.BoxName,
		BoxID:   c.config.Aimharder.BoxID,
		Type:    detectWorkoutTypeFromName(activity.Name),
		Result: &models.WorkoutResult{
			Score:  activity.Score,
			Scaled: !activity.RX,
		},
	}

	// Parse time result
	if activity.Time != "" {
		if dur, err := parseTimeString(activity.Time); err == nil {
			workout.Result.Time = &dur
		}
	}

	// Convert sections
	for _, section := range activity.Sections {
		ws := models.WorkoutSection{
			ID:              section.ID,
			Name:            section.Title,
			Type:            detectWorkoutTypeFromSection(section.Type, section.Title),
			TimeCap:         section.TimeCap,
			Time:            section.Time,
			Rounds:          section.Rounds,
			RoundsCompleted: section.RoundsCompleted,
			RepsAchieved:    section.Reps,
			RX:              section.RX,
			// Rank:            section.Rank,
			Notes: section.Notes,
		}
		workout.Sections = append(workout.Sections, ws)
	}

	// Convert exercises
	for _, ex := range activity.Exercises {
		exercise := models.Exercise{
			ID:           ex.ID,
			Name:         ex.Name,
			SectionIndex: ex.SectionIndex,
			Round:        ex.Round,
			RepsPerRound: ex.RoundReps,
			Reps:         ex.Reps,
			Weight:       ex.Weight,
			WeightUnit:   ex.WeightUnit,
			Distance:     ex.Distance,
			DistanceUnit: ex.DistanceUnit,
			Calories:     ex.Calories,
			Time:         ex.Time,
			ImageURL:     ex.ImageURL,
			VideoID:      ex.VideoID,
			PR:           ex.PR,
			FormatType:   ex.FormatType,
		}

		// Use roundrepeat as reps if reps is 0
		if exercise.Reps == 0 && ex.RoundReps > 0 {
			exercise.Reps = ex.RoundReps
		}

		workout.Exercises = append(workout.Exercises, exercise)
	}

	// Generate description
	workout.Description = workout.FormatDescription()

	return workout
}

func detectWorkoutTypeFromName(name string) models.WorkoutType {
	upper := strings.ToUpper(name)
	switch {
	case strings.Contains(upper, "AMRAP"):
		return models.WorkoutTypeAMRAP
	case strings.Contains(upper, "FOR TIME") || strings.Contains(upper, "FORTIME"):
		return models.WorkoutTypeForTime
	case strings.Contains(upper, "EMOM"):
		return models.WorkoutTypeEMOM
	case strings.Contains(upper, "TABATA"):
		return models.WorkoutTypeTabata
	case strings.Contains(upper, "STRENGTH") || strings.Contains(upper, "FUERZA"):
		return models.WorkoutTypeStrength
	case strings.Contains(upper, "SKILL") || strings.Contains(upper, "TECNICA"):
		return models.WorkoutTypeSkill
	default:
		return models.WorkoutTypeWOD
	}
}

// detectWorkoutTypeFromSection maps the API type number to WorkoutType
// API types: 1=ForTime, 2=AMRAP, 3=EMOM, 4=Tabata, 5=Strength, 6=Skill
func detectWorkoutTypeFromSection(apiType int, title string) models.WorkoutType {
	switch apiType {
	case 1:
		return models.WorkoutTypeForTime
	case 2:
		return models.WorkoutTypeAMRAP
	case 3:
		return models.WorkoutTypeEMOM
	case 4:
		return models.WorkoutTypeTabata
	case 5:
		return models.WorkoutTypeStrength
	case 6:
		return models.WorkoutTypeSkill
	default:
		// Fallback to name detection
		return detectWorkoutTypeFromName(title)
	}
}

// parseTimeString parses a time string like "12:34" or "1234" into a Duration
func parseTimeString(timeStr string) (time.Duration, error) {
	// Handle MM:SS format
	if strings.Contains(timeStr, ":") {
		parts := strings.Split(timeStr, ":")
		if len(parts) == 2 {
			mins, _ := strconv.Atoi(parts[0])
			secs, _ := strconv.Atoi(parts[1])
			return time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second, nil
		}
	}
	// Handle seconds-only
	if secs, err := strconv.Atoi(timeStr); err == nil {
		return time.Duration(secs) * time.Second, nil
	}
	return 0, fmt.Errorf("unable to parse time: %s", timeStr)
}

// parseEMOMName tries to build a proper EMOM name from the notes
// e.g., "EVERY 2'30" x 4 SETS" -> "E2MO2M" or "E2:30MO2:30M"
func parseEMOMName(notes string, timecap int) string {
	// Pattern: EVERY X'Y" or EVERY X' or EVERY X min
	// Examples: "EVERY 2'30"", "EVERY 2'", "EVERY 1 min", "EVERY 90""
	patterns := []string{
		`(?i)EVERY\s+(\d+)[''′](\d+)[""″]?`, // EVERY 2'30"
		`(?i)EVERY\s+(\d+)[''′]\s`,          // EVERY 2'
		`(?i)EVERY\s+(\d+)\s*min`,           // EVERY 2 min
		`(?i)E(\d+)MO(\d+)M`,                // Already formatted E2MO2M
	}

	// Try to extract interval from notes
	for i, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(notes)
		if len(matches) > 1 {
			switch i {
			case 0: // EVERY X'Y" format
				mins := matches[1]
				secs := matches[2]
				if secs == "30" {
					return fmt.Sprintf("E%sMO%sM", mins, mins) // E2MO2M style
				}
				return fmt.Sprintf("E%s:%sMO%s:%sM", mins, secs, mins, secs)
			case 1: // EVERY X' format
				mins := matches[1]
				return fmt.Sprintf("E%sMO%sM", mins, mins)
			case 2: // EVERY X min format
				mins := matches[1]
				return fmt.Sprintf("E%sMO%sM", mins, mins)
			case 3: // Already E2MO2M format
				return matches[0]
			}
		}
	}

	// Fallback: if timecap is set and it's a reasonable EMOM interval
	if timecap > 0 && timecap <= 5 {
		return fmt.Sprintf("E%dMO%dM", timecap, timecap)
	}

	return "" // Return empty to keep original title
}

// parseSectionTitle extracts a workout title from notes or generates one from type
func parseSectionTitle(notes string, sectionType int, timecap int) string {
	// First, try to extract a named workout from notes
	if notes != "" {
		// Clean up HTML
		cleanNotes := strings.ReplaceAll(notes, "<br />", "\n")
		cleanNotes = strings.ReplaceAll(notes, "<br>", "\n")
		cleanNotes = strings.TrimSpace(cleanNotes)

		// Look for common named workout patterns
		namedPatterns := []string{
			`(?i)(12\s+DAYS?\s+OF\s+CHRISTMAS)`,
			`(?i)(MURPH)`,
			`(?i)(FRAN)`,
			`(?i)(HELEN)`,
			`(?i)(GRACE)`,
			`(?i)(CINDY)`,
			`(?i)(ANNIE)`,
			`(?i)(DIANE)`,
			`(?i)(ELIZABETH)`,
			`(?i)(JACKIE)`,
			`(?i)(KAREN)`,
			`(?i)(MARY)`,
			`(?i)(ISABEL)`,
			`(?i)(NANCY)`,
		}

		for _, pattern := range namedPatterns {
			re := regexp.MustCompile(pattern)
			if matches := re.FindStringSubmatch(cleanNotes); len(matches) > 1 {
				return strings.ToUpper(matches[1])
			}
		}
	}

	// Map workout type to a readable name
	typeNames := map[int]string{
		1:  "Strength",
		2:  "AMRAP",
		3:  "EMOM",
		4:  "Tabata",
		5:  "For Time",
		6:  "Max Reps",
		7:  "Max Weight",
		8:  "Chipper",
		9:  "RFT", // Rounds For Time
		10: "Ladder",
		11: "For Time",
		12: "YGIG", // You Go I Go
	}

	if name, ok := typeNames[sectionType]; ok {
		if timecap > 0 && (sectionType == 2 || sectionType == 5 || sectionType == 11) {
			return fmt.Sprintf("%d' %s", timecap, name)
		}
		return name
	}

	// Last resort - use timecap if available
	if timecap > 0 {
		return fmt.Sprintf("%d min WOD", timecap)
	}

	return "WOD"
}

// parseTimecapFromNotes extracts time cap from workout notes
// Looks for patterns like "TC 40'", "Time Cap: 40 min", "40 min cap"
func parseTimecapFromNotes(notes string) int {
	patterns := []string{
		`(?i)TC\s*[:=]?\s*(\d+)[''′]?`,    // TC 40' or TC: 40
		`(?i)Time\s*Cap\s*[:=]?\s*(\d+)`,  // Time Cap: 40
		`(?i)(\d+)\s*min(?:utes?)?\s*cap`, // 40 min cap
		`(?i)cap\s*[:=]?\s*(\d+)\s*min`,   // cap: 40 min
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(notes); len(matches) > 1 {
			if tc, err := strconv.Atoi(matches[1]); err == nil && tc > 0 {
				return tc
			}
		}
	}

	return 0
}

// GetWorkoutResult fetches the athlete's result for a specific workout
func (c *Client) GetWorkoutResult(ctx context.Context, date time.Time, classID string) (*models.WorkoutResult, error) {
	if !c.loggedIn {
		return nil, fmt.Errorf("not logged in")
	}

	dateStr := date.Format("20060102")

	apiURL := fmt.Sprintf("%s/api/results?day=%s&classId=%s&box=%s&familyId=%s",
		c.boxURL, dateStr, classID, c.config.Aimharder.BoxID, c.familyID)

	resp, err := c.doAPIRequest(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return c.parseResultResponse(body)
	}

	return nil, nil
}

// Helper methods

func (c *Client) parseWODResponse(body []byte) (*models.WODInfo, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	wod := &models.WODInfo{}

	if id, ok := raw["id"].(string); ok {
		wod.ID = id
	} else if id, ok := raw["id"].(float64); ok {
		wod.ID = strconv.FormatFloat(id, 'f', 0, 64)
	}

	if name, ok := raw["name"].(string); ok {
		wod.Name = name
	} else if name, ok := raw["nombre"].(string); ok {
		wod.Name = name
	}

	if desc, ok := raw["description"].(string); ok {
		wod.Description = desc
	} else if desc, ok := raw["descripcion"].(string); ok {
		wod.Description = desc
	} else if desc, ok := raw["workout"].(string); ok {
		wod.Description = desc
	}

	if timeCap, ok := raw["timeCap"].(float64); ok {
		wod.TimeCap = int(timeCap)
	} else if timeCap, ok := raw["tiempo"].(float64); ok {
		wod.TimeCap = int(timeCap)
	}

	wod.Type = detectWorkoutType(wod.Name, wod.Description)

	return wod, nil
}

func (c *Client) parseResultResponse(body []byte) (*models.WorkoutResult, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		var results []map[string]interface{}
		if err := json.Unmarshal(body, &results); err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return nil, nil
		}
		raw = results[0]
	}

	result := &models.WorkoutResult{}

	if score, ok := raw["score"].(string); ok {
		result.Score = score
		parseScore(score, result)
	}

	if rounds, ok := raw["rounds"].(float64); ok {
		result.Rounds = int(rounds)
	}

	if reps, ok := raw["reps"].(float64); ok {
		result.Reps = int(reps)
	}

	if weight, ok := raw["weight"].(float64); ok {
		result.Weight = weight
	}

	if scaled, ok := raw["scaled"].(bool); ok {
		result.Scaled = scaled
	} else if rx, ok := raw["rx"].(bool); ok {
		result.Scaled = !rx
	}

	if notes, ok := raw["notes"].(string); ok {
		result.Notes = notes
	}

	return result, nil
}

func detectWorkoutType(name, description string) models.WorkoutType {
	combined := strings.ToUpper(name + " " + description)

	switch {
	case strings.Contains(combined, "AMRAP"):
		return models.WorkoutTypeAMRAP
	case strings.Contains(combined, "FOR TIME") || strings.Contains(combined, "FORTIME"):
		return models.WorkoutTypeForTime
	case strings.Contains(combined, "EMOM"):
		return models.WorkoutTypeEMOM
	case strings.Contains(combined, "TABATA"):
		return models.WorkoutTypeTabata
	case strings.Contains(combined, "STRENGTH") || strings.Contains(combined, "FUERZA"):
		return models.WorkoutTypeStrength
	case strings.Contains(combined, "SKILL") || strings.Contains(combined, "TECNICA"):
		return models.WorkoutTypeSkill
	case isHeroWOD(combined):
		return models.WorkoutTypeHero
	case isGirlWOD(combined):
		return models.WorkoutTypeGirl
	default:
		return models.WorkoutTypeWOD
	}
}

func isHeroWOD(name string) bool {
	heroWODs := []string{"MURPH", "DT", "MICHAEL", "RYAN", "RANDY", "JOSH", "CHAD", "TOMMY V", "NICK", "NATE", "JARED", "BADGER", "JASON", "WHITTEN", "JT"}
	for _, hero := range heroWODs {
		if strings.Contains(name, hero) {
			return true
		}
	}
	return false
}

func isGirlWOD(name string) bool {
	girlWODs := []string{"FRAN", "GRACE", "HELEN", "DIANE", "ELIZABETH", "ANNIE", "ISABEL", "KAREN", "NANCY", "CINDY", "JACKIE", "MARY", "EVA", "KELLY", "LINDA", "AMANDA"}
	for _, girl := range girlWODs {
		if strings.Contains(name, girl) {
			return true
		}
	}
	return false
}

func parseScore(score string, result *models.WorkoutResult) {
	score = strings.TrimSpace(score)

	timeRegex := regexp.MustCompile(`^(\d{1,2}):(\d{2})(?::(\d{2}))?$`)
	if matches := timeRegex.FindStringSubmatch(score); len(matches) > 0 {
		var duration time.Duration
		if matches[3] != "" {
			hours, _ := strconv.Atoi(matches[1])
			mins, _ := strconv.Atoi(matches[2])
			secs, _ := strconv.Atoi(matches[3])
			duration = time.Duration(hours)*time.Hour + time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second
		} else {
			mins, _ := strconv.Atoi(matches[1])
			secs, _ := strconv.Atoi(matches[2])
			duration = time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second
		}
		result.Time = &duration
		return
	}

	roundsRegex := regexp.MustCompile(`(\d+)\s*\+\s*(\d+)`)
	if matches := roundsRegex.FindStringSubmatch(score); len(matches) > 0 {
		result.Rounds, _ = strconv.Atoi(matches[1])
		result.Reps, _ = strconv.Atoi(matches[2])
		return
	}

	justRoundsRegex := regexp.MustCompile(`^(\d+)\s*(?:rounds?|rondas?)?$`)
	if matches := justRoundsRegex.FindStringSubmatch(strings.ToLower(score)); len(matches) > 0 {
		result.Rounds, _ = strconv.Atoi(matches[1])
		return
	}

	weightRegex := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(?:kg|lbs?)?$`)
	if matches := weightRegex.FindStringSubmatch(strings.ToLower(score)); len(matches) > 0 {
		result.Weight, _ = strconv.ParseFloat(matches[1], 64)
		return
	}
}
