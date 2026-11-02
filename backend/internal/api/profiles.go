package api

import (
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/go-chi/chi/v5"
)

type profileResponse struct {
	ID                string    `json:"id"`
	DisplayName       string    `json:"display_name"`
	Timezone          string    `json:"timezone"`
	MeasurementSystem string    `json:"measurement_system"`
	CreatedAt         time.Time `json:"created_at"`
}

func (a *API) getProfile(w http.ResponseWriter, r *http.Request) {
	var profile profileResponse
	err := a.db.Pool.QueryRow(r.Context(), `SELECT id,display_name,timezone,measurement_system,created_at FROM profiles WHERE id=$1 AND owner_id=$2`, chi.URLParam(r, "profileID"), owner(r)).Scan(&profile.ID, &profile.DisplayName, &profile.Timezone, &profile.MeasurementSystem, &profile.CreatedAt)
	if err != nil {
		writeError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	writeJSON(w, 200, profile)
}

type profilePatch struct {
	DisplayName       *string `json:"display_name"`
	Timezone          *string `json:"timezone"`
	MeasurementSystem *string `json:"measurement_system"`
}

func (a *API) updateProfile(w http.ResponseWriter, r *http.Request) {
	var in profilePatch
	if decodeJSON(r, 16<<10, &in) != nil || (in.DisplayName == nil && in.Timezone == nil && in.MeasurementSystem == nil) {
		writeError(w, 400, "INVALID_PROFILE", "Provide at least one valid profile field.", nil)
		return
	}
	if in.DisplayName != nil {
		value := strings.TrimSpace(*in.DisplayName)
		if value == "" || len(value) > 120 {
			writeError(w, 400, "INVALID_PROFILE", "Profile name must contain 1 to 120 characters.", map[string]string{"display_name": "invalid"})
			return
		}
		in.DisplayName = &value
	}
	if in.Timezone != nil {
		value := strings.TrimSpace(*in.Timezone)
		if _, err := time.LoadLocation(value); err != nil {
			writeError(w, 400, "INVALID_PROFILE", "Timezone must be a valid IANA timezone.", map[string]string{"timezone": "invalid"})
			return
		}
		in.Timezone = &value
	}
	if in.MeasurementSystem != nil {
		value := strings.TrimSpace(*in.MeasurementSystem)
		if value != "metric" && value != "us_customary" {
			writeError(w, 400, "INVALID_PROFILE", "Measurement system must be metric or us_customary.", map[string]string{"measurement_system": "invalid"})
			return
		}
		in.MeasurementSystem = &value
	}
	tx, err := a.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "PROFILE_UPDATE_FAILED", "The profile could not be updated.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var profile profileResponse
	err = tx.QueryRow(r.Context(), `UPDATE profiles SET display_name=COALESCE($3,display_name),timezone=COALESCE($4,timezone),measurement_system=COALESCE($5,measurement_system) WHERE id=$1 AND owner_id=$2 RETURNING id,display_name,timezone,measurement_system,created_at`, chi.URLParam(r, "profileID"), owner(r), in.DisplayName, in.Timezone, in.MeasurementSystem).Scan(&profile.ID, &profile.DisplayName, &profile.Timezone, &profile.MeasurementSystem, &profile.CreatedAt)
	if err != nil {
		writeError(w, 404, "PROFILE_NOT_FOUND", "The requested profile could not be found.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,'profile.updated','profile',$2::uuid::text)`, owner(r), profile.ID); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "PROFILE_UPDATE_FAILED", "The profile could not be updated.", nil)
		return
	}
	writeJSON(w, 200, profile)
}
