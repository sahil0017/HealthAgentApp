// Package schedule turns the way a prescription writes a medicine's timing ("1-0-1", "BD", "after food",
// "x 5 days") into general daily slots. These are conventional readings of standard abbreviations, not
// clinical advice: the person can change every slot, and anything not recognised is reported as not
// understood instead of being guessed.
package schedule

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	Morning   = "morning"
	Afternoon = "afternoon"
	Evening   = "evening"
	Night     = "night"
)

// Slots lists every slot in the order of the day.
var Slots = []string{Morning, Afternoon, Evening, Night}

// DefaultTimes are only suggestions for display and reminders; the slot is what matters.
var DefaultTimes = map[string]string{Morning: "08:00", Afternoon: "14:00", Evening: "18:00", Night: "21:00"}

var Labels = map[string]string{Morning: "Morning", Afternoon: "Afternoon", Evening: "Evening", Night: "Night"}

const (
	FoodAny    = "any"
	FoodBefore = "before"
	FoodAfter  = "after"
	FoodWith   = "with"
	FoodEmpty  = "empty"
)

type Result struct {
	Slots      []string `json:"slots"`
	WithFood   string   `json:"with_food"`
	AsNeeded   bool     `json:"as_needed"`
	Understood bool     `json:"understood"`
	Summary    string   `json:"summary"`
}

var (
	dashed    = regexp.MustCompile(`\b([0-9½¼])\s*[-–—]\s*([0-9½¼])\s*[-–—]\s*([0-9½¼])(?:\s*[-–—]\s*([0-9½¼]))?\b`)
	everyHrs  = regexp.MustCompile(`(?:\bq\s*|every\s+)(\d{1,2})\s*(?:h|hr|hrs|hour|hours)\b`)
	onceDaily = regexp.MustCompile(`\b(od|qd|o\.d\.?|once (a |per )?day|once daily|daily|1\s*x\s*1|1 time (a )?day)\b`)
	twice     = regexp.MustCompile(`\b(bd|bid|b\.d\.?|twice (a |per )?day|twice daily|2\s*x\s*1|2 times (a )?day|two times)\b`)
	thrice    = regexp.MustCompile(`\b(tds|tid|t\.d\.s\.?|thrice|three times|3 times|3\s*x\s*1)\b`)
	fourTimes = regexp.MustCompile(`\b(qid|q\.i\.d\.?|four times|4 times|4\s*x\s*1)\b`)
	nightWord = regexp.MustCompile(`\b(hs|h\.s\.?|bedtime|at night|nocte|night|before sleep)\b`)
	morning   = regexp.MustCompile(`\b(morning|mane|breakfast|on waking)\b`)
	noon      = regexp.MustCompile(`\b(noon|afternoon|lunch|midday)\b`)
	evening   = regexp.MustCompile(`\b(evening|dinner|supper)\b`)
	asNeeded  = regexp.MustCompile(`\b(sos|prn|if needed|when needed|as needed|when required|if required|if pain|if fever)\b`)
	before    = regexp.MustCompile(`\b(before (food|meals?|breakfast|lunch|dinner)|ac|a\.c\.|ante cibum|before eating)\b`)
	after     = regexp.MustCompile(`\b(after (food|meals?|breakfast|lunch|dinner)|pc|p\.c\.|pp|post cibum|after eating)\b`)
	with      = regexp.MustCompile(`\b(with (food|meals?|milk)|during meals?)\b`)
	empty     = regexp.MustCompile(`\b(empty stomach|on empty|fasting|bf|before breakfast)\b`)
	nonWord   = regexp.MustCompile(`[^a-z0-9½¼.\-–— ]+`)
)

func clean(s string) string {
	return strings.Join(strings.Fields(nonWord.ReplaceAllString(strings.ToLower(s), " ")), " ")
}

