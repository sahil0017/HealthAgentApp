export interface Profile { id: string; display_name: string }
export interface Consent { purpose: string; granted: boolean }

export interface AttentionItem {
  kind: string; severity: "action" | "info"; title: string; detail: string; ref_type?: string; ref_id?: string;
}
export interface GoalView {
  id: string; biomarker_code: string; title: string; target_min: number | null; target_max: number | null;
  unit: string; status: string;
  progress: { status: string; trend: string; latest_value: number | null; latest_unit: string | null; latest_on: string | null };
}
export interface FollowUp {
  id: string; kind: string; title: string; notes: string; due_on: string | null; status: string; completed_at: string | null;
}
export interface Overview {
  profile: { id: string; display_name: string; age: number | null; sex: string | null; blood_group: string | null; height_cm: number | null };
  summary: { documents: number; observations: number; biomarkers_tracked: number; active_goals: number; open_follow_ups: number; history_entries: number };
  attention: AttentionItem[];
  recent_changes: { biomarker_code: string; name: string; value: number; unit: string; observed_on: string; change: number | null; change_percent: number | null }[];
  goals: GoalView[];
  next_follow_up: FollowUp | null;
  suggested_follow_ups: { kind: string; title: string; reason: string }[];
  reminders: FollowUp[];
  completeness: { score: number; items: { key: string; label: string; done: boolean; detail: string }[] };
}
export interface TimelineItem { kind: string; id: string; title: string; detail: string; occurred_at: string }

export interface Biomarker { name: string; value: number; unit: string; observed_at?: string | null; reference_range: string; biomarker_code?: string; candidate_id?: string; evidence?: string; page?: number }
export interface MedicalDocument {
  id: string; file_name: string; status: string; document_type: string; document_version: number;
  extraction?: { biomarkers: Biomarker[]; text_preview: string; warnings: string[] } | null;
}
export interface SearchHit { document_id: string; file_name: string; document_type: string; document_version: number; status: string; page: number; snippet: string; match: string }

export interface RangeInfo { low: number | null; high: number | null; unit: string }
export interface TrendPoint {
  observation_id: string; observed_at: string; value: number; unit: string; flag: string; range: RangeInfo | null;
  verification_status: string; source_confidence: string;
}
export interface TrendResult { direction: string; points: number; span_days: number }
export interface BiomarkerSummary {
  biomarker_code: string; display_name: string; category: string; latest: TrendPoint | null;
  change: number | null; unit: string; trend: TrendResult; verified_readings: number; unverified_count: number;
}
export interface BiomarkerTrend { biomarker_code: string; display_name: string; canonical_unit: string; points: TrendPoint[]; trend: TrendResult; unverified_count: number }
export interface PanelCoverage { category: string; label: string; biomarkers: string[]; days_since_last: number | null; interval_days: number; status: string }

export interface AnswerSource { type: string; description: string; identifier?: string; url?: string; verification_status?: string }
export interface Answer {
  id?: string; question?: string; answer: string; sources: AnswerSource[]; limitations: string[];
  urgent?: boolean; triage?: string; confidence?: string; uncertainty?: string; suggested_questions?: string[];
  feedback?: string | null; created_at?: string;
}
export interface AccessEvent { id: number; action: string; resource_type: string; resource_id: string | null; created_at: string }

export interface Symptom { id: string; name: string; severity: number; started_at: string; ended_at: string | null; notes?: string; active: boolean }
export interface LifestyleCategory { code: string; name: string; unit: string; min: number; max: number }
export interface LifestyleLog { id: string; category: string; value: number; unit: string; label?: string; notes?: string; logged_on: string }
export interface LifestyleSummary { days: number; note: string; categories: { category: string; name: string; unit: string; days_logged: number; average_per_day: number | null; latest: { date: string; value: number } | null; daily: { date: string; value: number }[] }[] }
export interface LifestyleCorrelations { days: number; lag: number; note: string; correlations: { category: string; name: string; status: "ok" | "insufficient_data"; r?: number; days: number; strength?: string; description: string }[] }
