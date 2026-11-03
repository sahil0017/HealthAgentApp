package device

import (
	"math"
	"time"
)

// Day is one calendar day of readings for one metric.
type Day struct {
	Date  string  `json:"date"`
	Mean  float64 `json:"mean"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Sum   float64 `json:"sum"`
	Count int     `json:"count"`
}

// Value is what represents the day: the total for cumulative metrics (steps), otherwise the mean.
func (d Day) Value(m *Metric) float64 {
	if m.Sum {
		return d.Sum
	}
	return d.Mean
}

// Summary describes one metric over recent days. It is descriptive and never a diagnosis.
type Summary struct {
	Metric       string   `json:"metric"`
	Name         string   `json:"name"`
	Unit         string   `json:"unit"`
	Group        string   `json:"group"`
	Latest       float64  `json:"latest_value"`
	LatestAt     string   `json:"latest_at"`
	LatestDevice string   `json:"latest_device,omitempty"`
	Days7        int      `json:"days_with_data_7"`
	Mean7        *float64 `json:"mean_7_days"`
	Days30       int      `json:"days_with_data_30"`
	Mean30       *float64 `json:"mean_30_days"`
	Trend        string   `json:"trend"`
	TrendPct     *float64 `json:"trend_change_pct"`
	Typical      *Range   `json:"typical_adult_range,omitempty"`
	OutsideRange bool     `json:"outside_typical_range"`
}

// TrendThresholdPct is the relative change that counts as movement rather than noise.
const TrendThresholdPct = 5.0

// MinTrendDays is how many days of data each 30-day window needs before a trend is stated.
const MinTrendDays = 3

// Summarize builds a Summary from daily aggregates (any order) and the latest single reading.
// The trend compares the last 30 days with the 30 days before them.
func Summarize(m *Metric, days []Day, latest float64, latestAt time.Time, device string, now time.Time) Summary {
	s := Summary{Metric: m.Key, Name: m.Name, Unit: m.Unit, Group: m.Group, Latest: latest, LatestAt: latestAt.UTC().Format(time.RFC3339), LatestDevice: device, Trend: "insufficient_data", Typical: m.Typical}
	today := now.UTC().Truncate(24 * time.Hour)
	var w7, w30, prior []float64
	for _, d := range days {
		on, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		age := int(today.Sub(on).Hours() / 24)
		v := d.Value(m)
		switch {
		case age < 0:
		case age < 7:
			w7 = append(w7, v)
			w30 = append(w30, v)
		case age < 30:
			w30 = append(w30, v)
		case age < 60:
			prior = append(prior, v)
		}
	}
	s.Days7, s.Days30 = len(w7), len(w30)
	s.Mean7, s.Mean30 = mean(w7), mean(w30)
	if len(w30) >= MinTrendDays && len(prior) >= MinTrendDays {
		cur, before := *mean(w30), *mean(prior)
		if before != 0 {
			pct := math.Round((cur-before)/math.Abs(before)*1000) / 10
			s.TrendPct = &pct
			switch {
			case pct >= TrendThresholdPct:
				s.Trend = "rising"
			case pct <= -TrendThresholdPct:
				s.Trend = "falling"
			default:
				s.Trend = "stable"
			}
		}
	}
	if m.Typical != nil {
		ref := latest
		if s.Mean7 != nil {
			ref = *s.Mean7
		}
		s.OutsideRange = ref < m.Typical.Low || ref > m.Typical.High
	}
	return s
}

func mean(v []float64) *float64 {
	if len(v) == 0 {
		return nil
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	out := math.Round(sum/float64(len(v))*100) / 100
	return &out
}
