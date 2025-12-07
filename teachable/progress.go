package teachable

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ---------- API: GET /courses/{course_id}/progress?user_id={user_id}&per=1000 ----------

type ProgressLecture struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	IsCompleted bool    `json:"is_completed"`
	CompletedAt *string `json:"completed_at"`
}

type ProgressSection struct {
	Name     string            `json:"name"`
	Lectures []ProgressLecture `json:"lectures"`
}

type CourseProgress struct {
	ID              int               `json:"id"`
	PercentComplete int               `json:"percent_complete"`
	LectureSections []ProgressSection `json:"lecture_sections"`
}

type CourseProgressResponse struct {
	CourseProgress CourseProgress `json:"course_progress"`
}

func (c *Client) GetProgress(ctx context.Context, courseID, userID int) (*CourseProgressResponse, error) {
	url := fmt.Sprintf("%s/courses/%d/progress?user_id=%d&per=1000", baseURL, courseID, userID)
	var out CourseProgressResponse
	if err := c.doJSON(ctx, "GET", url, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- SQLite sync: sync_course_progress_to_sqlite ----------

func SyncCourseProgressToSQLite(ctx context.Context, db *sql.DB, userID int, payload *CourseProgressResponse, table string) (int64, error) {
	if table == "" {
		table = "course_progress_lectures"
	}
	cp := payload.CourseProgress
	if cp.ID == 0 {
		return 0, fmt.Errorf("course_progress.id is required")
	}

	// earliest finished_at across all lectures with completed_at
	var earliest *time.Time

	for _, sec := range cp.LectureSections {
		for _, lec := range sec.Lectures {
			if lec.CompletedAt == nil || *lec.CompletedAt == "" {
				continue
			}
			// tolerant parse – if it fails we just ignore that timestamp
			t, err := parseISOAny(*lec.CompletedAt)
			if err != nil {
				continue
			}
			if earliest == nil || t.Before(*earliest) {
				earliest = &t
			}
		}
	}

	var finishedAt *string
	if earliest != nil {
		s := earliest.UTC().Format(time.RFC3339)
		finishedAt = &s
	}

	ddl := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		user_id INTEGER NOT NULL,
		lecture_id INTEGER NOT NULL,
		percentage INTEGER NOT NULL,
		finished_at TEXT,
		section TEXT NOT NULL,
		lecture TEXT NOT NULL,
		is_complete INTEGER NOT NULL CHECK (is_complete IN (0,1)),
		completed_at TEXT,

		PRIMARY KEY (user_id, section, lecture_id)
	);
	`, table)

	upsert := fmt.Sprintf(`
	INSERT INTO %s (
		user_id, lecture_id, percentage, finished_at, section, lecture, is_complete, completed_at
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(user_id, section, lecture_id) DO UPDATE SET
		percentage = excluded.percentage,
		finished_at = excluded.finished_at,
		is_complete = excluded.is_complete,
		completed_at = excluded.completed_at;
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

	for _, sec := range cp.LectureSections {
		secName := sec.Name
		for _, lec := range sec.Lectures {
			lecName := lec.Name
			isComplete := 0
			if lec.IsCompleted {
				isComplete = 1
			}
			var completedAt *string
			if lec.CompletedAt != nil && *lec.CompletedAt != "" {
				n := normalizeISOZ(lec.CompletedAt)
				completedAt = n
			}
			if _, err := stmt.ExecContext(
				ctx,
				userID,
				lec.ID,
				clampPercent(cp.PercentComplete),
				finishedAt,
				secName,
				lecName,
				isComplete,
				completedAt,
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

// parseISOAny is a helper similar to Python _parse_iso_z.
func parseISOAny(s string) (time.Time, error) {
	// Very small helper: accept “...Z” or full RFC3339, and fall back to FromISO-like
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	// Fast path – standard RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// If ends with Z but not RFC3339-compliant, try trimming Z
	if s[len(s)-1] == 'Z' {
		if t, err := time.Parse("2006-01-02T15:04:05", s[:len(s)-1]); err == nil {
			return t.UTC(), nil
		}
	}
	// Fallback – you can add custom layouts here as needed
	return time.Parse(time.RFC3339, s)
}
