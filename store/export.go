package store

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
)

// ExportFinalToCSV runs SELECT from view `final` and writes to a CSV at path.
func ExportFinalToCSV(ctx context.Context, db *sql.DB, path string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT
			u.name,
			u.email,
			sh.course_name,
			sh.section_name,
			li.name,
			sh.lecture_is_published,
			COALESCE(cpl.is_complete, 0) AS Done
		FROM should_have sh
		LEFT JOIN course_progress_lectures cpl
			ON sh.lecture_id = cpl.lecture_id
			AND sh.user_id = cpl.user_id
		LEFT JOIN users u
			ON u.id = sh.user_id
		LEFT JOIN lecture_infos li
			ON li.lecture_id = sh.lecture_id
		ORDER BY u.name ASC;
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	defer w.Flush()

	// header
	header := []string{
		"name",
		"email",
		"course_name",
		"section_name",
		"lecture_name",
		"lecture_is_published",
		"Done",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for rows.Next() {
		var (
			name, email, courseName, sectionName, lectureName string
			lecturePublished                                  sql.NullInt64
			done                                              sql.NullInt64
		)
		if err := rows.Scan(
			&name,
			&email,
			&courseName,
			&sectionName,
			&lectureName,
			&lecturePublished,
			&done,
		); err != nil {
			return err
		}

		pub := "0"
		if lecturePublished.Valid && lecturePublished.Int64 == 1 {
			pub = "1"
		}
		doneStr := "0"
		if done.Valid && done.Int64 == 1 {
			doneStr = "1"
		}

		record := []string{
			name,
			email,
			courseName,
			sectionName,
			lectureName,
			pub,
			doneStr,
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("row iteration error: %w", err)
	}
	return w.Error()
}
