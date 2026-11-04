import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:latlong2/latlong.dart';
import 'package:url_launcher/url_launcher.dart';

import '../core/hardware.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'nearby.dart';

const _note = TextStyle(fontSize: 10.5, color: Pal.mut);
const _bold = TextStyle(fontSize: 12, fontWeight: FontWeight.w600);

void _open(BuildContext c, Widget w) =>
    Navigator.push(c, MaterialPageRoute<void>(builder: (_) => w));

Future<void> _launch(BuildContext context, Uri uri) async {
  var ok = false;
  try {
    ok = await launchUrl(uri, mode: LaunchMode.externalApplication);
  } catch (_) {}
  if (!ok && context.mounted)
    toast(context, 'Could not open that on this phone.');
}

/// Asks for the user's area when it is not known yet, then shows [builder].
class _NeedsArea extends ConsumerStatefulWidget {
  const _NeedsArea(this.title, this.builder);
  final String title;
  final Widget Function(Area area) builder;
  @override
  ConsumerState<_NeedsArea> createState() => _NeedsAreaState();
}

class _NeedsAreaState extends ConsumerState<_NeedsArea> {
  bool _busy = false;
  bool _denied = false;

  Future<void> _detect() async {
    setState(() => _busy = true);
    final ok = await ref.read(areaProvider.notifier).detect();
    if (mounted) {
      setState(() {
        _busy = false;
        _denied = !ok;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final area = ref.watch(areaProvider);
    return Scaffold(
      appBar: AppBar(title: Text(widget.title)),
      body: area != null
          ? widget.builder(area)
          : Empty(
              Icons.place_outlined,
              'Use my location?',
              body: _denied
                  ? 'Location is off or unavailable. You can allow it in your phone settings.'
                  : 'We use your city, not your exact position, to show care near you.',
              action: FilledButton(
                onPressed: _busy ? null : _detect,
                child: Text(_busy ? 'Locating…' : 'Allow approximate location'),
              ),
            ),
    );
  }
}

// ------------------------------------------------------------ Health news

class HealthNewsScreen extends ConsumerWidget {
  const HealthNewsScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final country = ref.watch(areaProvider)?.country ?? '';
    final alerts = ref.watch(alertsProvider(country));
    return Scaffold(
      appBar: AppBar(title: const Text('Health news')),
      body: AsyncBody<List<Json>>(
        alerts,
        onRetry: () => ref.invalidate(alertsProvider(country)),
        builder: (list) {
          if (list.isEmpty) {
            return const Empty(Icons.newspaper, 'Nothing new right now');
          }
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [
              const Text(
                'From the WHO Disease Outbreak News feed. Each item shows its source and date.',
                style: _note,
              ),
              const SizedBox(height: 8),
              for (final a in list)
                SCard(
                  onTap: () => _open(context, AlertScreen(a)),
                  child: Row(
                    children: [
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('${a['title']}', style: _bold),
                            Text(
                              '${a['source']} · ${'${a['published_at']}'.substring(0, 10)}',
                              style: _note,
                            ),
                          ],
                        ),
                      ),
                      if (a['in_country'] == true) const Pill('Your country'),
                    ],
                  ),
                ),
              if (country.isEmpty)
                const Text(
                  'Turn on approximate location to see your country first.',
                  style: _note,
                ),
            ],
          );
        },
      ),
    );
  }
}

// ----------------------------------------------------------------- Doctors

typedef _PlaceKey = (double, double, String);

final _placesProvider = FutureProvider.autoDispose.family<Json, _PlaceKey>(
  (ref, k) => ref
      .read(apiProvider)
      .map('/nearby/places?kind=${k.$3}&lat=${k.$1}&lng=${k.$2}&radius_km=10'),
);

const _kinds = [
  ('doctor', 'Doctors', Icons.medical_services_outlined),
  ('clinic', 'Clinics', Icons.local_hospital_outlined),
  ('pharmacy', 'Pharmacies', Icons.local_pharmacy_outlined),
  ('lab', 'Labs', Icons.biotech_outlined),
];

/// Doctors, clinics, pharmacies and labs around the phone's approximate location.
class DoctorsScreen extends ConsumerStatefulWidget {
  const DoctorsScreen({super.key});
  @override
  ConsumerState<DoctorsScreen> createState() => _DoctorsState();
}

class _DoctorsState extends ConsumerState<DoctorsScreen> {
  String _kind = 'doctor';
  bool _map = false;

