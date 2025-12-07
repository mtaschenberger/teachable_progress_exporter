package store

import (
	"context"
	"database/sql"
	"fmt"
)

// CreateViews creates the should_have and final views as in db.create_tables.
func CreateViews(ctx context.Context, db *sql.DB) error {
	drops := []string{
		"DROP VIEW IF EXISTS final;",
		"DROP VIEW IF EXISTS should_have;",
		"DROP TABLE IF EXISTS should_have;", // compatibility with old runs
	}

	for _, stmt := range drops {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	viewSQL := `
CREATE VIEW should_have AS
SELECT en.user_id,
       cl.course_id,
       cl.course_name,
       li.lecture_id,
       li.name AS lecture_name,
       cl.section_name,
       cl.lecture_is_published
  FROM enrollments en
  JOIN course_lectures cl
    ON cl.course_id = en.course_id
  JOIN lecture_infos li
    ON li.lecture_id = cl.lecture_id;
`

	resultSQL := `
CREATE VIEW final AS
SELECT u.name,
       u.email,
       sh.course_name,
       sh.section_name,
       li.name,
       sh.lecture_is_published,
       COALESCE(is_complete, 0) AS Done
  FROM should_have sh
  LEFT JOIN course_progress_lectures cpl
    ON sh.lecture_id = cpl.lecture_id
   AND sh.user_id = cpl.user_id
  LEFT JOIN users u
    ON u.id = sh.user_id
  LEFT JOIN lecture_infos li
    ON li.lecture_id = sh.lecture_id;
`

	if _, err := db.ExecContext(ctx, viewSQL); err != nil {
		return fmt.Errorf("creating should_have view: %w", err)
	}
	if _, err := db.ExecContext(ctx, resultSQL); err != nil {
		return fmt.Errorf("creating final view: %w", err)
	}
	return nil
}

// FetchResults corresponds to db.fetch_results: SELECT * FROM final ORDER BY name ASC
func FetchResults(ctx context.Context, db *sql.DB) (*sql.Rows, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, email, course_name, section_name, name, lecture_is_published, Done
		  FROM final
		 ORDER BY name ASC;
	`)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
