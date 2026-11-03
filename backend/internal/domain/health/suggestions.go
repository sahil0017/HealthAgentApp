package health

import "strings"

type Suggestion struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Reason  string `json:"reason"`
	RefType string `json:"ref_type"`
	RefID   string `json:"ref_id"`
}

// WithoutExisting drops suggestions that match an open follow-up by title, so the same
// prompt is never offered twice.
func WithoutExisting(candidates []Suggestion, openTitles []string) []Suggestion {
	taken := map[string]bool{}
	for _, title := range openTitles {
		taken[strings.ToLower(strings.TrimSpace(title))] = true
	}
	out := []Suggestion{}
	for _, c := range candidates {
		key := strings.ToLower(strings.TrimSpace(c.Title))
		if taken[key] {
			continue
		}
		taken[key] = true
		out = append(out, c)
	}
	return out
}
