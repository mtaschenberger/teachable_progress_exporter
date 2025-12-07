package teachable

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ---------- API: GET /courses/{course_id}/lectures/{lecture_id} ----------

type LectureInfoResponse struct {
	Lecture struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		ContentType string `json:"content_type"`
		ContentURL  string `json:"content_url"`
		IsPublished bool   `json:"is_published"`
	} `json:"lecture"`
}

// GetLectureInfo corresponds to get_lecture_infos in Python.
func (c *Client) GetLectureInfo(ctx context.Context, courseID, lectureID int) (*LectureInfoResponse, error) {
	url := fmt.Sprintf("%s/courses/%d/lectures/%d", baseURL, courseID, lectureID)
	var out LectureInfoResponse
	if err := c.doJSON(ctx, "GET", url, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- SQLite sync: sync_lecture_info_to_sqlite ----------

// SyncLectureInfoToSQLite is the Go port of sync_lecture_info_to_sqlite.
func SyncLectureInfoToSQLite(ctx context.Context, db *sql.DB, payload *LectureInfoResponse, table string) (int64, error) {
	if table == "" {
		table = "lecture_infos"
	}

	l := payload.Lecture
	if l.ID == 0 {
		return 0, fmt.Errorf("lecture.id is required")
	}
	name := l.Name
	isPublished := 0
	if l.IsPublished {
		isPublished = 1
	}

	ddl := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		lecture_id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		is_published INTEGER NOT NULL CHECK (is_published IN (0,1))
	);
	`, table)

	upsert := fmt.Sprintf(`
	INSERT INTO %s (lecture_id, name, is_published)
	VALUES (?, ?, ?)
	ON CONFLICT(lecture_id) DO UPDATE SET
		name = excluded.name,
		is_published = excluded.is_published;
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

	if _, err := tx.ExecContext(ctx, upsert, l.ID, name, isPublished); err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return dbChanges(ctx, db)
}

// optional: convenience for bulk lecture infos if you ever need it later
func ExtractLectureIDsFromCourse(payload *CourseLayoutResponse) []int {
	var ids []int
	for _, sec := range payload.Course.LectureSections {
		for _, lec := range sec.Lectures {
			ids = append(ids, lec.ID)
		}
	}
	return ids
}

func (r *LectureInfoResponse) String() string {
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}
