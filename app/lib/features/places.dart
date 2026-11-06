import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_contacts/flutter_contacts.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:latlong2/latlong.dart';
import 'package:share_plus/share_plus.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:url_launcher/url_launcher.dart';

import '../core/hardware.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'find_care.dart';
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
class NeedsArea extends ConsumerStatefulWidget {
  const NeedsArea(this.title, this.builder);
  final String title;
  final Widget Function(Area area) builder;
  @override
  ConsumerState<NeedsArea> createState() => NeedsAreaState();
}

class NeedsAreaState extends ConsumerState<NeedsArea> {
  bool _busy = false;
  bool _denied = false;
  final _city = TextEditingController();
  String? _cityError;

  @override
  void dispose() {
    _city.dispose();
    super.dispose();
  }

  Future<void> _search() async {
    setState(() {
      _busy = true;
      _cityError = null;
    });
    try {
      final a = await searchPlace(_city.text);
      if (!mounted) return;
      if (a == null) {
        setState(() => _cityError = 'No place found. Try a city name.');
      } else {
        ref.read(areaProvider.notifier).set(a);
      }
    } catch (_) {
      if (mounted)
        setState(() => _cityError = 'Could not search. Check your connection.');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _detect({bool precise = false}) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_preciseKey, precise);
    if (!mounted) return;
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
                  : 'Approximate location (city level) shows alerts and care near you. Your exact position is only used when you tap Directions or Share my location, and it never goes to Sehat.',
              action: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  FilledButton(
                    onPressed: _busy ? null : () => _detect(),
                    child: Text(_busy ? 'Locating…' : 'Approximate only'),
                  ),
                  const SizedBox(height: 8),
                  OutlinedButton(
                    onPressed: _busy ? null : () => _detect(precise: true),
                    child: const Text('Approximate + precise for directions'),
                  ),
                  const SizedBox(height: 14),
                  TextField(
                    controller: _city,
                    textInputAction: TextInputAction.search,
                    onSubmitted: (_) => _search(),
                    decoration: InputDecoration(
                      hintText: 'Or type your city',
                      errorText: _cityError,
                      suffixIcon: IconButton(
                        icon: const Icon(Icons.search),
                        onPressed: _busy ? null : _search,
                      ),
                    ),
                  ),
                  const Padding(
                    padding: EdgeInsets.only(top: 6),
                    child: Text(
                      'Precise is read on this phone only to start directions and to share your location. You can change this any time.',
                      textAlign: TextAlign.center,
                      style: _note,
                    ),
                  ),
                ],
              ),
            ),
    );
  }
}

// ------------------------------------------------------------ Health news

final newsProvider = FutureProvider.autoDispose.family<Json, (String, String)>(
  (ref, k) => ref
      .read(apiProvider)
      .map(
        '/nearby/news?city=${Uri.encodeQueryComponent(k.$1)}&country_code=${k.$2}',
      ),
);

String _ago(Object? iso) {
  final t = DateTime.tryParse('$iso');
  if (t == null) return '';
  final d = DateTime.now().difference(t);
  if (d.inMinutes < 60) return '${d.inMinutes.clamp(1, 59)} min ago';
  if (d.inHours < 24) return '${d.inHours} h ago';
  if (d.inDays < 7) return '${d.inDays} d ago';
  return '$iso'.substring(0, 10);
}

/// Health alerts and news: stories for the user's city, worldwide news, and official outbreak notices.
class HealthNewsScreen extends ConsumerStatefulWidget {
  const HealthNewsScreen({super.key});
  @override
  ConsumerState<HealthNewsScreen> createState() => _NewsState();
}

class _NewsState extends ConsumerState<HealthNewsScreen> {
  bool _locating = false;

