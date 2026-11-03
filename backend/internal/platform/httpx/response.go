// Package httpx holds the HTTP helpers every module shares: response envelopes, request decoding,
// pagination, validation and request-scoped context. Modules must use these instead of redefining them.
package httpx

import (
	"encoding/json"
	"net/http"
	"strings"
)

// WriteJSON writes the standard {"data","meta"} envelope, or the error envelope for status >= 400.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status >= 400 {
		code := "REQUEST_FAILED"
		if fields, ok := value.(map[string]string); ok && fields["error"] != "" {
			code = strings.ToUpper(fields["error"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": errorMessage(code), "request_id": w.Header().Get("X-Request-ID"), "fields": map[string]string{}}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": value, "meta": map[string]string{"request_id": w.Header().Get("X-Request-ID")}})
}

// WriteError writes the standard error envelope with an explicit code and message.
func WriteError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	if fields == nil {
		fields = map[string]string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message, "request_id": w.Header().Get("X-Request-ID"), "fields": fields}})
}

// Invalid reports a 400 for one bad field.
func Invalid(w http.ResponseWriter, code, message, field string) {
	WriteError(w, http.StatusBadRequest, code, message, map[string]string{field: "invalid"})
}

// NotFoundProfile is the single 404 shape for a profile the caller does not own.
func NotFoundProfile(w http.ResponseWriter) {
	WriteError(w, http.StatusNotFound, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
}

func errorMessage(code string) string {
	messages := map[string]string{
		"PROFILE_NOT_FOUND":        "The requested profile could not be found.",
		"DOCUMENT_NOT_FOUND":       "The requested document could not be found.",
		"UNAUTHORIZED":             "A valid bearer session is required.",
		"INVALID_PAGINATION":       "Pagination parameters are invalid.",
		"INVALID_BIOMARKER_FIELDS": "Biomarker fields are invalid.",
	}
	if message := messages[code]; message != "" {
		return message
	}
	return "The request could not be completed."
}
