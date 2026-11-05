package biomarker

import "strings"

// CodeFor derives a stable catalog code from a test name so the same test reported by different labs
// lands on one trend. It returns "" when the name has no usable letters or digits.
func CodeFor(name string) string {
	var b strings.Builder
	underscore := false
	for _, r := range strings.ToUpper(strings.TrimSpace(name)) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			if underscore && b.Len() > 0 {
				b.WriteByte('_')
			}
			underscore = false
			b.WriteRune(r)
		default:
			underscore = true
		}
	}
	code := b.String()
	if len(code) < 2 {
		return ""
	}
	if len(code) > 48 {
		code = strings.TrimRight(code[:48], "_")
	}
	return code
}
