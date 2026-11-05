package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cortex/backend/internal/platform/embedding"
	"cortex/backend/internal/processing"
)

const (
	maxAIPages     = 6
	maxAIPageChars = 14000
)

var aiDocTypes = map[string]bool{
	"LAB_REPORT": true, "PRESCRIPTION": true, "DISCHARGE_SUMMARY": true, "RADIOLOGY_REPORT": true,
	"VACCINATION_RECORD": true, "MEDICAL_CERTIFICATE": true,
}

// readWithAI asks the model to read each page, so reports from any lab or layout, and prescriptions, are
// understood. It runs only with the person's AI consent and cloud permission, sends text with identity details
// removed, and only adds results that the page itself supports. Any failure leaves the pattern reading untouched.
func (w *DocumentWorker) readWithAI(ctx context.Context, d documentInput, res *processing.Result, catalog []processing.CatalogEntry) {
	if len(res.Pages) == 0 || !w.cloudAllowed(ctx, d.ProfileID) {
		return
	}
	merged := &processing.AIReading{}
	for i, page := range res.Pages {
		if i >= maxAIPages {
			break
		}
		keepDoctor := processing.Classify(res.Text).Type == "PRESCRIPTION"
		text := processing.ScrubIdentity(page, keepDoctor)
		if len(strings.TrimSpace(text)) < 20 {
			continue
		}
		if len(text) > maxAIPageChars {
			text = text[:maxAIPageChars]
		}
		reading, err := callExtractor(ctx, text)
		if err != nil {
			log.Printf("ai extraction page %d: %v", i+1, err)
			continue
		}
		res.AIAdded += processing.MergeAI(res, i+1, page, reading, catalog)
		if merged.DocumentType == "" {
			merged.DocumentType = reading.DocumentType
		}
		if merged.Provider == "" {
			merged.Provider = reading.Provider
		}
		if merged.ReportDate == "" {
			merged.ReportDate = reading.ReportDate
		}
		if merged.Doctor == "" {
			merged.Doctor = reading.Doctor
		}
		merged.Medicines = append(merged.Medicines, reading.Medicines...)
	}
	merged.DocumentType = strings.ToUpper(strings.TrimSpace(merged.DocumentType))
	if !aiDocTypes[merged.DocumentType] {
		merged.DocumentType = ""
	}
	if merged.DocumentType != "" || len(merged.Medicines) > 0 || res.AIAdded > 0 {
		res.AI = merged
	}
}

func callExtractor(ctx context.Context, text string) (processing.AIReading, error) {
	var out processing.AIReading
	body, _ := json.Marshal(map[string]any{"text": text})
	cctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, embedding.ServiceURL()+"/v1/documents/extract", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, io.ErrUnexpectedEOF
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&out)
	return out, err
}
