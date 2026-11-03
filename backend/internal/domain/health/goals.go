// Package health holds the pure rules behind the command centre: goal evaluation, change summaries,
// data-completeness scoring and follow-up suggestions. No database or HTTP code lives here.
package health

import (
	"cortex/backend/internal/domain/biomarker"
	"strings"
)

type GoalTarget struct {
	Min  *float64
	Max  *float64
	Unit string
}

type GoalEvaluation struct {
	Status string `json:"status"` // no_data, unit_mismatch, on_target, below_target, above_target
	Trend  string `json:"trend"`  // improving, worsening, steady, unknown
}

func DistanceFromTarget(t GoalTarget, value float64) float64 {
	switch {
	case t.Min != nil && value < *t.Min:
		return *t.Min - value
	case t.Max != nil && value > *t.Max:
		return value - *t.Max
	}
	return 0
}

// EvaluateGoal compares the latest reading with the user's own target. It never compares
// values recorded in different units, because converting them would need clinical rules.
func EvaluateGoal(t GoalTarget, latest, previous *biomarker.Reading) GoalEvaluation {
	if latest == nil {
		return GoalEvaluation{Status: "no_data", Trend: "unknown"}
	}
	if !strings.EqualFold(strings.TrimSpace(latest.Unit), strings.TrimSpace(t.Unit)) {
		return GoalEvaluation{Status: "unit_mismatch", Trend: "unknown"}
	}
	status := "on_target"
	switch {
	case t.Min != nil && latest.Value < *t.Min:
		status = "below_target"
	case t.Max != nil && latest.Value > *t.Max:
		status = "above_target"
	}
	trend := "unknown"
	if previous != nil && strings.EqualFold(strings.TrimSpace(previous.Unit), strings.TrimSpace(t.Unit)) {
		before, now := DistanceFromTarget(t, previous.Value), DistanceFromTarget(t, latest.Value)
		switch {
		case now < before:
			trend = "improving"
		case now > before:
			trend = "worsening"
		default:
			trend = "steady"
		}
	}
	return GoalEvaluation{Status: status, Trend: trend}
}
