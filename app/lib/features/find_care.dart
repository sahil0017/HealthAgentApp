import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/hardware.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'nearby.dart';
import 'places.dart';

const _kinds = <(String, String, IconData)>[
  ('hospital', 'Hospitals', Icons.local_hospital_outlined),
  ('doctor', 'Doctors', Icons.medical_services_outlined),
  ('clinic', 'Clinics', Icons.healing_outlined),
  ('pharmacy', 'Pharmacies', Icons.local_pharmacy_outlined),
  ('lab', 'Diagnostic labs', Icons.biotech_outlined),
];

const _reachKm = 15.0;

typedef _Key = (double, double, String);

/// Everything of one kind near the user, nearest first. Hospitals reuse the ranked provider.
final _careProvider = FutureProvider.autoDispose.family<List<Json>, _Key>((
  ref,
  k,
) async {
  final List<Json> list;
  if (k.$3 == 'hospital') {
    list = [
      ...await ref.watch(hospitalsProvider(Area(lat: k.$1, lng: k.$2)).future),
    ];
  } else {
    final res = await ref
        .read(apiProvider)
        .map(
          '/nearby/places?kind=${k.$3}&lat=${k.$1}&lng=${k.$2}&radius_km=10',
        );
    list = ((res['places'] as List?) ?? [])
        .whereType<Map>()
        .map((e) => e.cast<String, dynamic>())
        .toList();
  }
  return list;
});

double _km(Json h) => (numOf(h['distance_km']) ?? 0).toDouble();

/// Rough road time in a city; it is only a guide and the map gives the real figure.
String _drive(double km) => '~${(km * 2.5).ceil().clamp(2, 999)} min drive';

/// One place to find care: emergency first, then how much care is within reach, then the best matches.
class FindCareScreen extends ConsumerWidget {
  const FindCareScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) =>
      NeedsArea('Care Locator', (area) => _FindCare(area));
}

class _FindCare extends ConsumerWidget {
  const _FindCare(this.area);
  final Area area;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final hospitals = ref.watch(
      _careProvider((area.lat, area.lng, 'hospital')),
    );
    final ranked = hospitals.asData?.value ?? const <Json>[];
    final nearest = nearestEmergency(ranked);
    final number = emergencyNumber(area);
    return ListView(
      padding: const EdgeInsets.all(14),
      children: [
        _Where(area),
        const SizedBox(height: 10),
        _Emergency(
          number: number,
          nearest: nearest,
          loading: hospitals.isLoading,
        ),
        const SectionTitle('Care within reach'),
        _Reach(area),
        if (ranked.isNotEmpty) ...[
          const SectionTitle('Hospitals by distance'),
          _Histogram(ranked),
          const SectionTitle('Best matches for you'),
          for (final h in ranked.take(5)) _HospitalTile(h),
        ],
        if (hospitals.hasError)
          SCard(
            child: Row(
              children: [
                const Expanded(
                  child: Text(
                    'Map data is busy right now.',
                    style: TextStyle(fontSize: 12),
                  ),
                ),
                TextButton(
                  onPressed: () => ref.invalidate(_careProvider),
                  child: const Text('Try again'),
                ),
              ],
            ),
          ),
        SCard(
          onTap: () => Navigator.push(
            context,
            MaterialPageRoute<void>(builder: (_) => const HospitalsMapScreen()),
          ),
          child: const Row(
            children: [
              Icon(Icons.map_outlined, color: Pal.pri, size: 20),
              SizedBox(width: 12),
              Expanded(child: Text('Map view')),
              Icon(Icons.chevron_right, color: Pal.mut),
            ],
          ),
        ),
        const Padding(
          padding: EdgeInsets.all(6),
          child: Text(
            'Listings and distances come from OpenStreetMap. Call ahead to confirm services and hours.',
            style: TextStyle(fontSize: 10, color: Pal.mut),
          ),
        ),
      ],
    );
  }
}

class _Where extends ConsumerStatefulWidget {
  const _Where(this.area);
  final Area area;
  @override
  ConsumerState<_Where> createState() => _WhereState();
}

class _WhereState extends ConsumerState<_Where> {
  bool _busy = false;

  @override
  Widget build(BuildContext context) {
    final label = widget.area.label;
    return Row(
      children: [
        const Icon(Icons.place, size: 16, color: Pal.pri),
        const SizedBox(width: 6),
        Expanded(
          child: Text(
            label.isEmpty ? 'Near your current location' : label,
            style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
          ),
        ),
        _busy
            ? const SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : TextButton.icon(
                onPressed: () async {
                  setState(() => _busy = true);
                  await ref.read(areaProvider.notifier).detect();
                  if (mounted) setState(() => _busy = false);
                },
                icon: const Icon(Icons.my_location, size: 14),
                label: const Text('Update'),
              ),
      ],
    );
  }
}

