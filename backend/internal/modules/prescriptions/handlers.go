package prescriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/domain/schedule"
	"cortex/backend/internal/platform/embedding"
	"cortex/backend/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxImageBytes   = 8 << 20
	maxItems        = 40
	maxRxPerProfile = 500
)

var imageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

const readerNote = "Handwriting is hard to read and a reading can be wrong. Compare every line with the original and correct it before saving. This is not medical advice; follow your doctor and pharmacist."

type item struct {
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name"`
	Strength      string   `json:"strength"`
	Form          string   `json:"form"`
	Dose          string   `json:"dose"`
	FrequencyText string   `json:"frequency_text"`
	TimingText    string   `json:"timing_text"`
	DurationText  string   `json:"duration_text"`
	Instructions  string   `json:"instructions"`
	Slots         []string `json:"slots"`
	WithFood      string   `json:"with_food"`
	AsNeeded      bool     `json:"as_needed"`
	DurationDays  *int     `json:"duration_days"`
	EndsOn        *string  `json:"ends_on"`
	Confidence    *float64 `json:"confidence"`
	Active        bool     `json:"active"`
	Understood    bool     `json:"understood"`
	Summary       string   `json:"summary"`
	Times         []string `json:"times"`
}

type prescription struct {
	ID           string    `json:"id"`
	Doctor       string    `json:"doctor"`
	PrescribedOn *string   `json:"prescribed_on"`
	Source       string    `json:"source"`
	CreatedAt    time.Time `json:"created_at"`
	Items        []item    `json:"items"`
}

type readerMedicine struct {
	Name         string  `json:"name"`
	Strength     string  `json:"strength"`
	Form         string  `json:"form"`
	Dose         string  `json:"dose"`
	Frequency    string  `json:"frequency"`
	Timing       string  `json:"timing"`
	Duration     string  `json:"duration"`
	Instructions string  `json:"instructions"`
	Confidence   float64 `json:"confidence"`
}

type readerResult struct {
	Medicines      []readerMedicine `json:"medicines"`
	Doctor         string           `json:"doctor"`
	Date           string           `json:"date"`
	Unreadable     []string         `json:"unreadable"`
	IsPrescription bool             `json:"is_prescription"`
	Model          string           `json:"model"`
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// fill derives the schedule fields shown to the person. The stored slots always win over the text.
func fill(it *item, parsed schedule.Result, useParsed bool) {
	if useParsed {
		it.Slots, it.WithFood, it.AsNeeded, it.Understood = parsed.Slots, parsed.WithFood, parsed.AsNeeded, parsed.Understood
	} else {
		it.Understood = len(it.Slots) > 0 || it.AsNeeded
	}
	if it.Slots == nil {
		it.Slots = []string{}
	}
	it.Summary = schedule.Summarise(it.Slots, it.WithFood, it.AsNeeded)
	it.Times = make([]string, len(it.Slots))
	for i, s := range it.Slots {
		it.Times[i] = schedule.DefaultTimes[s]
	}
}

func (m *Module) audit(ctx context.Context, owner, profileID, action string, id *string) {
	_, _ = m.DB.Pool.Exec(ctx, `INSERT INTO audit_events(owner_id,profile_id,action,resource_type,resource_id) VALUES($1,$2,$3,'prescription',$4)`, owner, profileID, action, id)
}

// read sends one photo to the AI service and returns an unsaved draft. It needs explicit confirmation that the
// image may leave the app (cloud=1) and the person's AI-processing consent. The image is never stored.
func (m *Module) read(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	profileID := chi.URLParam(r, "profileID")
	if r.URL.Query().Get("cloud") != "1" {
		httpx.WriteError(w, http.StatusBadRequest, "CLOUD_CONFIRMATION_REQUIRED", "Confirm that this photo may be sent to the AI reader.", nil)
		return
	}
	if !m.HasConsent(r.Context(), profileID, "ai_processing") {
		httpx.WriteError(w, http.StatusForbidden, "CONSENT_REQUIRED", "Reading a prescription with AI requires your AI processing consent.", nil)
		return
	}
	mime := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if !imageTypes[mime] {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_IMAGE", "Use a JPEG, PNG or WebP photo.", nil)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImageBytes))
	if err != nil || len(body) == 0 {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "IMAGE_SIZE_INVALID", "The photo must be between 1 byte and 8 MB.", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, embedding.ServiceURL()+"/v1/prescription/read", bytes.NewReader(body))
	req.Header.Set("Content-Type", mime)
	resp, err := (&http.Client{}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		httpx.WriteError(w, http.StatusServiceUnavailable, "READER_UNAVAILABLE", "The prescription reader is not available. You can add the medicines by hand.", nil)
		return
	}
	defer resp.Body.Close()
	var out readerResult
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil {
		httpx.WriteError(w, http.StatusBadGateway, "READER_FAILED", "The prescription could not be read.", nil)
		return
	}
	m.audit(r.Context(), httpx.Owner(r), profileID, "prescription.read", nil)
	if !out.IsPrescription {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"is_prescription": false, "items": []item{}, "unreadable": out.Unreadable, "model": out.Model, "note": readerNote})
		return
	}
	items := []item{}
	for _, med := range out.Medicines {
		if len(items) >= maxItems || strings.TrimSpace(med.Name) == "" {
			continue
		}
		it := item{
			Name: clip(med.Name, 160), Strength: clip(med.Strength, 80), Form: clip(med.Form, 60), Dose: clip(med.Dose, 80),
			FrequencyText: clip(med.Frequency, 120), TimingText: clip(med.Timing, 120), DurationText: clip(med.Duration, 80),
			Instructions: clip(med.Instructions, 500), Active: true,
		}
		c := med.Confidence
		if c < 0 {
			c = 0
		} else if c > 1 {
			c = 1
		}
		it.Confidence = &c
		if d := schedule.DurationDays(it.DurationText); d > 0 {
			it.DurationDays = &d
		}
		fill(&it, schedule.Parse(it.FrequencyText, it.TimingText), true)
		items = append(items, it)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"is_prescription": true, "doctor": clip(out.Doctor, 160), "date": clip(out.Date, 40), "items": items,
		"unreadable": out.Unreadable, "model": out.Model, "note": readerNote,
	})
}

