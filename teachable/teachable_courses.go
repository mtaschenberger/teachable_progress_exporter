package teachable

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// -------------------------
// HTTP client + types
// -------------------------

const baseURL = "https://developers.teachable.com/v1"

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) newRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("accept", "application/json")
	req.Header.Set("apiKey", c.apiKey)
	return req, nil
}

func (c *Client) doJSON(ctx context.Context, method, url string, v any) error {
	req, err := c.newRequest(ctx, method, url)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("teachable API error: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(v)
}

// -------------------------
// Course layout & enrollments – Go equivalents of courses.py
// -------------------------

// minimal struct for /courses/{id}
type CourseLayoutResponse struct {
	Course struct {
		ID              int    `json:"id"`
		Name            string `json:"name"`
		ImageURL        string `json:"image_url"`
		LectureSections []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Position    int    `json:"position"`
			IsPublished bool   `json:"is_published"`
			Lectures    []struct {
				ID          int  `json:"id"`
				Position    int  `json:"position"`
				IsPublished bool `json:"is_published"`
			} `json:"lectures"`
		} `json:"lecture_sections"`
	} `json:"course"`
}

// GET /courses/{course_id}
func (c *Client) GetCourseLayout(ctx context.Context, courseID int) (*CourseLayoutResponse, error) {
	url := fmt.Sprintf("%s/courses/%d", baseURL, courseID)
	var out CourseLayoutResponse
	if err := c.doJSON(ctx, http.MethodGet, url, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// -------------------------
// Enrollments
// -------------------------

type Enrollment struct {
	UserID          int     `json:"user_id"`
	EnrolledAt      *string `json:"enrolled_at"`
	CompletedAt     *string `json:"completed_at"`
	PercentComplete int     `json:"percent_complete"`
	ExpiresAt       *string `json:"expires_at"`
}

type CourseEnrollmentsResponse struct {
	Enrollments []Enrollment    `json:"enrollments"`
	Meta        map[string]any  `json:"meta"` // optional, we don't use it now
	RawMeta     json.RawMessage `json:"-"`
}

// GET /courses/{course_id}/enrollments?per=10000
func (c *Client) GetCourseEnrollments(ctx context.Context, courseID int) (*CourseEnrollmentsResponse, error) {
	url := fmt.Sprintf("%s/courses/%d/enrollments?per=10000", baseURL, courseID)
	var out CourseEnrollmentsResponse
	if err := c.doJSON(ctx, http.MethodGet, url, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// -------------------------
// SQLite helpers – Go equivalents of sync_course_structure_to_sqlite & sync_enrollments_to_sqlite
// -------------------------

// SyncCourseStructureToSQLite is the Go port of sync_course_structure_to_sqlite.
func SyncCourseStructureToSQLite(ctx context.Context, db *sql.DB, payload *CourseLayoutResponse, table string) (int64, error) {
	if table == "" {
		table = "course_lectures"
	}
	c := payload.Course
	if c.ID == 0 {
		return 0, fmt.Errorf("course.id is required")
	}

	ddl := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		course_id INTEGER NOT NULL,
		course_name TEXT NOT NULL,
		course_image_url TEXT,

		section_id INTEGER NOT NULL,
		section_name TEXT NOT NULL,
		section_position INTEGER NOT NULL,
		section_is_published INTEGER NOT NULL CHECK (section_is_published IN (0,1)),

		lecture_id INTEGER NOT NULL,
		lecture_position INTEGER NOT NULL,
		lecture_is_published INTEGER NOT NULL CHECK (lecture_is_published IN (0,1)),

		created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP),
		updated_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP),

		PRIMARY KEY (course_id, section_id, lecture_id)
	);
	`, table)

	upsert := fmt.Sprintf(`
	INSERT INTO %s (
		course_id, course_name, course_image_url,
		section_id, section_name, section_position, section_is_published,
		lecture_id, lecture_position, lecture_is_published
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(course_id, section_id, lecture_id) DO UPDATE SET
		course_name = excluded.course_name,
		course_image_url = excluded.course_image_url,
		section_name = excluded.section_name,
		section_position = excluded.section_position,
		section_is_published = excluded.section_is_published,
		lecture_position = excluded.lecture_position,
		lecture_is_published = excluded.lecture_is_published,
		updated_at = CURRENT_TIMESTAMP;
	`, table)

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL;"); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, upsert)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()

	for _, sec := range c.LectureSections {
		sectionIsPublished := 0
		if sec.IsPublished {
			sectionIsPublished = 1
		}
		for _, lec := range sec.Lectures {
			lectureIsPublished := 0
			if lec.IsPublished {
				lectureIsPublished = 1
			}
			if _, err := stmt.ExecContext(
				ctx,
				c.ID,
				c.Name,
				nullIfEmpty(c.ImageURL),
				sec.ID,
				sec.Name,
				sec.Position,
				sectionIsPublished,
				lec.ID,
				lec.Position,
				lectureIsPublished,
			); err != nil {
				_ = tx.Rollback()
				return 0, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return dbChanges(ctx, db)
}

// SyncEnrollmentsToSQLite is the Go port of sync_enrollments_to_sqlite.
func SyncEnrollmentsToSQLite(ctx context.Context, db *sql.DB, courseID int, payload *CourseEnrollmentsResponse, table string) (int64, error) {
	if table == "" {
		table = "enrollments"
	}
	if courseID == 0 {
		return 0, fmt.Errorf("course_id is required")
	}

	enrollments := payload.Enrollments
	if len(enrollments) == 0 {
		return 0, nil
	}

	ddl := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		course_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,

		enrolled_at TEXT,
		completed_at TEXT,
		percent_complete INTEGER NOT NULL CHECK (percent_complete BETWEEN 0 AND 100),
		expires_at TEXT,

		created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP),
		updated_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP),

		PRIMARY KEY (course_id, user_id)
	);
	`, table)

	upsert := fmt.Sprintf(`
	INSERT INTO %s (
		course_id, user_id, enrolled_at, completed_at, percent_complete, expires_at
	)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(course_id, user_id) DO UPDATE SET
		enrolled_at = excluded.enrolled_at,
		completed_at = excluded.completed_at,
		percent_complete = excluded.percent_complete,
		expires_at = excluded.expires_at,
		updated_at = CURRENT_TIMESTAMP;
	`, table)

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL;"); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, upsert)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()

	for _, e := range enrollments {
		enrolledAt := normalizeISOZ(e.EnrolledAt)
		completedAt := normalizeISOZ(e.CompletedAt)
		expiresAt := normalizeISOZ(e.ExpiresAt)

		if _, err := stmt.ExecContext(
			ctx,
			courseID,
			e.UserID,
			enrolledAt,
			completedAt,
			clampPercent(e.PercentComplete),
			expiresAt,
		); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return dbChanges(ctx, db)
}

// -------------------------
// Small helpers
// -------------------------

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// normalizeISOZ is the Go equivalent of _parse_iso_z from Python.
// It accepts *string (may be nil) and returns normalized *string (or nil).
func normalizeISOZ(in *string) *string {
	if in == nil {
		return nil
	}
	s := string(*in)
	if s == "" {
		return nil
	}
	// We keep this intentionally simple: just return the trimmed string.
	// If you want strict ISO normalization, we can extend this to full parsing.
	res := s
	return &res
}

// dbChanges returns the total number of changes on this connection.
func dbChanges(ctx context.Context, db *sql.DB) (int64, error) {
	var changes int64
	row := db.QueryRowContext(ctx, "SELECT total_changes()")
	if err := row.Scan(&changes); err != nil {
		return 0, err
	}
	return changes, nil
}

func clampPercent(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
