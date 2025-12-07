package teachable

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ---------- API: /users and /users/{id} ----------

// Shape mirrors your Python usage:
// - get_users()  -> list with "users": [ ... ]
// - get_user_profile() -> one object with fields + "courses": [ ... ]

type UserCourseEnrollment struct {
	CourseID           int     `json:"course_id"`
	CourseName         string  `json:"course_name"`
	EnrolledAt         *string `json:"enrolled_at"`
	IsActiveEnrollment bool    `json:"is_active_enrollment"`
	CompletedAt        *string `json:"completed_at"`
	PercentComplete    int     `json:"percent_complete"`
}

type UserProfileResponse struct {
	ID           int                    `json:"id"`
	Email        string                 `json:"email"`
	Name         string                 `json:"name"`
	Role         string                 `json:"role"`
	Src          *string                `json:"src"`
	LastSignInIP *string                `json:"last_sign_in_ip"`
	Courses      []UserCourseEnrollment `json:"courses"`
}

// Optional bulk response if you ever port sync_users_json_to_sqlite 1:1.
type UsersListResponse struct {
	Users []struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		ID    int    `json:"id"`
	} `json:"users"`
}

// GET /users?per=10000
func (c *Client) GetUsers(ctx context.Context) (*UsersListResponse, error) {
	url := fmt.Sprintf("%s/users?per=10000", baseURL)
	var out UsersListResponse
	if err := c.doJSON(ctx, "GET", url, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /users/{user_id}
func (c *Client) GetUserProfile(ctx context.Context, userID int) (*UserProfileResponse, error) {
	url := fmt.Sprintf("%s/users/%d", baseURL, userID)
	var out UserProfileResponse
	if err := c.doJSON(ctx, "GET", url, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------- SQLite sync: sync_user_profile_and_courses_to_sqlite ----------

// SyncUserProfileAndCoursesToSQLite is the Go port of
// sync_user_profile_and_courses_to_sqlite from users.py.
func SyncUserProfileAndCoursesToSQLite(
	ctx context.Context,
	db *sql.DB,
	payload *UserProfileResponse,
	usersTable string,
	userCoursesTable string,
) (int64, error) {

	if usersTable == "" {
		usersTable = "users"
	}
	if userCoursesTable == "" {
		userCoursesTable = "user_courses"
	}

	if payload.ID == 0 {
		return 0, fmt.Errorf("payload.id is required")
	}

	// Normalize primary fields
	userID := payload.ID
	email := payload.Email
	if email != "" {
		email = normalizeEmail(email)
	}
	name := payload.Name
	role := payload.Role
	src := payload.Src
	lastSignInIP := payload.LastSignInIP

	// Build course rows
	type courseRow struct {
		UserID             int
		CourseID           int
		CourseName         string
		EnrolledAt         *string
		IsActiveEnrollment int
		CompletedAt        *string
		PercentComplete    int
	}

	var rows []courseRow
	for _, c := range payload.Courses {
		if c.CourseID == 0 {
			continue
		}
		enrolledAt := normalizeISOZ(c.EnrolledAt)
		completedAt := normalizeISOZ(c.CompletedAt)
		active := 0
		if c.IsActiveEnrollment {
			active = 1
		}
		rows = append(rows, courseRow{
			UserID:             userID,
			CourseID:           c.CourseID,
			CourseName:         c.CourseName,
			EnrolledAt:         enrolledAt,
			IsActiveEnrollment: active,
			CompletedAt:        completedAt,
			PercentComplete:    clampPercent(c.PercentComplete),
		})
	}

	usersDDL := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		id INTEGER PRIMARY KEY,
		email TEXT UNIQUE,
		name TEXT NOT NULL,
		role TEXT,
		src TEXT,
		last_sign_in_ip TEXT,
		created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP),
		updated_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP)
	);
	`, usersTable)

	userCoursesDDL := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %s (
		user_id INTEGER NOT NULL,
		course_id INTEGER NOT NULL,
		course_name TEXT NOT NULL,
		enrolled_at TEXT,
		is_active_enrollment INTEGER NOT NULL CHECK (is_active_enrollment IN (0,1)),
		completed_at TEXT,
		percent_complete INTEGER NOT NULL CHECK (percent_complete BETWEEN 0 AND 100),
		created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP),
		updated_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP),
		PRIMARY KEY (user_id, course_id),
		FOREIGN KEY (user_id) REFERENCES %s(id) ON DELETE CASCADE
	);
	`, userCoursesTable, usersTable)

	usersUpsert := fmt.Sprintf(`
	INSERT INTO %s (id, email, name, role, src, last_sign_in_ip)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		email = excluded.email,
		name = excluded.name,
		role = excluded.role,
		src = excluded.src,
		last_sign_in_ip = excluded.last_sign_in_ip,
		updated_at = CURRENT_TIMESTAMP;
	`, usersTable)

	userCoursesUpsert := fmt.Sprintf(`
	INSERT INTO %s (
		user_id, course_id, course_name, enrolled_at, is_active_enrollment, completed_at, percent_complete
	)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(user_id, course_id) DO UPDATE SET
		course_name = excluded.course_name,
		enrolled_at = excluded.enrolled_at,
		is_active_enrollment = excluded.is_active_enrollment,
		completed_at = excluded.completed_at,
		percent_complete = excluded.percent_complete,
		updated_at = CURRENT_TIMESTAMP;
	`, userCoursesTable)

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL;"); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, usersDDL); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, userCoursesDDL); err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}

	// upsert user
	if _, err := tx.ExecContext(
		ctx,
		usersUpsert,
		userID,
		nullIfEmpty(email),
		name,
		nullIfEmpty(role),
		src,
		lastSignInIP,
	); err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	// upsert user_courses
	if len(rows) > 0 {
		stmt, err := tx.PrepareContext(ctx, userCoursesUpsert)
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		defer stmt.Close()

		for _, r := range rows {
			if _, err := stmt.ExecContext(
				ctx,
				r.UserID,
				r.CourseID,
				r.CourseName,
				r.EnrolledAt,
				r.IsActiveEnrollment,
				r.CompletedAt,
				r.PercentComplete,
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

// normalizeEmail is a small helper similar to the Python `.strip().lower()`.
func normalizeEmail(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}
