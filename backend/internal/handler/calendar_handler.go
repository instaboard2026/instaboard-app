package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"instaboard/internal/repository"
	"instaboard/pkg/utils"
)

// CalendarHandler serves the user's calendar schedule and day notes (authenticated).
type CalendarHandler struct {
	repo *repository.CalendarRepo
}

// NewCalendarHandler creates a new CalendarHandler.
func NewCalendarHandler(repo *repository.CalendarRepo) *CalendarHandler {
	return &CalendarHandler{repo: repo}
}

type calendarResponse struct {
	Schedule []calendarSlotJSON `json:"schedule"`
	Notes    []calendarNoteJSON `json:"notes"`
}

type calendarSlotJSON struct {
	SaveID uint64 `json:"save_id"`
	Date   string `json:"date"`
}

type calendarNoteJSON struct {
	Date string `json:"date"`
	Body string `json:"body"`
}

type scheduleRequest struct {
	Date string `json:"date"`
}

type noteRequest struct {
	Body string `json:"body"`
}

// Get returns every scheduled save and day note for the signed-in user.
func (h *CalendarHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := userAndID(w, r, "")
	if !ok {
		return
	}
	schedule, notes, err := h.repo.List(r.Context(), userID)
	if err != nil {
		slog.Error("list calendar failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, "could not load calendar", nil)
		return
	}
	resp := calendarResponse{
		Schedule: make([]calendarSlotJSON, 0, len(schedule)),
		Notes:    make([]calendarNoteJSON, 0, len(notes)),
	}
	for _, slot := range schedule {
		resp.Schedule = append(resp.Schedule, calendarSlotJSON{SaveID: slot.SaveID, Date: slot.Date})
	}
	for _, note := range notes {
		resp.Notes = append(resp.Notes, calendarNoteJSON{Date: note.Date, Body: note.Body})
	}
	utils.WriteSuccess(w, resp)
}

// Schedule places one of the user's saves on a day.
func (h *CalendarHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	userID, saveID, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	var req scheduleRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.repo.Schedule(r.Context(), userID, uint64(saveID), req.Date); err != nil {
		writeCalendarError(w, err, "schedule save", userID)
		return
	}
	utils.WriteSuccess(w, calendarSlotJSON{SaveID: uint64(saveID), Date: req.Date})
}

// Unschedule removes a save from the calendar.
func (h *CalendarHandler) Unschedule(w http.ResponseWriter, r *http.Request) {
	userID, saveID, ok := userAndID(w, r, "id")
	if !ok {
		return
	}
	if err := h.repo.Unschedule(r.Context(), userID, uint64(saveID)); err != nil {
		writeCalendarError(w, err, "unschedule save", userID)
		return
	}
	utils.WriteSuccess(w, calendarSlotJSON{SaveID: uint64(saveID)})
}

// SetNote saves or clears the note for a day. An empty body deletes it.
func (h *CalendarHandler) SetNote(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := userAndID(w, r, "")
	if !ok {
		return
	}
	date := chi.URLParam(r, "date")
	var req noteRequest
	if !decode(w, r, &req) {
		return
	}
	if err := h.repo.SetNote(r.Context(), userID, date, req.Body); err != nil {
		writeCalendarError(w, err, "save calendar note", userID)
		return
	}
	utils.WriteSuccess(w, calendarNoteJSON{Date: date, Body: req.Body})
}

func writeCalendarError(w http.ResponseWriter, err error, action string, userID int64) {
	switch {
	case errors.Is(err, repository.ErrSaveNotFound):
		utils.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, repository.ErrInvalidDate):
		utils.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		slog.Error(action+" failed", "user_id", userID, "err", err)
		utils.WriteError(w, http.StatusInternalServerError, action+" failed", nil)
	}
}