  @override
  Widget build(BuildContext context) => _NeedsArea('Care near you', (area) {
    final res = ref.watch(_placesProvider((area.lat, area.lng, _kind)));
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(14, 8, 14, 0),
          child: Row(
            children: [
              Expanded(
                child: Wrap(
                  spacing: 6,
                  children: [
                    for (final k in _kinds)
                      ChoiceChip(
                        label: Text(k.$2, style: const TextStyle(fontSize: 11)),
                        selected: _kind == k.$1,
                        onSelected: (_) => setState(() => _kind = k.$1),
                      ),
                  ],
                ),
              ),
              IconButton(
                tooltip: _map ? 'List' : 'Map',
                icon: Icon(_map ? Icons.list : Icons.map_outlined),
                onPressed: () => setState(() => _map = !_map),
              ),
            ],
          ),
        ),
        Expanded(
          child: AsyncBody<Json>(
            res,
            onRetry: () =>
                ref.invalidate(_placesProvider((area.lat, area.lng, _kind))),
            builder: (r) {
              final list = ((r['places'] as List?) ?? []).cast<Json>();
              if (_map) {
                return _HospitalMap(
                  area,
                  list,
                  onTap: (p) => _open(context, PlaceScreen(p)),
                );
              }
              return ListView(
                padding: const EdgeInsets.all(14),
                children: [
                  SCard(
                    color: Pal.priBg,
                    child: Text(
                      '${r['note'] ?? ''}',
                      style: const TextStyle(fontSize: 11, height: 1.4),
                    ),
                  ),
                  if (list.isEmpty)
                    const Padding(
                      padding: EdgeInsets.all(24),
                      child: Center(
                        child: Text(
                          'Nothing mapped within 10 km.',
                          style: _note,
                        ),
                      ),
                    ),
                  for (final p in list)
                    SCard(
                      onTap: () => _open(context, PlaceScreen(p)),
                      child: Row(
                        children: [
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text('${p['name']}', style: _bold),
                                Text(
                                  [
                                    if ('${p['speciality'] ?? ''}'.isNotEmpty)
                                      '${p['speciality']}',
                                    '${fmtNum(numOf(p['distance_km']))} km',
                                  ].join(' · '),
                                  style: _note,
                                ),
                              ],
                            ),
                          ),
                          const Icon(Icons.chevron_right, color: Pal.mut),
                        ],
                      ),
                    ),
                ],
              );
            },
          ),
        ),
      ],
    );
  });
}

class PlaceScreen extends StatelessWidget {
  const PlaceScreen(this.p, {super.key});
  final Json p;

  Widget _row(IconData i, String? text) => text == null || text.isEmpty
      ? const SizedBox.shrink()
      : Padding(
          padding: const EdgeInsets.symmetric(vertical: 4),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(i, size: 16, color: Pal.mut),
              const SizedBox(width: 8),
              Expanded(child: Text(text, style: const TextStyle(fontSize: 12))),
            ],
          ),
        );

  @override
  Widget build(BuildContext context) {
    final phone = p['phone'] as String?;
    final site = p['website'] as String?;
    return Scaffold(
      appBar: AppBar(title: Text('${p['name']}')),
      body: ListView(
        padding: const EdgeInsets.all(14),
        children: [
          SCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${p['name']}',
                  style: const TextStyle(
                    fontSize: 15,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 6),
                _row(Icons.work_outline, p['speciality'] as String?),
                _row(Icons.place_outlined, p['address'] as String?),
                _row(
                  Icons.near_me_outlined,
                  '${fmtNum(numOf(p['distance_km']))} km away',
                ),
                _row(Icons.schedule, p['opening_hours'] as String?),
                _row(Icons.call_outlined, phone),
              ],
            ),
          ),
          Row(
            children: [
              if (phone != null && phone.isNotEmpty)
                Expanded(
                  child: FilledButton.icon(
                    onPressed: () => _launch(
                      context,
                      Uri(scheme: 'tel', path: phone.replaceAll(' ', '')),
                    ),
                    icon: const Icon(Icons.call),
                    label: const Text('Call'),
                  ),
                ),
              if (phone != null && phone.isNotEmpty) const SizedBox(width: 8),
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: () => _directions(context, p),
                  icon: const Icon(Icons.directions),
                  label: const Text('Directions'),
                ),
              ),
            ],
          ),
          if (site != null && site.startsWith('http'))
            TextButton(
              onPressed: () => _launch(context, Uri.parse(site)),
              child: const Text('Website'),
            ),
          const Padding(
            padding: EdgeInsets.only(top: 10),
            child: Text(
              'Listed by OpenStreetMap contributors. Not verified against a medical registry and not a recommendation. Confirm hours and registration by phone.',
              style: _note,
            ),
          ),
        ],
      ),
    );
  }
}

// -------------------------------------------------------------------- Maps