class _Emergency extends ConsumerWidget {
  const _Emergency({
    required this.number,
    required this.nearest,
    required this.loading,
  });
  final String number;
  final Json? nearest;
  final bool loading;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final km = nearest == null ? null : _km(nearest!);
    return SCard(
      color: Pal.badBg,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.emergency, color: Pal.bad, size: 18),
              const SizedBox(width: 8),
              const Text(
                'Emergency',
                style: TextStyle(
                  fontSize: 14,
                  fontWeight: FontWeight.w700,
                  color: Pal.bad,
                ),
              ),
              const Spacer(),
              if (km != null)
                Pill(
                  '${fmtNum(km)} km · ${_drive(km)}',
                  fg: Pal.bad,
                  bg: Pal.card,
                ),
            ],
          ),
          const SizedBox(height: 10),
          if (nearest != null)
            InkWell(
              onTap: () => Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) => HospitalScreen(nearest!),
                ),
              ),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 4),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '${nearest!['name']}',
                            style: const TextStyle(
                              fontSize: 13,
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                          Text(
                            nearest!['emergency'] == true
                                ? 'Emergency care listed'
                                : 'Nearest hospital',
                            style: const TextStyle(
                              fontSize: 11,
                              color: Pal.mut,
                            ),
                          ),
                        ],
                      ),
                    ),
                    const Icon(Icons.chevron_right, color: Pal.mut),
                  ],
                ),
              ),
            )
          else
            Text(
              loading
                  ? 'Finding the nearest hospital…'
                  : 'No hospital found in range.',
              style: const TextStyle(fontSize: 12, color: Pal.mut),
            ),
          const SizedBox(height: 10),
          Row(
            children: [
              Expanded(
                flex: 3,
                child: FilledButton.icon(
                  style: FilledButton.styleFrom(
                    backgroundColor: Pal.bad,
                    foregroundColor: Colors.black,
                  ),
                  onPressed: () => dial(context, number),
                  icon: const Icon(Icons.call, size: 18),
                  label: Text('Call $number'),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                flex: 2,
                child: OutlinedButton.icon(
                  onPressed: nearest == null
                      ? null
                      : () => openDirections(context, nearest!),
                  icon: const Icon(Icons.directions, size: 18),
                  label: const Text('Go'),
                ),
              ),
            ],
          ),
          TextButton.icon(
            onPressed: () => shareMyLocation(context, ref),
            icon: const Icon(Icons.share_location, size: 16),
            label: const Text('Send my location to someone'),
          ),
        ],
      ),
    );
  }
}

/// Count and nearest distance for each kind of care, as a bar that fills the closer it is.
class _Reach extends ConsumerWidget {
  const _Reach(this.area);
  final Area area;

  @override
  Widget build(BuildContext context, WidgetRef ref) => SCard(
    child: Column(
      children: [
        for (final k in _kinds)
          _ReachRow(
            k.$2,
            k.$3,
            ref.watch(_careProvider((area.lat, area.lng, k.$1))),
            () => Navigator.push(
              context,
              MaterialPageRoute<void>(
                builder: (_) => k.$1 == 'hospital'
                    ? const HospitalsMapScreen()
                    : DoctorsScreen(initialKind: k.$1),
              ),
            ),
          ),
      ],
    ),
  );
}

