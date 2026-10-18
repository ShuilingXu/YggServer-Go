package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

type ErrorResponse struct {
	Error        string `json:"error"`
	ErrorMessage string `json:"errorMessage"`
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Error(w http.ResponseWriter, status int) {
	JSON(w, status, ErrorResponse{Error: http.StatusText(status), ErrorMessage: http.StatusText(status)})
}

func Decode(w http.ResponseWriter, r *http.Request, value any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		Error(w, http.StatusUnsupportedMediaType)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(value); err != nil {
		Error(w, http.StatusBadRequest)
		return false
	}
	if !errors.Is(d.Decode(new(any)), io.EOF) {
		Error(w, http.StatusBadRequest)
		return false
	}
	return true
}