type saveInput struct {
	Doctor       string  `json:"doctor"`
	PrescribedOn *string `json:"prescribed_on"`
	Source       string  `json:"source"`
	Items        []item  `json:"items"`
}

func (m *Module) save(w http.ResponseWriter, r *http.Request) {
	if !m.CanStore(w, r) {
		return
	}
	var in saveInput
	if httpx.DecodeJSON(r, 256<<10, &in) != nil || len(in.Items) == 0 || len(in.Items) > maxItems {
		httpx.Invalid(w, "INVALID_PRESCRIPTION", "Add between 1 and 40 medicines.", "items")
		return
	}
	if in.Source != "photo" {
		in.Source = "manual"
	}
	day, err := httpx.ParseDay(in.PrescribedOn)
	if err != nil {
		httpx.Invalid(w, "INVALID_DATE", "Use a date like 2026-07-31.", "prescribed_on")
		return
	}
	doctor := clip(in.Doctor, 160)
	valid := map[string]bool{}
	for _, s := range schedule.Slots {
		valid[s] = true
	}
	foods := map[string]bool{schedule.FoodAny: true, schedule.FoodBefore: true, schedule.FoodAfter: true, schedule.FoodWith: true, schedule.FoodEmpty: true}
	for i := range in.Items {
		it := &in.Items[i]
		it.Name = clip(it.Name, 160)
		if it.Name == "" {
			httpx.Invalid(w, "INVALID_MEDICINE", "Every medicine needs a name.", "items")
			return
		}
		it.Strength, it.Form, it.Dose = clip(it.Strength, 80), clip(it.Form, 60), clip(it.Dose, 80)
		it.FrequencyText, it.TimingText, it.DurationText = clip(it.FrequencyText, 120), clip(it.TimingText, 120), clip(it.DurationText, 80)
		it.Instructions = clip(it.Instructions, 500)
		seen, slots := map[string]bool{}, []string{}
		for _, s := range it.Slots {
			if !valid[s] {
				httpx.Invalid(w, "INVALID_SLOT", "Slots must be morning, afternoon, evening or night.", "items")
				return
			}
			seen[s] = true
		}
		for _, s := range schedule.Slots {
			if seen[s] {
				slots = append(slots, s)
			}
		}
		it.Slots = slots
		if it.WithFood == "" {
			it.WithFood = schedule.FoodAny
		}
		if !foods[it.WithFood] {
			httpx.Invalid(w, "INVALID_FOOD", "Food timing must be any, before, after, with or empty.", "items")
			return
		}
		if it.Confidence != nil && (*it.Confidence < 0 || *it.Confidence > 1) {
			it.Confidence = nil
		}
		if it.DurationDays == nil {
			if d := schedule.DurationDays(it.DurationText); d > 0 {
				it.DurationDays = &d
			}
		}
		if it.DurationDays != nil && (*it.DurationDays < 1 || *it.DurationDays > 730) {
			httpx.Invalid(w, "INVALID_DURATION", "Duration must be between 1 and 730 days.", "items")
			return
		}
	}
	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	var count int
	if err := m.DB.Pool.QueryRow(r.Context(), `SELECT count(*) FROM prescriptions WHERE profile_id=$1 AND owner_id=$2`, profileID, owner).Scan(&count); err != nil {
		httpx.WriteError(w, 500, "PRESCRIPTION_SAVE_FAILED", "The prescription could not be saved.", nil)
		return
	}
	if count >= maxRxPerProfile {
		httpx.WriteError(w, http.StatusConflict, "PRESCRIPTION_LIMIT_REACHED", "This profile has reached the prescription limit.", nil)
		return
	}
	tx, err := m.DB.Pool.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "PRESCRIPTION_SAVE_FAILED", "The prescription could not be saved.", nil)
		return
	}
	defer tx.Rollback(r.Context())
	var rxID string
	if err := tx.QueryRow(r.Context(), `INSERT INTO prescriptions(profile_id,owner_id,doctor,prescribed_on,source) VALUES($1,$2,NULLIF($3,''),$4::date,$5) RETURNING id::text`,
		profileID, owner, doctor, day, in.Source).Scan(&rxID); err != nil {
		httpx.WriteError(w, 500, "PRESCRIPTION_SAVE_FAILED", "The prescription could not be saved.", nil)
		return
	}
	for _, it := range in.Items {
		// A fixed course ends a known number of days after the prescription (or today when undated).
		_, err := tx.Exec(r.Context(), `INSERT INTO prescription_items(prescription_id,profile_id,name,strength,form,dose,frequency_text,timing_text,duration_text,instructions,slots,with_food,as_needed,duration_days,ends_on,confidence)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::int,CASE WHEN $14::int IS NULL THEN NULL ELSE COALESCE($15::date,CURRENT_DATE)+$14::int END,$16)`,
			rxID, profileID, it.Name, it.Strength, it.Form, it.Dose, it.FrequencyText, it.TimingText, it.DurationText, it.Instructions,
			it.Slots, it.WithFood, it.AsNeeded, it.DurationDays, day, it.Confidence)
		if err != nil {
			httpx.WriteError(w, 500, "PRESCRIPTION_SAVE_FAILED", "The prescription could not be saved.", nil)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, 500, "PRESCRIPTION_SAVE_FAILED", "The prescription could not be saved.", nil)
		return
	}
	m.audit(r.Context(), owner, profileID, "prescription.save", &rxID)
	rx, err := m.load(r.Context(), profileID, owner, rxID)
	if err != nil || len(rx) == 0 {
		httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": rxID})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rx[0])
}

