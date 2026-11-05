package schedule

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		freq, timing string
		slots        []string
		food         string
		asNeeded     bool
		understood   bool
	}{
		{"1-0-1", "after food", []string{Morning, Night}, FoodAfter, false, true},
		{"1-1-1", "", []string{Morning, Afternoon, Night}, FoodAny, false, true},
		{"1-1-1-1", "", Slots, FoodAny, false, true},
		{"0-0-1", "", []string{Night}, FoodAny, false, true},
		{"BD", "pc", []string{Morning, Night}, FoodAfter, false, true},
		{"TDS", "before meals", []string{Morning, Afternoon, Night}, FoodBefore, false, true},
		{"QID", "", Slots, FoodAny, false, true},
		{"OD", "empty stomach", []string{Morning}, FoodEmpty, false, true},
		{"HS", "", []string{Night}, FoodAny, false, true},
		{"once daily", "at night", []string{Night}, FoodAny, false, true},
		{"twice a day", "with food", []string{Morning, Night}, FoodWith, false, true},
		{"q8h", "", []string{Morning, Afternoon, Night}, FoodAny, false, true},
		{"every 12 hours", "", []string{Morning, Night}, FoodAny, false, true},
		{"SOS", "if pain", []string{}, FoodAny, true, true},
		{"morning and evening", "", []string{Morning, Evening}, FoodAny, false, true},
		{"", "", []string{}, FoodAny, false, false},
		{"as directed", "", []string{}, FoodAny, false, false},
	}
	for _, c := range cases {
		got := Parse(c.freq, c.timing)
		if !reflect.DeepEqual(got.Slots, c.slots) || got.WithFood != c.food || got.AsNeeded != c.asNeeded || got.Understood != c.understood {
			t.Errorf("Parse(%q,%q) = %+v, want slots %v food %s asNeeded %v understood %v", c.freq, c.timing, got, c.slots, c.food, c.asNeeded, c.understood)
		}
	}
}

func TestSummaryIsPlain(t *testing.T) {
	if got := Parse("1-0-1", "after food").Summary; got != "Morning, night, after food" {
		t.Fatalf("summary = %q", got)
	}
	if got := Parse("xyz", "").Summary; got != "Timing not understood. Choose when to take it." {
		t.Fatalf("summary = %q", got)
	}
}

func TestDurationDays(t *testing.T) {
	for in, want := range map[string]int{"5 days": 5, "x 2 weeks": 14, "1 month": 30, "3 mo": 90, "continue": 0, "": 0, "9999 days": 0} {
		if got := DurationDays(in); got != want {
			t.Errorf("DurationDays(%q) = %d, want %d", in, got, want)
		}
	}
}