// Parse reads the frequency and timing text of one medicine line.
func Parse(frequency, timing string) Result {
	text := clean(frequency + " " + timing)
	res := Result{Slots: []string{}, WithFood: FoodAny}
	switch {
	case empty.MatchString(text):
		res.WithFood = FoodEmpty
	case before.MatchString(text):
		res.WithFood = FoodBefore
	case with.MatchString(text):
		res.WithFood = FoodWith
	case after.MatchString(text):
		res.WithFood = FoodAfter
	}
	res.AsNeeded = asNeeded.MatchString(text)

	set := map[string]bool{}
	if m := dashed.FindStringSubmatch(text); m != nil {
		order := []string{Morning, Afternoon, Night}
		if m[4] != "" {
			order = Slots
		}
		for i, slot := range order {
			if m[i+1] != "0" {
				set[slot] = true
			}
		}
	} else {
		switch {
		case fourTimes.MatchString(text):
			set[Morning], set[Afternoon], set[Evening], set[Night] = true, true, true, true
		case thrice.MatchString(text):
			set[Morning], set[Afternoon], set[Night] = true, true, true
		case twice.MatchString(text):
			set[Morning], set[Night] = true, true
		default:
			if m := everyHrs.FindStringSubmatch(text); m != nil {
				h, _ := strconv.Atoi(m[1])
				switch {
				case h >= 5 && h <= 7:
					set[Morning], set[Afternoon], set[Evening], set[Night] = true, true, true, true
				case h >= 8 && h <= 9:
					set[Morning], set[Afternoon], set[Night] = true, true, true
				case h >= 10 && h <= 13:
					set[Morning], set[Night] = true, true
				case h >= 24:
					set[Morning] = true
				}
			}
		}
		// Named times of day stand in for, or refine, a once-a-day frequency.
		if len(set) == 0 {
			if nightWord.MatchString(text) {
				set[Night] = true
			}
			if morning.MatchString(text) {
				set[Morning] = true
			}
			if noon.MatchString(text) {
				set[Afternoon] = true
			}
			if evening.MatchString(text) {
				set[Evening] = true
			}
			if len(set) == 0 && onceDaily.MatchString(text) {
				set[Morning] = true
			}
		}
	}
	for _, s := range Slots {
		if set[s] {
			res.Slots = append(res.Slots, s)
		}
	}
	res.Understood = len(res.Slots) > 0 || res.AsNeeded
	res.Summary = Summarise(res.Slots, res.WithFood, res.AsNeeded)
	return res
}

// Summarise describes a schedule in plain words.
func Summarise(slots []string, food string, asNeeded bool) string {
	parts := []string{}
	switch {
	case asNeeded && len(slots) == 0:
		parts = append(parts, "Only when needed")
	case len(slots) == 0:
		return "Timing not understood. Choose when to take it."
	default:
		names := make([]string, len(slots))
		for i, s := range slots {
			names[i] = strings.ToLower(Labels[s])
		}
		parts = append(parts, strings.Join(names, ", "))
		if asNeeded {
			parts = append(parts, "or when needed")
		}
	}
	switch food {
	case FoodBefore:
		parts = append(parts, "before food")
	case FoodAfter:
		parts = append(parts, "after food")
	case FoodWith:
		parts = append(parts, "with food")
	case FoodEmpty:
		parts = append(parts, "on an empty stomach")
	}
	s := strings.Join(parts, ", ")
	return strings.ToUpper(s[:1]) + s[1:]
}

var durationRe = regexp.MustCompile(`(\d{1,3})\s*(day|days|d|week|weeks|wk|wks|w|month|months|mo|mos|m)\b`)

// DurationDays reads "5 days", "2 weeks" or "1 month". It returns 0 when the text has no usable duration,
// including open-ended wording such as "continue" or "lifelong".
func DurationDays(text string) int {
	m := durationRe.FindStringSubmatch(strings.ToLower(text))
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2][0] {
	case 'w':
		n *= 7
	case 'm':
		n *= 30
	}
	if n < 1 || n > 730 {
		return 0
	}
	return n
}
