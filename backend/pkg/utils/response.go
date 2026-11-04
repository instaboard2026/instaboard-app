package utils

import (
	"encoding/json"
	"net/http"
)

// APIResponse is the standard API response structure.
type APIResponse struct {
	Status  string       `json:"status"` // "SUCCESS" | "ERROR"
	Data    interface{}  `json:"data,omitempty"`
	Message string       `json:"message,omitempty"`
	Errors  []FieldError `json:"errors,omitempty"`
}

// FieldError represents a field-level validation error.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteSuccess writes a successful API response.
func WriteSuccess(w http.ResponseWriter, data interface{}) {
	WriteJSON(w, http.StatusOK, APIResponse{Status: "SUCCESS", Data: data})
}

// WriteError writes an error API response.
func WriteError(w http.ResponseWriter, status int, message string, errors []FieldError) {
	WriteJSON(w, status, APIResponse{
		Status:  "ERROR",
		Message: message,
		Errors:  errors,
	})
}
