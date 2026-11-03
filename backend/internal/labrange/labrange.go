// Package labrange reads the reference range printed beside a lab result.
package labrange

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	numberPattern  = `([0-9]+(?:\.[0-9]+)?)`
	rangeBetween   = regexp.MustCompile(`^` + numberPattern + `\s*(?:-|–|—|to)\s*` + numberPattern)
	rangeUpperOnly = regexp.MustCompile(`^(?:<=|≤|<|up to|below|under)\s*` + numberPattern)
	rangeLowerOnly = regexp.MustCompile(`^(?:>=|≥|>|above|over|at least)\s*` + numberPattern)
)

// Parse reads the range printed on a lab report, such as "70-99", "< 5.7" or
// "3.5 to 5.0 mg/dL". Anything it cannot read confidently returns ok=false rather than a guess.
func Parse(text string) (low, high *float64, ok bool) {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.NewReplacer(",", "", "(", "", ")", "").Replace(t)
	if m := rangeBetween.FindStringSubmatch(t); m != nil {
		l, _ := strconv.ParseFloat(m[1], 64)
		h, _ := strconv.ParseFloat(m[2], 64)
		if l <= h {
			return &l, &h, true
		}
		return nil, nil, false
	}
	if m := rangeUpperOnly.FindStringSubmatch(t); m != nil {
		h, _ := strconv.ParseFloat(m[1], 64)
		return nil, &h, true
	}
	if m := rangeLowerOnly.FindStringSubmatch(t); m != nil {
		l, _ := strconv.ParseFloat(m[1], 64)
		return &l, nil, true
	}
	return nil, nil, false
}