const itemColumns = `i.id::text,i.name,i.strength,i.form,i.dose,i.frequency_text,i.timing_text,i.duration_text,i.instructions,i.slots,i.with_food,i.as_needed,i.duration_days,to_char(i.ends_on,'YYYY-MM-DD'),i.confidence,(i.active AND (i.ends_on IS NULL OR i.ends_on>=CURRENT_DATE))`

func scanItem(row interface{ Scan(...any) error }) (item, error) {
	var it item
	err := row.Scan(&it.ID, &it.Name, &it.Strength, &it.Form, &it.Dose, &it.FrequencyText, &it.TimingText, &it.DurationText, &it.Instructions,
		&it.Slots, &it.WithFood, &it.AsNeeded, &it.DurationDays, &it.EndsOn, &it.Confidence, &it.Active)
	fill(&it, schedule.Result{}, false)
	return it, err
}

// load returns prescriptions (newest first) with their items; rxID narrows it to one prescription.
func (m *Module) load(ctx context.Context, profileID, owner, rxID string) ([]prescription, error) {
	rows, err := m.DB.Pool.Query(ctx, `SELECT id::text,COALESCE(doctor,''),to_char(prescribed_on,'YYYY-MM-DD'),source,created_at FROM prescriptions
		WHERE profile_id=$1 AND owner_id=$2 AND ($3='' OR id::text=$3) ORDER BY created_at DESC LIMIT 200`, profileID, owner, rxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, index := []prescription{}, map[string]int{}
	for rows.Next() {
		var p prescription
		if err := rows.Scan(&p.ID, &p.Doctor, &p.PrescribedOn, &p.Source, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Items = []item{}
		index[p.ID] = len(out)
		out = append(out, p)
	}
	rows.Close()
	irows, err := m.DB.Pool.Query(ctx, `SELECT i.prescription_id::text,`+itemColumns+` FROM prescription_items i JOIN prescriptions p ON p.id=i.prescription_id
		WHERE p.profile_id=$1 AND p.owner_id=$2 AND ($3='' OR p.id::text=$3) ORDER BY i.created_at,i.name`, profileID, owner, rxID)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	for irows.Next() {
		var rx string
		var it item
		if err := irows.Scan(&rx, &it.ID, &it.Name, &it.Strength, &it.Form, &it.Dose, &it.FrequencyText, &it.TimingText, &it.DurationText, &it.Instructions,
			&it.Slots, &it.WithFood, &it.AsNeeded, &it.DurationDays, &it.EndsOn, &it.Confidence, &it.Active); err != nil {
			return nil, err
		}
		fill(&it, schedule.Result{}, false)
		if i, ok := index[rx]; ok {
			out[i].Items = append(out[i].Items, it)
		}
	}
	return out, irows.Err()
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	out, err := m.load(r.Context(), chi.URLParam(r, "profileID"), httpx.Owner(r), "")
	if err != nil {
		httpx.WriteError(w, 500, "PRESCRIPTION_LOAD_FAILED", "Prescriptions could not be loaded.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type scheduleSlot struct {
	Slot  string `json:"slot"`
	Label string `json:"label"`
	Time  string `json:"time"`
	Items []item `json:"items"`
}

// dailySchedule groups every still-active medicine by time of day.
func (m *Module) dailySchedule(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	rxs, err := m.load(r.Context(), chi.URLParam(r, "profileID"), httpx.Owner(r), "")
	if err != nil {
		httpx.WriteError(w, 500, "PRESCRIPTION_LOAD_FAILED", "Prescriptions could not be loaded.", nil)
		return
	}
	slots := make([]scheduleSlot, len(schedule.Slots))
	for i, s := range schedule.Slots {
		slots[i] = scheduleSlot{Slot: s, Label: schedule.Labels[s], Time: schedule.DefaultTimes[s], Items: []item{}}
	}
	asNeeded, unscheduled := []item{}, []item{}
	for _, rx := range rxs {
		for _, it := range rx.Items {
			if !it.Active {
				continue
			}
			placed := false
			for i, s := range schedule.Slots {
				if httpx.Contains(it.Slots, s) {
					slots[i].Items = append(slots[i].Items, it)
					placed = true
				}
			}
			switch {
			case it.AsNeeded:
				asNeeded = append(asNeeded, it)
			case !placed:
				unscheduled = append(unscheduled, it)
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"slots": slots, "as_needed": asNeeded, "unscheduled": unscheduled,
		"note": "Times are general suggestions based on common prescription shorthand. Follow your prescriber's instructions.",
	})
}

func (m *Module) remove(w http.ResponseWriter, r *http.Request) {
	if !m.ProfileReadable(w, r) {
		return
	}
	id := chi.URLParam(r, "rxID")
	if _, err := uuid.Parse(id); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "PRESCRIPTION_NOT_FOUND", "Prescription not found.", nil)
		return
	}
	profileID, owner := chi.URLParam(r, "profileID"), httpx.Owner(r)
	tag, err := m.DB.Pool.Exec(r.Context(), `DELETE FROM prescriptions WHERE id=$1 AND profile_id=$2 AND owner_id=$3`, id, profileID, owner)
	if err != nil {
		httpx.WriteError(w, 500, "PRESCRIPTION_DELETE_FAILED", "The prescription could not be deleted.", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, http.StatusNotFound, "PRESCRIPTION_NOT_FOUND", "Prescription not found.", nil)
		return
	}
	m.audit(r.Context(), owner, profileID, "prescription.delete", &id)
	w.WriteHeader(http.StatusNoContent)
}