class _HospitalMap extends StatelessWidget {
  const _HospitalMap(this.area, this.hospitals, {this.onTap});
  final Area area;
  final List<Json> hospitals;
  final void Function(Json)? onTap;
  @override
  Widget build(BuildContext context) => FlutterMap(
    options: MapOptions(
      initialCenter: LatLng(area.lat, area.lng),
      initialZoom: 12.5,
    ),
    children: [
      TileLayer(
        urlTemplate: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
        userAgentPackageName: 'app.sehat',
      ),
      MarkerLayer(
        markers: [
          Marker(
            point: LatLng(area.lat, area.lng),
            child: const Icon(Icons.my_location, color: Pal.pri),
          ),
          for (final h in hospitals)
            if (numOf(h['lat']) != null && numOf(h['lng']) != null)
              Marker(
                point: LatLng(
                  numOf(h['lat'])!.toDouble(),
                  numOf(h['lng'])!.toDouble(),
                ),
                width: 36,
                height: 36,
                child: GestureDetector(
                  onTap: () => onTap?.call(h),
                  child: Icon(
                    Icons.local_hospital,
                    color: h['emergency'] == true ? Pal.bad : Pal.warn,
                  ),
                ),
              ),
        ],
      ),
      const RichAttributionWidget(
        attributions: [TextSourceAttribution('© OpenStreetMap contributors')],
      ),
    ],
  );
}

Future<void> _directions(BuildContext context, Json h) => _launch(
  context,
  Uri.parse(
    'https://www.openstreetmap.org/?mlat=${h['lat']}&mlon=${h['lng']}#map=17/${h['lat']}/${h['lng']}',
  ),
);

class HospitalsMapScreen extends ConsumerStatefulWidget {
  const HospitalsMapScreen({super.key});
  @override
  ConsumerState<HospitalsMapScreen> createState() => _HospitalsMapState();
}

class _HospitalsMapState extends ConsumerState<HospitalsMapScreen> {
  bool _emergencyOnly = false;

  @override
  Widget build(BuildContext context) => _NeedsArea('Hospitals', (area) {
    final h = ref.watch(hospitalsProvider(area));
    return AsyncBody<List<Json>>(
      h,
      onRetry: () => ref.invalidate(hospitalsProvider(area)),
      builder: (all) {
        final list = _emergencyOnly
            ? all.where((x) => x['emergency'] == true).toList()
            : all;
        return Column(
          children: [
            SizedBox(
              height: 240,
              child: _HospitalMap(
                area,
                list,
                onTap: (x) => _directions(context, x),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 14),
              child: Row(
                children: [
                  FilterChip(
                    label: const Text('Emergency'),
                    selected: _emergencyOnly,
                    onSelected: (v) => setState(() => _emergencyOnly = v),
                  ),
                  const Spacer(),
                  Text('${list.length} found', style: _note),
                ],
              ),
            ),
            Expanded(
              child: list.isEmpty
                  ? const Empty(
                      Icons.local_hospital_outlined,
                      'None found nearby',
                    )
                  : ListView(
                      padding: const EdgeInsets.all(14),
                      children: [
                        for (final x in list)
                          SCard(
                            onTap: () => _directions(context, x),
                            child: Row(
                              children: [
                                Expanded(
                                  child: Column(
                                    crossAxisAlignment:
                                        CrossAxisAlignment.start,
                                    children: [
                                      Text('${x['name']}', style: _bold),
                                      Text(
                                        '${fmtNum(numOf(x['distance_km']))} km${x['address'] == null ? '' : ' · ${x['address']}'}',
                                        style: _note,
                                      ),
                                    ],
                                  ),
                                ),
                                if (x['emergency'] == true)
                                  const Pill(
                                    'Emergency',
                                    fg: Pal.bad,
                                    bg: Pal.badBg,
                                  ),
                              ],
                            ),
                          ),
                      ],
                    ),
            ),
          ],
        );
      },
    );
  });
}

/// One place for alerts and care in the user's area, switched by chips.
class AreaMapScreen extends ConsumerStatefulWidget {
  const AreaMapScreen({super.key});
  @override
  ConsumerState<AreaMapScreen> createState() => _AreaMapState();
}

class _AreaMapState extends ConsumerState<AreaMapScreen> {
  String _layer = 'Hospitals';

