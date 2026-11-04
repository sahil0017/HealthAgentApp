import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/hardware.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'places.dart';

const _note = TextStyle(fontSize: 10.5, color: Pal.mut);

final environmentProvider = FutureProvider.autoDispose.family<Json, Area>(
  (ref, a) =>
      ref.read(apiProvider).map('/environment/now?lat=${a.lat}&lng=${a.lng}'),
);

String _num(Object? v) => fmtNum(numOf(v));

/// Live air quality and weather for the user's approximate area.
class EnvironmentScreen extends ConsumerWidget {
  const EnvironmentScreen({super.key});
  @override
  Widget build(
    BuildContext context,
    WidgetRef ref,
  ) => NeedsArea('Air & weather', (area) {
    return AsyncBody<Json>(
      ref.watch(environmentProvider(area)),
      onRetry: () => ref.invalidate(environmentProvider(area)),
      builder: (d) {
        final air = ((d['air'] as Map?)?['current'] as Map?)
            ?.cast<String, dynamic>();
        final wx = ((d['weather'] as Map?)?['current'] as Map?)
            ?.cast<String, dynamic>();
        final station = (d['station'] as Map?)?.cast<String, dynamic>();
        return ListView(
          padding: const EdgeInsets.all(14),
          children: [
            if (air != null)
              SCard(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Text(
                          _num(air['us_aqi']),
                          style: const TextStyle(
                            fontSize: 30,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        const SizedBox(width: 10),
                        Pill('${d['air_category'] ?? 'Air quality'}'),
                      ],
                    ),
                    const SizedBox(height: 6),
                    Text(
                      '${d['air_advice'] ?? ''}',
                      style: const TextStyle(fontSize: 12),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      'PM2.5 ${_num(air['pm2_5'])} · PM10 ${_num(air['pm10'])} · Ozone ${_num(air['ozone'])} · NO₂ ${_num(air['nitrogen_dioxide'])} (µg/m³)',
                      style: _note,
                    ),
                    if (air['uv_index'] != null)
                      Text('UV index ${_num(air['uv_index'])}', style: _note),
                  ],
                ),
              ),
            if (wx != null)
              SCard(
                child: Row(
                  children: [
                    const Icon(Icons.thermostat, color: Pal.pri),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        '${_num(wx['temperature_2m'])}°C, feels like ${_num(wx['apparent_temperature'])}°C\nHumidity ${_num(wx['relative_humidity_2m'])}% · Wind ${_num(wx['wind_speed_10m'])} km/h',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                  ],
                ),
              ),
            if (station != null)
              SCard(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      'Nearest station: ${station['name']}',
                      style: const TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    for (final r
                        in ((station['readings'] as List?) ?? []).cast<Json>())
                      Text(
                        '${r['parameter']}: ${_num(r['value'])} ${r['unit']}',
                        style: _note,
                      ),
                    const Text('Source: OpenAQ', style: _note),
                  ],
                ),
              ),
            if (air == null && wx == null)
              const Empty(Icons.cloud_off, 'No data for this area'),
            const Padding(
              padding: EdgeInsets.all(8),
              child: Text(
                'Source: Open-Meteo, for your approximate area. Categories follow the US EPA AQI scale. General guidance, not medical advice.',
                style: _note,
              ),
            ),
          ],
        );
      },
    );
  });
}

/// Looks up a medicine by name: RxNorm matches and the FDA prescribing text.
class MedicineScreen extends ConsumerStatefulWidget {
  const MedicineScreen({super.key});
  @override
  ConsumerState<MedicineScreen> createState() => _MedicineState();
}

class _MedicineState extends ConsumerState<MedicineScreen> {
  final _c = TextEditingController();
  Json? _search;
  Json? _label;
  Object? _error;
  bool _busy = false;

  Future<void> _go() async {
    final q = _c.text.trim();
    if (q.length < 2) return;
    setState(() {
      _busy = true;
      _error = null;
      _label = null;
    });
    final api = ref.read(apiProvider);
    final enc = Uri.encodeQueryComponent(q);
    try {
      final s = await api.map('/medications/search?q=$enc');
      Json? l;
      try {
        l = await api.map('/medications/label?name=$enc');
      } on ApiError catch (e) {
        if (e.status != 404) rethrow;
      }
      if (mounted) {
        setState(() {
          _search = s;
          _label = l;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final meds = ((_search?['medicines'] as List?) ?? []).cast<Json>();
    final sections = ((_label?['sections'] as List?) ?? [])
        .cast<Json>()
        .where((s) => '${s['text']}'.isNotEmpty)
        .toList();
    return Scaffold(
      appBar: AppBar(title: const Text('Medicine lookup')),
      body: ListView(
        padding: const EdgeInsets.all(14),
        children: [
          TextField(
            controller: _c,
            textInputAction: TextInputAction.search,
            onSubmitted: (_) => _go(),
            decoration: InputDecoration(
              hintText: 'Medicine name, e.g. metformin',
              suffixIcon: IconButton(
                icon: const Icon(Icons.search),
                onPressed: _busy ? null : _go,
              ),
            ),
          ),
          if (_busy)
            const Padding(
              padding: EdgeInsets.all(20),
              child: Skeleton(rows: 2),
            ),
          if (_error != null)
            SizedBox(height: 200, child: ErrorView(_error!, onRetry: _go)),
          if (_search?['did_you_mean'] != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                'Showing results for “${_search!['did_you_mean']}”.',
                style: _note,
              ),
            ),
          if (_search != null && !_busy && meds.isEmpty && _error == null)
            const Empty(Icons.medication_outlined, 'No match found'),
          for (final s in sections) ...[
            SectionTitle('${s['title']}'),
            SCard(
              color: s['title'] == 'Boxed warning' ? Pal.warnBg : null,
              child: Text(
                '${s['text']}',
                style: const TextStyle(fontSize: 11.5),
              ),
            ),
          ],
          if (meds.isNotEmpty) ...[
            const SectionTitle('Matching products'),
            for (final m in meds.take(12))
              SCard(
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        '${m['name']}',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                    Text('${m['type']}', style: _note),
                  ],
                ),
              ),
          ],
          if (_search != null)
            Padding(
              padding: const EdgeInsets.all(8),
              child: Text('${_search!['coverage']}', style: _note),
            ),
        ],
      ),
    );
  }
}
