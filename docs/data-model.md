# CORTEX data model

Derived from [`backend/internal/store/migrations/`](../backend/internal/store/migrations/) (001–023). Key columns only.

**Ownership rule:** `profiles` is the root of all personal health data. Child tables carry `(profile_id, owner_id)` with a composite FK to `profiles(id, owner_id)` (cascade delete), so a row can never point at a profile owned by someone else. `owner_id` is the identity string from `users.id`; it is a logical link, not a foreign key.

## 1. Identity, consent and audit

```mermaid
erDiagram
    users ||--o{ api_sessions : "has"
    users ||..o{ profiles : "owner_id (logical)"
    profiles ||--o{ consents : "grants"
    profiles ||--o{ audit_events : "logged in"
    profiles ||--o| retention_policies : "has"
    profiles ||--o{ profile_shares : "shared via"
    profiles ||--o{ agent_permissions : "allows"
    profiles ||--o{ agent_actions : "proposes"

    users { text id PK }
    api_sessions { text token_hash PK
        text user_id FK
        timestamptz expires_at }
    profiles { uuid id PK
        text owner_id
        text display_name
        date date_of_birth
        text blood_group }
    consents { uuid id PK
        uuid profile_id FK
        text purpose
        bool granted }
    retention_policies { uuid profile_id PK
        int ai_history_days }
    profile_shares { uuid id PK
        text scopes
        text code_hash }
    agent_permissions { text permission
        bool granted }
    agent_actions { uuid id PK
        text kind
        text status }
    audit_events { uuid id PK
        text action
        text resource_type }
```

## 2. Documents, extraction and observations (the core record)

```mermaid
erDiagram
    profiles ||--o{ medical_documents : "owns"
    medical_documents ||--o{ medical_documents : "supersedes"
    medical_documents ||--o{ document_extraction_runs : "processed by"
    medical_documents ||--o{ document_extraction_pages : "text of"
    medical_documents ||--o{ document_extraction_candidates : "proposes"
    medical_documents ||--o{ document_page_chunks : "searchable as"
    medical_documents |o--o{ observations : "source (SET NULL)"
    profiles ||--o{ observations : "owns"
    biomarker_catalog ||--o{ observations : "classifies"
    biomarker_catalog ||--o{ document_extraction_candidates : "matches"
    observations ||--o{ observations : "supersedes"
    observations ||--o{ observation_revisions : "history"
    observations ||--o{ record_chunks : "embedded as"

    medical_documents { uuid id PK
        uuid profile_id FK
        text object_key
        text status
        int document_version
        char sha256 }
    document_extraction_candidates { uuid id PK
        uuid document_id FK
        text biomarker_code FK
        numeric value_numeric
        text review_status }
    document_page_chunks { uuid id PK
        int page_number
        vector embedding }
    observations { uuid id PK
        uuid document_id FK
        text biomarker_code FK
        numeric value
        text unit
        int revision }
    observation_revisions { uuid id PK
        uuid observation_id FK
        jsonb snapshot }
    record_chunks { uuid id PK
        uuid observation_id FK
        vector embedding }
    biomarker_catalog { text code PK
        text display_name
        text status
        text category }
    outbox_events { uuid id PK
        text aggregate_type
        uuid aggregate_id
        text status }
```

`outbox_events` is the durable queue for document processing. It references documents by `aggregate_id` (no FK).

## 3. Learned knowledge and research

```mermaid
erDiagram
    knowledge_entries ||--o| research_sources : "approved literature"
    research_sources ||--o{ research_chunks : "embedded as"
    knowledge_entries |o--o{ reference_ranges : "evidence"
    knowledge_entries |o--o{ unit_conversions : "evidence"
    biomarker_catalog ||--o{ reference_ranges : "has"
    biomarker_catalog ||--o{ unit_conversions : "has"
    profiles ||--o{ research_investigations : "runs"
    research_investigations ||--o{ research_investigation_items : "contains"
    research_sources |o--o{ research_investigation_items : "cites"

    knowledge_entries { uuid id PK
        text kind
        jsonb payload
        text status
        text reviewed_by }
    research_tasks { uuid id PK
        text kind
        text term
        text status }
    research_sources { uuid id PK
        uuid knowledge_entry_id FK
        text identifier
        text title }
    research_chunks { uuid id PK
        uuid source_id FK
        vector embedding }
    reference_ranges { uuid id PK
        text biomarker_code FK
        numeric low
        numeric high }
    unit_conversions { uuid id PK
        text from_unit
        text to_unit
        numeric factor }
    research_investigations { uuid id PK
        uuid profile_id FK
        text question }
    research_investigation_items { uuid id PK
        uuid investigation_id FK
        uuid source_id FK }
```

`research_tasks` is a standalone queue; results land in `knowledge_entries` as `pending` until an administrator approves them.

## 4. Health tracking, devices, medications and questions

```mermaid
erDiagram
    profiles ||--o{ history_entries : "has"
    profiles ||--o{ goals : "sets"
    profiles ||--o{ follow_ups : "tracks"
    profiles ||--o{ symptom_entries : "logs"
    profiles ||--o{ lifestyle_logs : "logs"
    profiles ||--o{ health_questions : "asks"
    profiles ||--o{ devices : "connects"
    biomarker_catalog ||--o{ goals : "targets"
    devices ||--o{ device_readings : "produces"
    devices ||--o{ device_syncs : "syncs"
    profiles ||--o{ prescriptions : "has"
    prescriptions ||--o{ prescription_items : "lists"
    prescription_items ||--o{ dose_logs : "taken as"

    goals { uuid id PK
        text biomarker_code FK
        numeric target_min
        numeric target_max }
    health_questions { uuid id PK
        text question
        text answer
        jsonb sources }
    devices { uuid id PK
        text kind
        text source
        timestamptz last_synced_at }
    device_readings { uuid id PK
        uuid device_id FK
        text metric
        numeric value
        text dedupe_key }
    prescriptions { uuid id PK
        text doctor
        date prescribed_on }
    prescription_items { uuid id PK
        uuid prescription_id FK
        text name
        text dose }
    dose_logs { uuid item_id FK
        date day
        text slot }
```

`history_entries`, `follow_ups`, `symptom_entries` and `lifestyle_logs` have no links beyond the profile.
