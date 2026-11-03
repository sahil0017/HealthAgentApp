package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var ErrPagination = errors.New("invalid pagination")

// DecodeJSON reads exactly one JSON object, rejecting unknown fields, other content types and oversized bodies.
func DecodeJSON(r *http.Request, maxBytes int64, destination any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

// Page parses limit (1-100, default 25) and offset from the query string.
func Page(r *http.Request) (int, int, error) {
	limit, offset := 25, 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return 0, 0, ErrPagination
		}
		limit = value
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 1000000 {
			return 0, 0, ErrPagination
		}
		offset = value
	}
	return limit, offset, nil
}

// OptionalQuery returns a non-empty query value, or nil when absent.
func OptionalQuery(r *http.Request, key string) *string {
	if value := r.URL.Query().Get(key); value != "" {
		return &value
	}
	return nil
}

// Getenv returns an environment variable or the fallback when it is unset or empty.
func Getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// ValidText checks a length in characters, and optionally that it is not empty.
func ValidText(value string, max int, required bool) bool {
	length := len([]rune(value))
	return length <= max && (!required || length > 0)
}

// ParseDay normalises an optional YYYY-MM-DD string; blank means "not set".
func ParseDay(value *string) (*string, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*value))
	if err != nil {
		return nil, err
	}
	day := parsed.Format("2006-01-02")
	return &day, nil
}

// Contains reports whether values holds value.
func Contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// Plural renders "1 report is" / "3 reports are" style text.
func Plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