class _ReachRow extends StatelessWidget {
  const _ReachRow(this.label, this.icon, this.data, this.onTap);
  final String label;
  final IconData icon;
  final AsyncValue<List<Json>> data;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final list = data.asData?.value;
    final nearest = list == null || list.isEmpty
        ? null
        : list.map(_km).reduce((a, b) => a < b ? a : b);
    final fill = nearest == null
        ? 0.0
        : (1 - nearest / _reachKm).clamp(0.06, 1.0);
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Row(
          children: [
            Icon(icon, size: 18, color: Pal.pri),
            const SizedBox(width: 10),
            SizedBox(
              width: 104,
              child: Text(label, style: const TextStyle(fontSize: 12)),
            ),
            Expanded(
              child: ClipRRect(
                borderRadius: BorderRadius.circular(4),
                child: LinearProgressIndicator(
                  value: data.isLoading ? null : fill,
                  minHeight: 7,
                  backgroundColor: Pal.line,
                  color: nearest == null
                      ? Pal.mut
                      : nearest <= 3
                      ? Pal.ok
                      : nearest <= 8
                      ? Pal.warn
                      : Pal.bad,
                ),
              ),
            ),
            SizedBox(
              width: 86,
              child: Text(
                list == null
                    ? ''
                    : nearest == null
                    ? 'none found'
                    : '${list.length} · ${fmtNum(nearest)} km',
                textAlign: TextAlign.right,
                style: const TextStyle(
                  fontSize: 11,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// How many hospitals sit in each distance band.
class _Histogram extends StatelessWidget {
  const _Histogram(this.hospitals);
  final List<Json> hospitals;

  @override
  Widget build(BuildContext context) {
    const bands = [(0.0, 2.0), (2.0, 5.0), (5.0, 10.0), (10.0, _reachKm)];
    final counts = [
      for (final b in bands)
        hospitals.where((h) => _km(h) >= b.$1 && _km(h) < b.$2).length,
    ];
    final most = counts.fold<int>(1, (a, b) => a > b ? a : b);
    return SCard(
      child: SizedBox(
        height: 96,
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            for (var i = 0; i < bands.length; i++)
              Expanded(
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: [
                    Text(
                      '${counts[i]}',
                      style: const TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Container(
                      height: 8 + 44.0 * counts[i] / most,
                      margin: const EdgeInsets.symmetric(horizontal: 10),
                      decoration: BoxDecoration(
                        color: i == 0 ? Pal.ok : Pal.pri.withValues(alpha: .55),
                        borderRadius: BorderRadius.circular(4),
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      '${fmtNum(bands[i].$1)}–${fmtNum(bands[i].$2)} km',
                      style: const TextStyle(fontSize: 9.5, color: Pal.mut),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _HospitalTile extends StatelessWidget {
  const _HospitalTile(this.h);
  final Json h;

  @override
  Widget build(BuildContext context) => SCard(
    onTap: () => Navigator.push(
      context,
      MaterialPageRoute<void>(builder: (_) => HospitalScreen(h)),
    ),
    child: Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '${h['name']}',
                style: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 2),
              Text(
                [
                  '${fmtNum(_km(h))} km',
                  if ('${h['speciality'] ?? ''}'.isNotEmpty)
                    '${h['speciality']}',
                ].join(' · '),
                style: const TextStyle(fontSize: 11, color: Pal.mut),
              ),
              if (h['reason'] != null || h['emergency'] == true)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Wrap(
                    spacing: 6,
                    children: [
                      if (h['reason'] != null) Pill('${h['reason']}'),
                      if (h['emergency'] == true)
                        const Pill('Emergency', fg: Pal.bad, bg: Pal.badBg),
                    ],
                  ),
                ),
            ],
          ),
        ),
        const Icon(Icons.chevron_right, color: Pal.mut),
      ],
    ),
  );
}

/// A single hospital: key facts, then the actions people actually need.
class HospitalScreen extends StatelessWidget {
  const HospitalScreen(this.h, {super.key});
  final Json h;

  @override
  Widget build(BuildContext context) {
    final km = _km(h);
    final phone = '${h['phone'] ?? ''}';
    Widget fact(IconData i, String label, String value) => Padding(
      padding: const EdgeInsets.symmetric(vertical: 7),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(i, size: 17, color: Pal.pri),
          const SizedBox(width: 12),
          SizedBox(
            width: 78,
            child: Text(
              label,
              style: const TextStyle(fontSize: 11, color: Pal.mut),
            ),
          ),
          Expanded(child: Text(value, style: const TextStyle(fontSize: 12.5))),
        ],
      ),
    );
    return Scaffold(
      appBar: AppBar(title: const Text('Hospital')),
      body: ListView(
        padding: const EdgeInsets.all(14),
        children: [
          Text(
            '${h['name']}',
            style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 6,
            children: [
              if (h['emergency'] == true)
                const Pill('Emergency care', fg: Pal.bad, bg: Pal.badBg),
              if (h['reason'] != null) Pill('${h['reason']}'),
            ],
          ),
          const SizedBox(height: 12),
          SCard(
            child: Column(
              children: [
                fact(
                  Icons.near_me_outlined,
                  'Distance',
                  '${fmtNum(km)} km · ${_drive(km)}',
                ),
                fact(
                  Icons.local_hospital_outlined,
                  'Speciality',
                  '${h['speciality'] ?? 'Not listed'}',
                ),
                fact(
                  Icons.place_outlined,
                  'Address',
                  '${h['address'] ?? 'Not listed'}',
                ),
                fact(
                  Icons.call_outlined,
                  'Phone',
                  phone.isEmpty ? 'Not listed' : phone,
                ),
              ],
            ),
          ),
          const SizedBox(height: 4),
          Row(
            children: [
              Expanded(
                child: FilledButton.icon(
                  onPressed: () => openDirections(context, h),
                  icon: const Icon(Icons.directions, size: 18),
                  label: const Text('Directions'),
                ),
              ),
              if (phone.isNotEmpty) ...[
                const SizedBox(width: 8),
                Expanded(
                  child: OutlinedButton.icon(
                    onPressed: () => dial(context, phone),
                    icon: const Icon(Icons.call, size: 18),
                    label: const Text('Call'),
                  ),
                ),
              ],
            ],
          ),
          TextButton.icon(
            onPressed: () {
              Clipboard.setData(
                ClipboardData(
                  text:
                      '${h['name']}, ${h['address'] ?? ''} https://www.google.com/maps?q=${h['lat']},${h['lng']}',
                ),
              );
              toast(context, 'Copied');
            },
            icon: const Icon(Icons.copy, size: 16),
            label: const Text('Copy address and map link'),
          ),
          const Padding(
            padding: EdgeInsets.all(6),
            child: Text(
              'Listed by OpenStreetMap contributors. Call ahead to confirm services and hours.',
              style: TextStyle(fontSize: 10, color: Pal.mut),
            ),
          ),
        ],
      ),
    );
  }
}
