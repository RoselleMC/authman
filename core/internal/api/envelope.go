package api

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Data  any        `json:"data"`
	Meta  any        `json:"meta"`
	Error *ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func NewError(status int, code, message string) *Error {
	return &Error{
		Status:  status,
		Code:    code,
		Message: message,
	}
}

func WriteJSON(w http.ResponseWriter, status int, data any, meta any) {
	write(w, status, Envelope{
		Data:  data,
		Meta:  meta,
		Error: nil,
	})
}

func WriteError(w http.ResponseWriter, err *Error) {
	if err == nil {
		err = NewError(http.StatusInternalServerError, "system.internal", "internal server error")
	}
	status := err.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	write(w, status, Envelope{
		Data: nil,
		Meta: nil,
		Error: &ErrorBody{
			Code:    err.Code,
			Message: err.Message,
			Details: err.Details,
		},
	})
}

func DecodeJSON(r *http.Request, dst any) *Error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return NewError(http.StatusBadRequest, "system.invalid_json", "invalid JSON request body")
	}
	return nil
}

func write(w http.ResponseWriter, status int, envelope Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope)
}
