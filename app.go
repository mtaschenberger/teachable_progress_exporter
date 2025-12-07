package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	r "runtime"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"course-exporter/store"
	"course-exporter/teachable"

	_ "github.com/mattn/go-sqlite3"
)

type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called at application start. The context is saved so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// GetDefaultPath returns a platform-specific "Desktop" folder as default path.
func (a *App) GetDefaultPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	var desktop string
	switch r.GOOS {
	case "windows":
		desktop = filepath.Join(homeDir, "Desktop")
	case "darwin", "linux":
		desktop = filepath.Join(homeDir, "Desktop")
	default:
		desktop = homeDir
	}

	return desktop, nil
}

// Helper: send progress event to frontend
func (a *App) logProgress(step, message string) {
	if a.ctx == nil {
		return
	}
	payload := map[string]string{
		"step":    step,
		"message": message,
	}
	runtime.EventsEmit(a.ctx, "export:progress", payload)
}

// RunExport is called from the frontend.
// It will:
//  1. Validate the inputs
//  2. Simulate multiple API calls
//  3. Generate a CSV and save it to the given path

func (a *App) RunExport(courseIDStr string, apiToken string, outputDir string) (string, error) {
	if courseIDStr == "" {
		return "", fmt.Errorf("course_id must not be empty")
	}
	if apiToken == "" {
		return "", fmt.Errorf("API token must not be empty")
	}
	if outputDir == "" {
		return "", fmt.Errorf("output path must not be empty")
	}

	courseID, err := strconv.Atoi(courseIDStr)
	if err != nil {
		return "", fmt.Errorf("invalid course_id: %w", err)
	}
	a.logProgress("validate", fmt.Sprintf("Parameters validated (course_id=%s)", courseIDStr))
	// 1) in-memory SQLite
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return "", err
	}
	defer db.Close()

	ctx := a.ctx
	client := teachable.NewClient(apiToken)

	// 2) Course layout → course_lectures
	a.logProgress("course", "Fetching course layout…")
	layout, err := client.GetCourseLayout(ctx, courseID)
	if err != nil {
		return "", fmt.Errorf("get course layout: %w", err)
	}
	if _, err := teachable.SyncCourseStructureToSQLite(ctx, db, layout, "course_lectures"); err != nil {
		return "", fmt.Errorf("sync course structure: %w", err)
	}

	// 3) Lecture infos for each lecture_id
	a.logProgress("course", "Extract Lecture Information")
	lectureIDs := teachable.ExtractLectureIDsFromCourse(layout)
	for _, lecID := range lectureIDs {
		info, err := client.GetLectureInfo(ctx, courseID, lecID)
		if err != nil {
			return "", fmt.Errorf("get lecture info %d: %w", lecID, err)
		}
		if _, err := teachable.SyncLectureInfoToSQLite(ctx, db, info, "lecture_infos"); err != nil {
			return "", fmt.Errorf("sync lecture info %d: %w", lecID, err)
		}
	}
	a.logProgress("lectures", "All lecture details synced.")
	// 4) Enrollments

	a.logProgress("enrollment", "Check enrollments.")
	enrollmentsResp, err := client.GetCourseEnrollments(ctx, courseID)
	if err != nil {
		return "", fmt.Errorf("get enrollments: %w", err)
	}
	if _, err := teachable.SyncEnrollmentsToSQLite(ctx, db, courseID, enrollmentsResp, "enrollments"); err != nil {
		return "", fmt.Errorf("sync enrollments: %w", err)
	}

	// 5) For each enrolled user: progress + user profile
	userIDs := make(map[int]struct{})
	for _, e := range enrollmentsResp.Enrollments {
		userIDs[e.UserID] = struct{}{}
	}
	a.logProgress("enrollment", fmt.Sprintf("Found %d users - Continue to get their progress", len(userIDs)))

	for uid := range userIDs {
		// 5a: progress
		prog, err := client.GetProgress(ctx, courseID, uid)
		if err != nil {
			return "", fmt.Errorf("get progress user %d: %w", uid, err)
		}
		if _, err := teachable.SyncCourseProgressToSQLite(ctx, db, uid, prog, "course_progress_lectures"); err != nil {
			return "", fmt.Errorf("sync progress user %d: %w", uid, err)
		}

		// 5b: user profile + courses → users + user_courses
		profile, err := client.GetUserProfile(ctx, uid)
		if err != nil {
			return "", fmt.Errorf("get user profile %d: %w", uid, err)
		}
		if _, err := teachable.SyncUserProfileAndCoursesToSQLite(ctx, db, profile, "users", "user_courses"); err != nil {
			return "", fmt.Errorf("sync user profile %d: %w", uid, err)
		}
	}
	a.logProgress("export", "Start exporting to CSV…")
	// 6) Views + export
	if err := store.CreateViews(ctx, db); err != nil {
		return "", fmt.Errorf("create views: %w", err)
	}

	ts := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("course_%d_export_%s.csv", courseID, ts)
	fullPath := filepath.Join(outputDir, filename)

	if err := store.ExportFinalToCSV(ctx, db, fullPath); err != nil {
		return "", fmt.Errorf("export CSV: %w", err)
	}

	return fullPath, nil
}
