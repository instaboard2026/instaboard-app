package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrInvalidDate is returned when a calendar date is not a real YYYY-MM-DD day.
var ErrInvalidDate = errors.New("invalid date")

// CalendarSlot is one save placed on a calendar day.
type CalendarSlot struct {
	SaveID uint64
	Date   string
}

// CalendarNote is the note text for one calendar day.
type CalendarNote struct {
	Date string
	Body string
}

// CalendarRepo handles database access for calendar days and day notes.
type CalendarRepo struct {
	db *sql.DB
}

// NewCalendarRepo creates a new CalendarRepo.
func NewCalendarRepo(db *sql.DB) *CalendarRepo {
	return &CalendarRepo{db: db}
}

// List returns the user's scheduled saves and day notes.
func (r *CalendarRepo) List(ctx context.Context, userID int64) ([]CalendarSlot, []CalendarNote, error) {
	schedule, err := r.listSchedule(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	notes, err := r.listNotes(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return schedule, notes, nil
}

func (r *CalendarRepo) listSchedule(ctx context.Context, userID int64) ([]CalendarSlot, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT save_id, scheduled_on FROM tbl_calendar_schedule
		 WHERE user_id = ? ORDER BY scheduled_on, save_id`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CalendarSlot{}
	for rows.Next() {
		var slot CalendarSlot
		if err := rows.Scan(&slot.SaveID, &slot.Date); err != nil {
			return nil, err
		}
		out = append(out, slot)
	}
	return out, rows.Err()
}

func (r *CalendarRepo) listNotes(ctx context.Context, userID int64) ([]CalendarNote, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT note_date, body FROM tbl_calendar_notes
		 WHERE user_id = ? ORDER BY note_date`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CalendarNote{}
	for rows.Next() {
		var note CalendarNote
		if err := rows.Scan(&note.Date, &note.Body); err != nil {
			return nil, err
		}
		out = append(out, note)
	}
	return out, rows.Err()
}

// Schedule places the user's save on date (YYYY-MM-DD), replacing any earlier day.
func (r *CalendarRepo) Schedule(ctx context.Context, userID int64, saveID uint64, date string) error {
	if !validDate(date) {
		return ErrInvalidDate
	}
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tbl_saves WHERE id = ? AND user_id = ?`, saveID, userID,
	).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrSaveNotFound
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tbl_calendar_schedule (user_id, save_id, scheduled_on) VALUES (?, ?, ?)
		 ON CONFLICT (user_id, save_id) DO UPDATE SET
		   scheduled_on = excluded.scheduled_on,
		   updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')`,
		userID, saveID, date,
	)
	return err
}

// Unschedule removes a save from the calendar. Missing rows are left as-is.
func (r *CalendarRepo) Unschedule(ctx context.Context, userID int64, saveID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM tbl_calendar_schedule WHERE user_id = ? AND save_id = ?`,
		userID, saveID,
	)
	return err
}

// SetNote saves the day's note. A blank body removes the note.
func (r *CalendarRepo) SetNote(ctx context.Context, userID int64, date, body string) error {
	if !validDate(date) {
		return ErrInvalidDate
	}
	body = strings.TrimSpace(body)
	if body == "" {
		_, err := r.db.ExecContext(ctx,
			`DELETE FROM tbl_calendar_notes WHERE user_id = ? AND note_date = ?`,
			userID, date,
		)
		return err
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tbl_calendar_notes (user_id, note_date, body) VALUES (?, ?, ?)
		 ON CONFLICT (user_id, note_date) DO UPDATE SET
		   body = excluded.body,
		   updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')`,
		userID, date, body,
	)
	return err
}

func validDate(s string) bool {
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
}
