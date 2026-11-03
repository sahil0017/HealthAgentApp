package devices

import (
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/domain/device"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxDevicesPerProfile = 50

var deviceKinds = []string{"phone_health_app", "smartwatch", "fitness_band", "blood_pressure_monitor", "glucometer", "cgm", "scale", "pulse_oximeter", "thermometer", "other"}
var deviceSources = []string{"healthkit", "bluetooth", "import", "manual"}

type deviceRecord struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Source       string     `json:"source"`
	Name         string     `json:"name"`
	Manufacturer *string    `json:"manufacturer,omitempty"`
	Model        *string    `json:"model,omitempty"`
	ExternalID   *string    `json:"external_id,omitempty"`
	Status       string     `json:"status"`
	LastSyncedAt *time.Time `json:"last_synced_at"`
	ReadingCount int        `json:"reading_count"`
	CreatedAt    time.Time  `json:"created_at"`
}

const deviceColumns = `d.id::text,d.kind,d.source,d.name,d.manufacturer,d.model,d.external_id,d.status,d.last_synced_at,
	(SELECT count(*) FROM device_readings r WHERE r.device_id=d.id),d.created_at`

func scanDevice(row interface{ Scan(...any) error }) (deviceRecord, error) {
	var d deviceRecord
	err := row.Scan(&d.ID, &d.Kind, &d.Source, &d.Name, &d.Manufacturer, &d.Model, &d.ExternalID, &d.Status, &d.LastSyncedAt, &d.ReadingCount, &d.CreatedAt)
	return d, err
}

// requireIntegrationConsent gates device writes on the dedicated integration consent.
func (m *Module) requireIntegrationConsent(w http.ResponseWriter, r *http.Request) bool {
	if !m.HasConsent(r.Context(), chi.URLParam(r, "profileID"), "health_data_integration") {
		httpx.WriteError(w, http.StatusForbidden, "CONSENT_REQUIRED", "Connecting devices requires your health data integration consent.", nil)
		return false
	}
	return true
}