  @override
  void initState() {
    super.initState();
    // The Near you tab is about place, so ask for it as soon as the screen opens.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && ref.read(areaProvider) == null) _locate();
    });
  }

  Future<void> _locate() async {
    setState(() => _locating = true);
    await ref.read(areaProvider.notifier).detect();
    if (mounted) setState(() => _locating = false);
  }

  @override
  Widget build(BuildContext context) {
    final area = ref.watch(areaProvider);
    final city = (area?.city?.isNotEmpty ?? false)
        ? area!.city!
        : (area?.region ?? '');
    final cc = area?.countryCode ?? '';
    final country = area?.country ?? '';
    final news = ref.watch(newsProvider((city, cc)));
    final alerts = ref.watch(alertsProvider(country));
    return DefaultTabController(
      length: 3,
      child: Scaffold(
        appBar: AppBar(
          title: const Text('Public Health Alerts'),
          bottom: const TabBar(
            tabs: [
              Tab(text: 'Near you'),
              Tab(text: 'World'),
              Tab(text: 'Outbreaks'),
            ],
          ),
        ),
        body: TabBarView(
          children: [
            _NewsList(
              news,
              scope: 'local',
              onRetry: () => ref.invalidate(newsProvider((city, cc))),
              empty: area == null
                  ? Empty(
                      Icons.place_outlined,
                      'See health news for your city',
                      body: 'Allow location to get local stories and alerts.',
                      action: FilledButton(
                        onPressed: _locating ? null : _locate,
                        child: Text(
                          _locating ? 'Locating…' : 'Use my location',
                        ),
                      ),
                    )
                  : const Empty(
                      Icons.newspaper,
                      'No local stories right now',
                      body: 'Try again, or check the World tab.',
                    ),
              header: area == null
                  ? null
                  : Row(
                      children: [
                        const Icon(Icons.place, size: 14, color: Pal.pri),
                        const SizedBox(width: 4),
                        Text(
                          area.label.isEmpty ? 'Near you' : area.label,
                          style: const TextStyle(
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ],
                    ),
            ),
            _NewsList(
              news,
              scope: 'global',
              onRetry: () => ref.invalidate(newsProvider((city, cc))),
              empty: const Empty(Icons.public, 'No world news right now'),
            ),
            AsyncBody<List<Json>>(
              alerts,
              onRetry: () => ref.invalidate(alertsProvider(country)),
              builder: (all) => ListView(
                padding: const EdgeInsets.all(14),
                children: [
                  const Text(
                    'WHO Disease Outbreak News. Notices for your country come first.',
                    style: _note,
                  ),
                  const SizedBox(height: 8),
                  if (all.isEmpty)
                    const Empty(Icons.verified_outlined, 'No active notices'),
                  for (final a in all) _OutbreakCard(a),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _NewsList extends StatelessWidget {
  const _NewsList(
    this.news, {
    required this.scope,
    required this.onRetry,
    required this.empty,
    this.header,
  });
  final AsyncValue<Json> news;
  final String scope;
  final VoidCallback onRetry;
  final Widget empty;
  final Widget? header;

  @override
  Widget build(BuildContext context) => AsyncBody<Json>(
    news,
    onRetry: onRetry,
    builder: (data) {
      if (data['${scope}_unavailable'] == true) {
        return ErrorView(
          'Health news is temporarily unavailable.',
          onRetry: onRetry,
        );
      }
      final all = (data['news'] as List? ?? []).whereType<Map>();
      final list = all.where((n) => n['scope'] == scope).toList();
      if (list.isEmpty) {
        return Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            empty,
            TextButton.icon(
              onPressed: onRetry,
              icon: const Icon(Icons.refresh),
              label: const Text('Try again'),
            ),
          ],
        );
      }
      return ListView(
        padding: const EdgeInsets.all(14),
        children: [
          if (header != null)
            Padding(padding: const EdgeInsets.only(bottom: 8), child: header),
          for (var i = 0; i < list.length; i++)
            SCard(
              onTap: () => _launch(context, Uri.parse('${list[i]['url']}')),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '${list[i]['title']}',
                    maxLines: i == 0 ? 4 : 3,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: i == 0 ? 15 : 13,
                      fontWeight: FontWeight.w600,
                      height: 1.3,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      Pill('${list[i]['source']}'),
                      const SizedBox(width: 8),
                      Text(_ago(list[i]['published_at']), style: _note),
                      const Spacer(),
                      const Icon(Icons.open_in_new, size: 14, color: Pal.mut),
                    ],
                  ),
                ],
              ),
            ),
          const Padding(
            padding: EdgeInsets.all(6),
            child: Text(
              'Headlines link to the publisher. Sehat does not edit them.',
              style: _note,
            ),
          ),
        ],
      );
    },
  );
}

class _OutbreakCard extends StatelessWidget {
  const _OutbreakCard(this.a);
  final Json a;

  @override
  Widget build(BuildContext context) {
    final yours = a['in_country'] == true;
    return SCard(
      onTap: () => _open(context, AlertScreen(a)),
      child: IntrinsicHeight(
        child: Row(
          children: [
            Container(
              width: 4,
              decoration: BoxDecoration(
                color: yours ? Pal.warn : Pal.line,
                borderRadius: BorderRadius.circular(2),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('${a['title']}', style: _bold),
                  const SizedBox(height: 4),
                  Row(
                    children: [
                      if (yours)
                        const Padding(
                          padding: EdgeInsets.only(right: 8),
                          child: Pill(
                            'Your country',
                            fg: Pal.warn,
                            bg: Pal.warnBg,
                          ),
                        ),
                      Text(
                        '${a['source']} · ${_ago(a['published_at'])}',
                        style: _note,
                      ),
                    ],
                  ),
                ],
              ),
            ),
            const Icon(Icons.chevron_right, color: Pal.mut),
          ],
        ),
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
  const DoctorsScreen({super.key, this.initialKind = 'doctor'});
  final String initialKind;
  @override
  ConsumerState<DoctorsScreen> createState() => _DoctorsState();
}

class _DoctorsState extends ConsumerState<DoctorsScreen> {
  late String _kind = widget.initialKind;
  bool _map = false;

  @override
  Widget build(BuildContext context) => NeedsArea('Care near you', (area) {
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

/// Opens turn-by-turn directions in Google Maps. With no origin given, Google Maps starts from the
/// phone's live position, so Sehat never needs the exact location.
Future<void> _directions(BuildContext context, Json h) async {
  final prefs = await SharedPreferences.getInstance();
  var origin = '';
  if (prefs.getBool(_preciseKey) ?? false) {
    final p = await ProviderScope.containerOf(context)
        .read(locationProvider)
        .preciseNow();
    if (p != null) origin = '&origin=${p.lat},${p.lng}';
  }
  if (!context.mounted) return;
  return _launch(
    context,
    Uri.parse(
      'https://www.google.com/maps/dir/?api=1$origin&destination=${h['lat']},${h['lng']}&travelmode=driving',
    ),
  );
}

const _preciseKey = 'precise_location_ok';

class HospitalsMapScreen extends ConsumerStatefulWidget {
  const HospitalsMapScreen({super.key});
  @override
  ConsumerState<HospitalsMapScreen> createState() => _HospitalsMapState();
}

class _HospitalsMapState extends ConsumerState<HospitalsMapScreen> {
  bool _emergencyOnly = false;

  @override
  Widget build(BuildContext context) => NeedsArea('Hospitals', (area) {
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
                                      if (x['speciality'] != null)
                                        Text(
                                          '${x['speciality']}',
                                          style: _note,
                                        ),
                                    ],
                                  ),
                                ),
                                if (x['reason'] != null)
                                  Padding(
                                    padding: const EdgeInsets.only(right: 6),
                                    child: Pill('${x['reason']}'),
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
  Widget build(BuildContext context) => NeedsArea('Your area', (area) {
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

const _contactsKey = 'emergency_contacts';
const _medicalIdKey = 'medical_id_cache_confirmed';

/// Contacts are kept on the phone only, so they work offline and are never uploaded.
final emergencyContactsProvider = FutureProvider.autoDispose<List<Json>>((
  ref,
) async {
  final p = await SharedPreferences.getInstance();
  return (p.getStringList(_contactsKey) ?? [])
      .map((e) => (jsonDecode(e) as Map).cast<String, dynamic>())
      .toList();
});

Future<void> _saveContacts(List<Json> list) async {
  final p = await SharedPreferences.getInstance();
  await p.setStringList(_contactsKey, [for (final c in list) jsonEncode(c)]);
}

/// Medical ID read from the account; the last good copy is kept so it still shows without a connection.
final medicalIdProvider = FutureProvider.autoDispose<Json>((ref) async {
  final prefs = await SharedPreferences.getInstance();
  try {
    final profile = await ref.watch(profileMapProvider('').future);
    final history = await ref.watch(profileListProvider('/history').future);
    final rx = await ref.watch(profileListProvider('/prescriptions').future);
    // Saved prescriptions are the source of truth for current medicines; history entries add the rest.
    final meds = <String, String>{};
    for (final p in rx) {
      for (final i in ((p['items'] as List?) ?? []).whereType<Map>()) {
        if (i['active'] == false) continue;
        final label = [
          i['name'],
          i['strength'],
        ].where((e) => '${e ?? ''}'.isNotEmpty).join(' ');
        meds[('${i['name']}').toLowerCase()] = 'medication: $label';
      }
    }
    for (final e in history) {
      if (e['source'] != 'report' &&
          e['status'] == 'active' &&
          e['kind'] == 'medication') {
        meds.putIfAbsent(
          ('${e['title']}').toLowerCase(),
          () => 'medication: ${e['title']}',
        );
      }
    }
    final id = <String, dynamic>{
      'blood_group': profile['blood_group'],
      'items': [
        for (final e in history)
          if (e['source'] != 'report' &&
              e['status'] == 'active' &&
              (e['kind'] == 'allergy' || e['kind'] == 'condition'))
            '${e['kind']}: ${e['title']}',
        ...meds.values,
      ],
      'cached': false,
    };
    await prefs.setString(_medicalIdKey, jsonEncode(id));
    return id;
  } catch (_) {
    final saved = prefs.getString(_medicalIdKey);
    if (saved == null) rethrow;
    return {
      ...(jsonDecode(saved) as Map).cast<String, dynamic>(),
      'cached': true,
    };
  }
});

class EmergencyScreen extends ConsumerWidget {
  const EmergencyScreen({super.key});

  Future<void> _shareLocation(BuildContext context, WidgetRef ref) async {
    final prefs = await SharedPreferences.getInstance();
    if (!(prefs.getBool(_preciseKey) ?? false)) {
      final ok = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Share your exact location?'),
          content: const Text(
            'Your phone will read your precise position once and put it in a message you choose to send. Sehat never receives it.',
            style: TextStyle(fontSize: 12),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Allow and share'),
            ),
          ],
        ),
      );
      if (ok != true) return;
      await prefs.setBool(_preciseKey, true);
    }
    final p = await ref.read(locationProvider).preciseNow();
    if (!context.mounted) return;
    if (p == null) {
      toast(context, 'Location is off or not allowed on this phone.');
      return;
    }
    await Share.share(
      'I need help. My location: https://www.google.com/maps?q=${p.lat},${p.lng}',
    );
  }

  /// Picks from the phone's address book. The system picker needs no contacts permission, and it
  /// is unavailable in a browser, where the caller falls back to typing.
  Future<Json?> _pickFromContacts() async {
    try {
      final c = await FlutterContacts.native.showPicker(
        properties: {ContactProperty.name, ContactProperty.phone},
      );
      if (c == null || c.phones.isEmpty) return null;
      return {'name': c.displayName, 'phone': c.phones.first.number};
    } catch (_) {
      return null;
    }
  }

  Future<void> _addContact(BuildContext context, WidgetRef ref) async {
    final picked = await _pickFromContacts();
    if (picked != null) {
      await _saveContacts([
        ...await ref.read(emergencyContactsProvider.future),
        picked,
      ]);
      ref.invalidate(emergencyContactsProvider);
      return;
    }
    if (!context.mounted) return;
    toast(
      context,
      'Pick from contacts is not available here. Enter the number.',
    );
    final name = TextEditingController(), phone = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Emergency contact'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: name,
              autofocus: true,
              decoration: const InputDecoration(labelText: 'Name'),
            ),
            TextField(
              controller: phone,
              keyboardType: TextInputType.phone,
              decoration: const InputDecoration(labelText: 'Phone'),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (ok != true || name.text.trim().isEmpty || phone.text.trim().isEmpty) {
      return;
    }
    final list = [
      ...await ref.read(emergencyContactsProvider.future),
      {'name': name.text.trim(), 'phone': phone.text.trim()},
    ];
    await _saveContacts(list);
    ref.invalidate(emergencyContactsProvider);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final area = ref.watch(areaProvider);
    final number = _ambulance[area?.countryCode] ?? '112';
    final medical = ref.watch(medicalIdProvider);
    final contacts = ref.watch(emergencyContactsProvider).asData?.value ?? [];
    final hospitals = area == null
        ? const AsyncValue<List<Json>>.data([])
        : ref.watch(hospitalsProvider(area));
    final nearest = nearestEmergency(hospitals.asData?.value ?? []);
    return Scaffold(
      appBar: AppBar(title: const Text('Emergency Profile')),
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
          SectionTitle(
            nearest != null && nearest['emergency'] != true
                ? 'Nearest hospital'
                : 'Nearest emergency hospital',
          ),
          if (area == null)
            SCard(
              onTap: () => _open(context, const HospitalsMapScreen()),
              child: const Text(
                'Turn on approximate location to find the nearest one.',
                style: _note,
              ),
            )
          else if (nearest == null)
            Text(
              hospitals.isLoading
                  ? 'Looking…'
                  : 'No emergency hospital found within range.',
              style: _note,
            )
          else
            SCard(
              onTap: () => _open(context, HospitalScreen(nearest)),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '${nearest['name']} · ${fmtNum(numOf(nearest['distance_km']))} km',
                      style: _bold,
                    ),
                  ),
                  const Icon(Icons.directions, color: Pal.pri),
                ],
              ),
            ),
          const SectionTitle('Medical ID'),
          SCard(
            child: medical.when(
              loading: () => const Text('Loading…', style: _note),
              error: (e, _) => Text(friendly(e), style: _note),
              data: (m) => Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Blood group: ${m['blood_group'] ?? 'not set'}',
                    style: const TextStyle(fontSize: 12),
                  ),
                  const SizedBox(height: 6),
                  if ((m['items'] as List).isEmpty)
                    const Text(
                      'No allergies, conditions or medications recorded.',
                      style: _note,
                    ),
                  for (final i in (m['items'] as List))
                    Text('$i', style: const TextStyle(fontSize: 12)),
                  if (m['cached'] == true)
                    const Padding(
                      padding: EdgeInsets.only(top: 6),
                      child: Text('Saved copy (no connection).', style: _note),
                    ),
                ],
              ),
            ),
          ),
          SectionTitle(
            'Emergency contacts',
            trailing: TextButton.icon(
              onPressed: () => _addContact(context, ref),
              icon: const Icon(Icons.contacts_outlined, size: 16),
              label: const Text('Choose from contacts'),
            ),
          ),
          if (contacts.isEmpty)
            const Text(
              'Saved on this phone only, so they work offline.',
              style: _note,
            ),
          for (var i = 0; i < contacts.length; i++)
            SCard(
              onTap: () => _launch(
                context,
                Uri(scheme: 'tel', path: '${contacts[i]['phone']}'),
              ),
              child: Row(
                children: [
                  CircleAvatar(
                    radius: 16,
                    backgroundColor: Pal.priBg,
                    child: Text(
                      '${contacts[i]['name']}'.characters.firstOrNull
                              ?.toUpperCase() ??
                          '?',
                      style: const TextStyle(color: Pal.pri, fontSize: 13),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('${contacts[i]['name']}', style: _bold),
                        Text('${contacts[i]['phone']}', style: _note),
                      ],
                    ),
                  ),
                  const Icon(Icons.call, size: 16, color: Pal.pri),
                  IconButton(
                    visualDensity: VisualDensity.compact,
                    icon: const Icon(Icons.close, size: 16, color: Pal.mut),
                    onPressed: () async {
                      await _saveContacts([...contacts]..removeAt(i));
                      ref.invalidate(emergencyContactsProvider);
                    },
                  ),
                ],
              ),
            ),
          const SizedBox(height: 6),
          OutlinedButton.icon(
            onPressed: () => _shareLocation(context, ref),
            icon: const Icon(Icons.share_location),
            label: const Text('Share my location'),
          ),
          const Padding(
            padding: EdgeInsets.only(top: 4),
            child: Text(
              'Sends a Google Maps link with your exact position through the app you pick. Sehat never receives it.',
              style: _note,
            ),
          ),
        ],
      ),
    );
  }
}

/// The closest hospital that offers emergency care; many map entries carry no emergency tag, so the
/// closest hospital of any kind is used when none does.
Json? nearestEmergency(List<Json> all) {
  final byDistance = [...all]
    ..sort(
      (a, b) => (numOf(a['distance_km']) ?? 0).compareTo(
        numOf(b['distance_km']) ?? 0,
      ),
    );
  return byDistance.where((h) => h['emergency'] == true).firstOrNull ??
      byDistance.firstOrNull;
}

Future<void> openDirections(BuildContext c, Json h) => _directions(c, h);
Future<void> dial(BuildContext c, String number) =>
    _launch(c, Uri(scheme: 'tel', path: number));
String emergencyNumber(Area? a) => _ambulance[a?.countryCode] ?? '112';
Future<void> shareMyLocation(BuildContext c, WidgetRef r) =>
    const EmergencyScreen()._shareLocation(c, r);
