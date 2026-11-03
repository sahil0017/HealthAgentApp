package identity

import (
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
)

type ProfileResponse struct {
	ID                string    `json:"id"`
	DisplayName       string    `json:"display_name"`
	Timezone          string    `json:"timezone"`
	MeasurementSystem string    `json:"measurement_system"`
	DateOfBirth       *string   `json:"date_of_birth"`
	Sex               *string   `json:"sex"`
	BloodGroup        *string   `json:"blood_group"`
	HeightCM          *float64  `json:"height_cm"`
	CreatedAt         time.Time `json:"created_at"`
}

const profileColumns = `id,display_name,timezone,measurement_system,date_of_birth::text,sex,blood_group,height_cm::float8,created_at`

func (p *ProfileResponse) scan(row interface{ Scan(...any) error }) error {
	return row.Scan(&p.ID, &p.DisplayName, &p.Timezone, &p.MeasurementSystem, &p.DateOfBirth, &p.Sex, &p.BloodGroup, &p.HeightCM, &p.CreatedAt)
}

var (
	sexValues        = []string{"female", "male", "intersex", "prefer_not_to_say"}
	bloodGroupValues = []string{"A+", "A-", "B+", "B-", "AB+", "AB-", "O+", "O-"}
)

// validateHealthDetails checks the optional health fields of a profile patch and normalises them.
func validateHealthDetails(in *profilePatch, today time.Time) (string, bool) {
	if in.DateOfBirth != nil {
		day, err := httpx.ParseDay(in.DateOfBirth)
		if err != nil || day == nil {
			return "date_of_birth", false
		}
		parsed, _ := time.Parse("2006-01-02", *day)
		if parsed.After(today) || parsed.Before(today.AddDate(-130, 0, 0)) {
			return "date_of_birth", false
		}
		in.DateOfBirth = day
	}
	if in.Sex != nil && !httpx.Contains(sexValues, *in.Sex) {
		return "sex", false
	}
	if in.BloodGroup != nil {
		group := strings.ToUpper(strings.TrimSpace(*in.BloodGroup))
		if !httpx.Contains(bloodGroupValues, group) {
			return "blood_group", false
		}
		in.BloodGroup = &group
	}
	if in.HeightCM != nil && (*in.HeightCM < 30 || *in.HeightCM > 272) {
		return "height_cm", false
	}
	return "", true
}

func (m *Module) getProfile(w http.ResponseWriter, r *http.Request) {
	var profile ProfileResponse
	err := profile.scan(m.DB.Pool.QueryRow(r.Context(), `SELECT `+profileColumns+` FROM profiles WHERE id=$1 AND owner_id=$2`, chi.URLParam(r, "profileID"), httpx.Owner(r)))
	if err != nil {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	httpx.WriteJSON(w, 200, profile)
}

type profilePatch struct {
	DisplayName       *string  `json:"display_name"`
	Timezone          *string  `json:"timezone"`
	MeasurementSystem *string  `json:"measurement_system"`
	DateOfBirth       *string  `json:"date_of_birth"`
	Sex               *string  `json:"sex"`
	BloodGroup        *string  `json:"blood_group"`
	HeightCM          *float64 `json:"height_cm"`
}

func (m *Module) updateProfile(w http.ResponseWriter, r *http.Request) {
	var in profilePatch
	if httpx.DecodeJSON(r, 16<<10, &in) != nil || (in.DisplayName == nil && in.Timezone == nil && in.MeasurementSystem == nil && in.DateOfBirth == nil && in.Sex == nil && in.BloodGroup == nil && in.HeightCM == nil) {
		httpx.WriteError(w, 400, "INVALID_PROFILE", "Provide at least one valid profile field.", nil)
		return
	}
	if in.DisplayName != nil {
		value := strings.TrimSpace(*in.DisplayName)
		if value == "" || len(value) > 120 {
			httpx.WriteError(w, 400, "INVALID_PROFILE", "Profile name must contain 1 to 120 characters.", map[string]string{"display_name": "invalid"})
			return
		}
		in.DisplayName = &value
	}
	if in.Timezone != nil {
		value := strings.TrimSpace(*in.Timezone)
		if _, err := time.LoadLocation(value); err != nil {
			httpx.WriteError(w, 400, "INVALID_PROFILE", "Timezone must be a valid IANA timezone.", map[string]string{"timezone": "invalid"})
			return
		}
		in.Timezone = &value
	}
	if in.MeasurementSystem != nil {
		value := strings.TrimSpace(*in.MeasurementSystem)
		if value != "metric" && value != "us_customary" {
			httpx.WriteError(w, 400, "INVALID_PROFILE", "Measurement system must be metric or us_customary.", map[string]string{"measurement_system": "invalid"})
			return
		}
		in.MeasurementSystem = &value
	}
	if field, ok := validateHealthDetails(&in, time.Now().UTC()); !ok {
		httpx.WriteError(w, 400, "INVALID_PROFILE", "The "+field+" value is not valid.", map[string]string{field: "invalid"})
		return
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "PROFILE_UPDATE_FAILED", "The profile could not be updated.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var profile ProfileResponse
	row := tx.QueryRow(r.Context(), `UPDATE profiles SET display_name=COALESCE($3,display_name),timezone=COALESCE($4,timezone),measurement_system=COALESCE($5,measurement_system),date_of_birth=COALESCE($6::text::date,date_of_birth),sex=COALESCE($7,sex),blood_group=COALESCE($8,blood_group),height_cm=COALESCE($9,height_cm) WHERE id=$1 AND owner_id=$2 RETURNING `+profileColumns, chi.URLParam(r, "profileID"), httpx.Owner(r), in.DisplayName, in.Timezone, in.MeasurementSystem, in.DateOfBirth, in.Sex, in.BloodGroup, in.HeightCM)
	err = profile.scan(row)
	if err != nil {
		httpx.WriteError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'profile.updated','profile',$2::uuid::text)`, httpx.Owner(r), profile.ID); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, 500, "PROFILE_UPDATE_FAILED", "The profile could not be updated.", nil)
		return
	}
	httpx.WriteJSON(w, 200, profile)
}