  @override
  Widget build(BuildContext context) => _NeedsArea('Your area', (area) {
    final h = ref.watch(hospitalsProvider(area));
    final alerts = ref.watch(alertsProvider(area.country ?? ''));
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(14, 8, 14, 0),
          child: Row(
            children: [
              const Icon(Icons.place, size: 16, color: Pal.pri),
              const SizedBox(width: 4),
              Text(area.label.isEmpty ? 'Your area' : area.label, style: _bold),
              const Spacer(),
              for (final l in const ['Hospitals', 'Alerts'])
                Padding(
                  padding: const EdgeInsets.only(left: 6),
                  child: ChoiceChip(
                    label: Text(l),
                    selected: _layer == l,
                    onSelected: (_) => setState(() => _layer = l),
                  ),
                ),
            ],
          ),
        ),
        Expanded(
          child: _layer == 'Hospitals'
              ? AsyncBody<List<Json>>(
                  h,
                  onRetry: () => ref.invalidate(hospitalsProvider(area)),
                  builder: (list) => _HospitalMap(
                    area,
                    list,
                    onTap: (x) => _directions(context, x),
                  ),
                )
              : AsyncBody<List<Json>>(
                  alerts,
                  onRetry: () =>
                      ref.invalidate(alertsProvider(area.country ?? '')),
                  builder: (list) => ListView(
                    padding: const EdgeInsets.all(14),
                    children: [
                      const Text(
                        'Alerts are published per country, so they are listed rather than pinned on the map.',
                        style: _note,
                      ),
                      const SizedBox(height: 8),
                      for (final a in list.take(15))
                        SCard(
                          onTap: () => _open(context, AlertScreen(a)),
                          child: Text('${a['title']}', style: _bold),
                        ),
                    ],
                  ),
                ),
        ),
      ],
    );
  });
}

// --------------------------------------------------------------- Emergency

const _ambulance = {
  'IN': '112',
  'US': '911',
  'CA': '911',
  'GB': '999',
  'AU': '000',
  'NZ': '111',
};

class EmergencyScreen extends ConsumerWidget {
  const EmergencyScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final area = ref.watch(areaProvider);
    final number = _ambulance[area?.countryCode] ?? '112';
    final profile = ref.watch(profileMapProvider(''));
    final history = ref.watch(profileListProvider('/history'));
    final hospitals = area == null
        ? const AsyncValue<List<Json>>.data([])
        : ref.watch(hospitalsProvider(area));
    final ers = (hospitals.asData?.value ?? [])
        .where((h) => h['emergency'] == true)
        .toList();
    return Scaffold(
      appBar: AppBar(title: const Text('Emergency')),
      body: ListView(
        padding: const EdgeInsets.all(14),
        children: [
          FilledButton.icon(
            style: FilledButton.styleFrom(
              backgroundColor: Pal.bad,
              foregroundColor: Colors.black,
              padding: const EdgeInsets.symmetric(vertical: 16),
            ),
            onPressed: () => _launch(context, Uri(scheme: 'tel', path: number)),
            icon: const Icon(Icons.call),
            label: Text(
              'Call ambulance ($number)',
              style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
            ),
          ),
          if (area?.countryCode == null ||
              !_ambulance.containsKey(area!.countryCode))
            const Padding(
              padding: EdgeInsets.only(top: 4),
              child: Text(
                '112 works on most mobile networks. Check your local emergency number.',
                style: _note,
              ),
            ),
          const SectionTitle('Nearest emergency hospital'),
          if (area == null)
            SCard(
              onTap: () => _open(context, const HospitalsMapScreen()),
              child: const Text(
                'Turn on approximate location to find the nearest one.',
                style: _note,
              ),
            )
          else if (ers.isEmpty)
            Text(
              hospitals.isLoading
                  ? 'Looking…'
                  : 'No emergency hospital found within range.',
              style: _note,
            )
          else
            SCard(
              onTap: () => _directions(context, ers.first),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '${ers.first['name']} · ${fmtNum(numOf(ers.first['distance_km']))} km',
                      style: _bold,
                    ),
                  ),
                  const Icon(Icons.directions, color: Pal.pri),
                ],
              ),
            ),
          const SectionTitle('Medical ID'),
          SCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Blood group: ${profile.asData?.value['blood_group'] ?? 'not set'}',
                  style: const TextStyle(fontSize: 12),
                ),
                const SizedBox(height: 6),
                ...() {
                  final items = (history.asData?.value ?? [])
                      .where(
                        (e) =>
                            e['status'] == 'active' &&
                            (e['kind'] == 'allergy' ||
                                e['kind'] == 'condition' ||
                                e['kind'] == 'medication'),
                      )
                      .toList();
                  if (items.isEmpty) {
                    return [
                      const Text(
                        'No allergies, conditions or medications recorded.',
                        style: _note,
                      ),
                    ];
                  }
                  return [
                    for (final e in items)
                      Text(
                        '${'${e['kind']}'.replaceAll('_', ' ')}: ${e['title']}',
                        style: const TextStyle(fontSize: 12),
                      ),
                  ];
                }(),
              ],
            ),
          ),
          if (area != null)
            OutlinedButton.icon(
              onPressed: () => _launch(
                context,
                Uri.parse(
                  'https://www.openstreetmap.org/?mlat=${area.lat}&mlon=${area.lng}#map=15/${area.lat}/${area.lng}',
                ),
              ),
              icon: const Icon(Icons.share_location),
              label: const Text('Open my area in maps'),
            ),
        ],
      ),
    );
  }
}