// ownedDevice resolves {deviceID} within the caller's profile, writing a 404 when it is not theirs.
func (m *Module) ownedDevice(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "deviceID")
	var found bool
	if _, err := uuid.Parse(id); err == nil {
		_ = m.DB.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1 AND profile_id=$2 AND owner_id=$3)`, id, chi.URLParam(r, "profileID"), httpx.Owner(r)).Scan(&found)
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "DEVICE_NOT_FOUND", "The requested device could not be found.", nil)
	}
	return id, found
}

func (m *Module) metricCatalog(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, device.Metrics())
}

func (m *Module) listDevices(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	rows, err := m.DB.Pool.Query(r.Context(), `SELECT `+deviceColumns+` FROM devices d WHERE d.profile_id=$1 AND d.owner_id=$2 ORDER BY d.created_at DESC`, chi.URLParam(r, "profileID"), httpx.Owner(r))
	if err != nil {
		httpx.WriteError(w, 500, "DEVICES_UNAVAILABLE", "Devices could not be loaded.", nil)
		return
	}
	defer rows.Close()
	out := []deviceRecord{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			httpx.WriteError(w, 500, "DEVICES_UNAVAILABLE", "Devices could not be loaded.", nil)
			return
		}
		out = append(out, d)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type deviceInput struct {
	Kind         string  `json:"kind"`
	Source       string  `json:"source"`
	Name         string  `json:"name"`
	Manufacturer *string `json:"manufacturer"`
	Model        *string `json:"model"`
	ExternalID   *string `json:"external_id"`
}

func optionalText(v *string, max int) (*string, bool) {
	if v == nil {
		return nil, true
	}
	t := strings.TrimSpace(*v)
	if t == "" {
		return nil, true
	}
	return &t, len(t) <= max
}

// registerDevice is idempotent on (source, external_id): reconnecting the same device returns it again.
func (m *Module) registerDevice(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) || !m.requireIntegrationConsent(w, r) {
		return
	}
	var in deviceInput
	if httpx.DecodeJSON(r, 8<<10, &in) != nil {
		httpx.Invalid(w, "INVALID_DEVICE", "The device details are invalid.", "body")
		return
	}
	name := strings.TrimSpace(in.Name)
	maker, okMaker := optionalText(in.Manufacturer, 120)
	model, okModel := optionalText(in.Model, 120)
	external, okExt := optionalText(in.ExternalID, 200)
	switch {
	case !httpx.Contains(deviceKinds, in.Kind):
		httpx.Invalid(w, "INVALID_DEVICE", "Choose a supported device kind.", "kind")
		return
	case !httpx.Contains(deviceSources, in.Source):
		httpx.Invalid(w, "INVALID_DEVICE", "Choose a supported connection source.", "source")
		return
	case name == "" || len(name) > 120:
		httpx.Invalid(w, "INVALID_DEVICE", "A device name of up to 120 characters is required.", "name")
		return
	case !okMaker || !okModel || !okExt:
		httpx.Invalid(w, "INVALID_DEVICE", "Manufacturer, model and identifier are too long.", "body")
		return
	}
	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "DEVICE_CREATE_FAILED", "The device could not be saved.", nil)
		return
	}
	defer tx.Rollback(r.Context())

	if external != nil {
		var existing string
		if err := tx.QueryRow(r.Context(), `UPDATE devices SET status='connected',name=$4,updated_at=now() WHERE profile_id=$1 AND owner_id=$2 AND source=$3 AND external_id=$5 RETURNING id::text`, profileID, owner, in.Source, name, *external).Scan(&existing); err == nil {
			d, derr := scanDevice(tx.QueryRow(r.Context(), `SELECT `+deviceColumns+` FROM devices d WHERE d.id=$1`, existing))
			if derr != nil || tx.Commit(r.Context()) != nil {
				httpx.WriteError(w, 500, "DEVICE_CREATE_FAILED", "The device could not be saved.", nil)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, d)
			return
		}
	}
	var count int
	if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM devices WHERE profile_id=$1 AND owner_id=$2`, profileID, owner).Scan(&count); err != nil {
		httpx.WriteError(w, 500, "DEVICE_CREATE_FAILED", "The device could not be saved.", nil)
		return
	}
	if count >= maxDevicesPerProfile {
		httpx.WriteError(w, http.StatusConflict, "DEVICE_LIMIT_REACHED", "This profile has reached the device limit. Remove a device first.", nil)
		return
	}
	var id string
	if err := tx.QueryRow(r.Context(), `INSERT INTO devices(profile_id,owner_id,kind,source,name,manufacturer,model,external_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`, profileID, owner, in.Kind, in.Source, name, maker, model, external).Scan(&id); err != nil {
		httpx.WriteError(w, 500, "DEVICE_CREATE_FAILED", "The device could not be saved.", nil)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'device.connected','device',$3,jsonb_build_object('kind',$4::text,'source',$5::text))`, owner, profileID, id, in.Kind, in.Source); err != nil {
		httpx.WriteError(w, 500, "DEVICE_CREATE_FAILED", "The device could not be saved.", nil)
		return
	}
	d, err := scanDevice(tx.QueryRow(r.Context(), `SELECT `+deviceColumns+` FROM devices d WHERE d.id=$1`, id))
	if err != nil || tx.Commit(r.Context()) != nil {
		httpx.WriteError(w, 500, "DEVICE_CREATE_FAILED", "The device could not be saved.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, d)
}

type devicePatch struct {
	Name   *string `json:"name"`
	Status *string `json:"status"`
}

// updateDevice renames a device or disconnects and reconnects it. Disconnecting keeps its history and
// stops further syncs.
func (m *Module) updateDevice(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	id, ok := m.ownedDevice(w, r)
	if !ok {
		return
	}
	var in devicePatch
	if httpx.DecodeJSON(r, 4<<10, &in) != nil || (in.Name == nil && in.Status == nil) {
		httpx.Invalid(w, "INVALID_DEVICE", "Provide a name or a status.", "body")
		return
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > 120 {
			httpx.Invalid(w, "INVALID_DEVICE", "A device name of up to 120 characters is required.", "name")
			return
		}
		in.Name = &n
	}
	if in.Status != nil {
		if *in.Status != "connected" && *in.Status != "disconnected" {
			httpx.Invalid(w, "INVALID_DEVICE", "Status must be connected or disconnected.", "status")
			return
		}
		if *in.Status == "connected" && !m.requireIntegrationConsent(w, r) {
			return
		}
	}
	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "DEVICE_UPDATE_FAILED", "The device could not be updated.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE devices SET name=COALESCE($2,name),status=COALESCE($3,status),updated_at=now() WHERE id=$1`, id, in.Name, in.Status); err != nil {
		httpx.WriteError(w, 500, "DEVICE_UPDATE_FAILED", "The device could not be updated.", nil)
		return
	}
	if in.Status != nil {
		action := "device.disconnected"
		if *in.Status == "connected" {
			action = "device.reconnected"
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,$3,'device',$4)`, owner, profileID, action, id); err != nil {
			httpx.WriteError(w, 500, "DEVICE_UPDATE_FAILED", "The device could not be updated.", nil)
			return
		}
	}
	d, err := scanDevice(tx.QueryRow(r.Context(), `SELECT `+deviceColumns+` FROM devices d WHERE d.id=$1`, id))
	if err != nil || tx.Commit(r.Context()) != nil {
		httpx.WriteError(w, 500, "DEVICE_UPDATE_FAILED", "The device could not be updated.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, d)
}

// removeDevice erases the device together with every reading and sync record it produced.
func (m *Module) removeDevice(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	id, ok := m.ownedDevice(w, r)
	if !ok {
		return
	}
	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "DEVICE_DELETE_FAILED", "The device could not be removed.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var readings int
	if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM device_readings WHERE device_id=$1`, id).Scan(&readings); err != nil {
		httpx.WriteError(w, 500, "DEVICE_DELETE_FAILED", "The device could not be removed.", nil)
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM devices WHERE id=$1`, id); err != nil {
		httpx.WriteError(w, 500, "DEVICE_DELETE_FAILED", "The device could not be removed.", nil)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id,details) VALUES($1,$2,'device.removed','device',$3,jsonb_build_object('readings_deleted',$4::int))`, owner, profileID, id, readings); err != nil {
		httpx.WriteError(w, 500, "DEVICE_DELETE_FAILED", "The device could not be removed.", nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, 500, "DEVICE_DELETE_FAILED", "The device could not be removed.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "readings_deleted": readings})
}
