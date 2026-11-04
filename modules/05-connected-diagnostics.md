# 05 Connected Diagnostics — 🟢 shipped

## Scope
- HealthKit, wearables, device history, lab vs device distinction

## Code
- `backend/internal/domain/device/` — 14 metrics, unit conversion, plausibility bounds, typical ranges, 7/30-day summaries and trend
- `backend/internal/modules/devices/` — devices, batch sync, readings, daily series, summary, lab comparison
- `backend/internal/store/migrations/016_connected_devices.sql` — `devices`, `device_readings`, `device_syncs`
- `app/lib/features/health.dart` — phone health data, summaries, lab-vs-device view
- Tests: `domain/device`, `modules/devices`, `api/devices_flow_test.go`, `e2e/test_devices.py`, smoke step in `scripts/ci-smoke.py`

## Done
- [x] Connect a device (needs `health_data_integration` consent; idempotent per source and external id; max 50 per profile)
- [x] Batch sync of up to 500 readings with per-reading validation, unit normalisation and de-duplication
- [x] Device readings kept in their own table and always labelled `measurement_kind: device`, never mixed into lab observations
- [x] Daily series, 7/30-day means, trend and typical adult range with a non-diagnostic note
- [x] Lab vs device comparison for metrics with a lab equivalent (blood glucose); units that cannot be converted are excluded
- [x] Disconnect, reconnect, remove device (deletes its readings); sync history
- [x] Audit events carry counts only, never values; export includes devices; profile delete cascades
- [x] iOS: HealthKit authorisation, 30-day read, batched upload, summaries, comparison

- [x] Device syncs appear in the command-centre timeline (`kind=device`, counts only, never values)

## Not done
- [ ] iOS HealthKit code is compiled by CI only and not yet run on a device
- [ ] Bluetooth and vendor cloud APIs (Fitbit, Oura, Garmin); only Apple Health is wired
- [ ] Background and automatic sync; sync is manual
- [ ] Sleep and blood-pressure correlation reading in HealthKit
- [ ] Goals and AI retrieval stay lab-only by design: device values are unverified, so they are not mixed into verified-only goals or AI answers
