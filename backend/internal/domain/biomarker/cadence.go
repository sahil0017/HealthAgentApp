package biomarker

import (
	"sort"
	"time"
)

// Reading is one verified measurement of a biomarker.
type Reading struct {
	Value float64
	Unit  string
	On    time.Time
}

// MeasurementCadence is the person's own typical gap between measurements (median),
// learned from their history. It needs at least three distinct dates.
func MeasurementCadence(dates []time.Time) (time.Duration, bool) {
	days := map[string]time.Time{}
	for _, d := range dates {
		days[d.Format("2006-01-02")] = d
	}
	if len(days) < 3 {
		return 0, false
	}
	sorted := make([]time.Time, 0, len(days))
	for _, d := range days {
		sorted = append(sorted, d)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	gaps := make([]time.Duration, 0, len(sorted)-1)
	for i := 1; i < len(sorted); i++ {
		gaps = append(gaps, sorted[i].Sub(sorted[i-1]))
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	mid := len(gaps) / 2
	if len(gaps)%2 == 1 {
		return gaps[mid], true
	}
	return (gaps[mid-1] + gaps[mid]) / 2, true
}

// OverdueForMeasurement is true once the wait since the last reading is more than twice
// the person's usual gap.
func OverdueForMeasurement(last, now time.Time, cadence time.Duration) bool {
	return cadence > 0 && now.Sub(last) > 2*cadence
}

// AgeYears returns the whole years between a birth date and today.
func AgeYears(birth, today time.Time) int {
	years := today.Year() - birth.Year()
	if today.Month() < birth.Month() || (today.Month() == birth.Month() && today.Day() < birth.Day()) {
		years--
	}
	return years
}

type Series struct {
	Name     string
	Readings []Reading
}

func LastTwo(readings []Reading) (latest, previous *Reading) {
	if n := len(readings); n > 0 {
		latest = &readings[n-1]
		if n > 1 {
			previous = &readings[n-2]
		}
	}
	return
}
