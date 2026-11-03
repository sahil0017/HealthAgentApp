# CORTEX product requirements

The twelve product areas the full platform must provide. Each area lists its capabilities, the status today, and the hard constraints. Delivery order and tracking live in [milestones.md](./milestones.md); how it is built lives in [technical-architecture.md](./technical-architecture.md).

Status: **Built** (works and tested), **Partial**, **Not started**.

## Cross-cutting rules

These apply to every area and are acceptance criteria, not features.

1. **Not a diagnostic device.** CORTEX organises, explains and cites. It does not diagnose, prescribe or replace a clinician, laboratory test or medical imaging. Urgent-looking findings get calm escalation guidance.
2. **User-approved data only.** Extracted values, device readings and photos become part of the record only after the user (or a verified source) accepts them. Unverified data is always labelled.
3. **Every claim has a source.** Answers, charts and summaries link back to the report, device, or publication they came from.
4. **Consent per purpose.** Each integration (documents, AI, research, HealthKit, camera, location, sharing) has its own consent, provenance, evaluation and deletion path.
5. **No built-in clinical data.** Catalogs, ranges, synonyms and literature are learned from open standards and reviewed by a human before use.
6. **Uncertainty is shown.** Missing, conflicting, stale or low-confidence data is displayed, never hidden.
7. **Private by default.** Health content never appears in logs; only public terms leave the system for research; external model calls require explicit consent.
8. **Audited.** Access, AI use, tool calls and sharing are recorded and visible to the user.

## 01. Personal Health Command Centre — Not started

- Unified overview ("Today"): latest key values, what changed, what needs attention
- Personal profile and medical history (conditions, allergies, surgeries, family history)
- Longitudinal health timeline across documents, labs, devices, visits and notes
- Health goals and progress
- Pending investigations and follow-ups
- Data completeness ("no HbA1c in 14 months")
- Proactive notifications and reminders

## 02. Medical Intelligence Vault — Partial

Built: signed upload, private storage, versions, worker extraction, review and approval, audit.

- Lab, pathology, radiology reports; prescriptions and medication history; discharge summaries; consultation notes; certificates
- **Missing:** OCR for scans and photos, automatic document classification, cross-document search, correction history view, medication and history extraction

## 03. Biomarker & Longitudinal Intelligence — Partial

Built: observations, history, catalog learned by the research agent, units validated against UCUM, reference range captured from the report.

- CBC, glucose, HbA1c, lipids, liver, kidney, vitamins, hormones, others
- **Missing:** trend comparison charts, learned lab-specific ranges, unit normalisation, change detection across tests and dates, measurement-frequency and missing-data detection, source confidence display

## 04. Multimodal Body Observation Intelligence — Not started

- Skin and appearance documentation, wound-healing timeline, bruising and swelling
- Posture, gait and movement comparison
- Image-quality assessment, body-region mapping, photo comparison over time
- **Constraint:** non-diagnostic observations only. The camera cannot replace imaging or laboratory testing. Separate consent, on-device processing first, and a deletion path.

## 05. Connected Diagnostics Intelligence — Not started

- Apple HealthKit; smartwatch and activity data
- Blood pressure monitors, glucose meters, pulse oximeters, ECG-capable devices, scales, thermometers
- Device history, calibration and sync status
- **Constraint:** lab-measured and device-measured values are stored and shown as different source types.

## 06. Medical Imaging Intelligence — Not started

- X-ray, CT, MRI and ultrasound study management
- DICOM / DICOMweb and PACS or provider integration, OHIF viewer
- Linking radiology reports to scans; study comparison and metadata
- Future: validated imaging AI, pathology and digital pathology
- **Constraint:** separate domain with its own privacy, clinical and regulatory review before any AI inference.

## 07. CORTEX Personal Health AI — Partial

Built: LangGraph workflow, hybrid retrieval over approved records, deterministic lab facts, safety policy, optional Ollama explanation, citations, abstention.

- **Missing:** history summaries, report-to-report comparison, missing-information detection, doctor consultation summaries, conflicting-evidence explanation, MCP-client tool use

## 08. Agentic Health Research Intelligence — Partial

Built: allowlisted research agent (LOINC, UCUM, MedlinePlus, PubMed), review queue, approved literature cited as `[R#]`.

- **Missing:** guideline sources, study comparison, systematic-review summaries, evidence-quality grading, publication-date and emerging-finding tracking, contradiction detection, cited research reports, saved investigations

## 09. Lifestyle & Symptom Intelligence — Not started

- Symptom journal; nutrition and food diary; sleep; exercise and activity; stress and wellbeing
- Medication and supplement records
- Contextual correlations across available data, labelled as correlation not cause
- Personal goals and routines

## 10. Environmental Health Intelligence — Not started

- AQI, pollution, weather and temperature
- Location-based context and historical exposure
- Optional correlation with symptoms and activity; environmental alerts
- **Constraint:** coarse location only, separate consent, no precise location stored by default.

## 11. Care Navigation & Coordination — Not started

- Doctor visit preparation and question lists
- Appointment scheduling and reminders; follow-up tracking
- Medical history summaries; shareable summaries
- Provider and hospital discovery
- Family and caregiver access with scoped permissions
- **Constraint:** external actions require explicit user confirmation and are audited.

## 12. Personal Data Governance — Partial

Built: purpose-based consent, audit events, scoped short-lived MCP tokens.

- **Missing:** access-history screen, export, account and data deletion (including embeddings and objects), family sharing controls, model and agent activity history, retention controls, provenance viewer
