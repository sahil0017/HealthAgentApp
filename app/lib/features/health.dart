import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';

import 'records.dart';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';

class HealthTab extends ConsumerStatefulWidget {
  const HealthTab({super.key});
  @override
  ConsumerState<HealthTab> createState() => _HealthState();
}

class _HealthState extends ConsumerState<HealthTab> {
  int _seg = 0;
  @override
  Widget build(BuildContext context) => Column(
    children: [
      Padding(
        padding: const EdgeInsets.fromLTRB(14, 8, 14, 4),
        child: SegmentedButton<int>(
          showSelectedIcon: false,
          segments: const [
            ButtonSegment(value: 0, label: Text('Lab results')),
            ButtonSegment(value: 1, label: Text('Devices')),
          ],
          selected: {_seg},
          onSelectionChanged: (s) => setState(() => _seg = s.first),
        ),
      ),
      if (_seg == 0)
        Align(
          alignment: Alignment.centerRight,
          child: TextButton.icon(
            onPressed: () => showAddBiomarker(context, ref),
            icon: const Icon(Icons.add, size: 16),
            label: const Text('Add a value'),
          ),
        ),
      Expanded(child: _seg == 0 ? const _Labs() : const _Devices()),
    ],
  );
}

class _Labs extends ConsumerWidget {
  const _Labs();
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(profileListProvider('/biomarkers/summary'));
    return RefreshIndicator(
      onRefresh: () async =>
          ref.refresh(profileListProvider('/biomarkers/summary').future),
      child: AsyncBody<List<Json>>(
        s,
        onRetry: () =>
            ref.invalidate(profileListProvider('/biomarkers/summary')),
        builder: (list) {
          if (list.isEmpty)
            return const Empty(
              Icons.science_outlined,
              'No lab results yet',
              body: 'Confirm a document in the Vault to see your biomarkers here.',
            );
          final flagged = list
              .where(
                (b) => ((b['latest'] as Map?)?['flag'] ?? 'normal') != 'normal',
              )
              .length;
          final byCat = <String, List<Json>>{};
          for (final b in list) {
            byCat.putIfAbsent('${b['category']}', () => []).add(b);
          }
          return ListView(
            padding: const EdgeInsets.fromLTRB(14, 6, 14, 16),
            children: [
              SCard(
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        '${list.length} biomarkers tracked',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                    Pill(
                      flagged == 0 ? 'All in range' : '$flagged to review',
                      fg: flagged == 0 ? Pal.ok : Pal.warn,
                      bg: flagged == 0 ? Pal.okBg : Pal.warnBg,
                    ),
                  ],
                ),
              ),
              for (final e in byCat.entries) ...[
                SectionTitle(e.key.replaceAll('_', ' ')),
                for (final b in e.value) _BiomarkerTile(b),
              ],
            ],
          );
        },
      ),
    );
  }
}

class _BiomarkerTile extends StatelessWidget {
  const _BiomarkerTile(this.b);
  final Json b;
  @override
  Widget build(BuildContext context) {
    final latest = (b['latest'] as Map?)?.cast<String, dynamic>();
    final range = (latest?['range'] as Map?)?.cast<String, dynamic>();
    final v = numOf(latest?['value']);
    return SCard(
      onTap: () => Navigator.push(
        context,
        MaterialPageRoute(
          builder: (_) =>
              BiomarkerScreen('${b['biomarker_code']}', '${b['display_name']}'),
        ),
      ),
      child: Column(
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  '${b['display_name']}',
                  style: const TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              Text(
                '${fmtNum(v)} ${b['unit'] ?? ''}',
                style: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(width: 8),
              Pill.flag(latest?['flag'] as String?),
            ],
          ),
          if (v != null) ...[
            const SizedBox(height: 7),
            RangeBar(
              value: v.toDouble(),
              low: numOf(range?['low'])?.toDouble(),
              high: numOf(range?['high'])?.toDouble(),
            ),
          ],
        ],
      ),
    );
  }
}

final _trendProvider = FutureProvider.family<Json, String>((ref, code) {
  final id = ref.watch(profileIdProvider);
  return ref.read(apiProvider).map('/profiles/$id/biomarkers/$code/trend');
});

class BiomarkerScreen extends ConsumerWidget {
  const BiomarkerScreen(this.code, this.name, {super.key});
  final String code, name;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = ref.watch(_trendProvider(code));
    return Scaffold(
      appBar: AppBar(title: Text(name)),
      body: AsyncBody<Json>(
        t,
        onRetry: () => ref.invalidate(_trendProvider(code)),
        builder: (d) {
          final pts = (d['points'] as List? ?? [])
              .whereType<Map>()
              .map((e) => e.cast<String, dynamic>())
              .toList();
          final spots = [
            for (var i = 0; i < pts.length; i++)
              FlSpot(i.toDouble(), (numOf(pts[i]['value']) ?? 0).toDouble()),
          ];
          final trend = (d['trend'] as Map?)?['direction'] ?? '';
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [
              if (spots.length >= 2)
                SCard(
                  child: SizedBox(
                    height: 170,
                    child: LineChart(
                      LineChartData(
                        gridData: const FlGridData(show: false),
                        borderData: FlBorderData(show: false),
                        titlesData: const FlTitlesData(
                          topTitles: AxisTitles(),
                          rightTitles: AxisTitles(),
                          bottomTitles: AxisTitles(),
                          leftTitles: AxisTitles(
                            sideTitles: SideTitles(
                              showTitles: true,
                              reservedSize: 36,
                            ),
                          ),
                        ),
                        lineBarsData: [
                          LineChartBarData(
                            spots: spots,
                            isCurved: true,
                            color: Pal.pri,
                            barWidth: 2.5,
                            dotData: const FlDotData(show: true),
                          ),
                        ],
                      ),
                    ),
                  ),
                )
              else
                const SCard(
                  child: Text(
                    'Add another result to see a trend.',
                    style: TextStyle(fontSize: 12, color: Pal.mut),
                  ),
                ),
              if ('$trend'.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.all(4),
                  child: Text(
                    'Direction: $trend',
                    style: const TextStyle(fontSize: 11, color: Pal.mut),
                  ),
                ),
              const SectionTitle('Readings'),
              for (final p in pts.reversed)
                SCard(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 12,
                    vertical: 9,
                  ),
                  child: Row(
                    children: [
                      Expanded(
                        child: Text(
                          '${p['observed_at']}'.split('T').first,
                          style: const TextStyle(fontSize: 12),
                        ),
                      ),
                      Text(
                        '${fmtNum(numOf(p['value']))} ${p['unit']}',
                        style: const TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Pill.flag(p['flag'] as String?),
                    ],
                  ),
                ),
              const Padding(
                padding: EdgeInsets.all(8),
                child: Text(
                  'Reference ranges are general guidance, not a diagnosis.',
                  style: TextStyle(fontSize: 10, color: Pal.mut),
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}

class _Devices extends ConsumerStatefulWidget {
  const _Devices();
  @override
  ConsumerState<_Devices> createState() => _DevicesState();
}

class _DevicesState extends ConsumerState<_Devices> {
  bool _busy = false;

  Future<void> _sync() async {
    final h = ref.read(healthProvider);
    final api = ref.read(apiProvider);
    final pid = ref.read(profileIdProvider);
    setState(() => _busy = true);
    try {
      if (!await h.authorize()) {
        if (mounted) toast(context, 'Health access was not granted.');
        return;
      }
      final samples = await h.read();
      if (samples.isEmpty) {
        if (mounted) toast(context, 'No recent health data on this phone.');
        return;
      }
      final devices = await api.list('/profiles/$pid/devices');
      var dev = devices
          .where(
            (d) =>
                d['kind'] == 'phone_health_app' && d['source'] == h.sourceName,
          )
          .firstOrNull;
      dev ??= (await api.post('/profiles/$pid/devices', {
        'kind': 'phone_health_app',
        'source': h.sourceName,
        'name': 'This phone',
        'external_id': 'phone',
      })) as Json;
      final res = await api.post(
        '/profiles/$pid/devices/${dev['id']}/readings',
        {
          'readings': [
            for (final s in samples)
              {
                'metric': s.metric,
                'value': s.value,
                'unit': s.unit,
                'measured_at': s.at.toUtc().toIso8601String(),
                'external_id': s.externalId,
                'utc_offset_minutes': s.at.timeZoneOffset.inMinutes,
              },
          ],
        },
      );
      ref.invalidate(profileListProvider('/devices'));
      ref.invalidate(_summaryProvider);
      if (mounted)
        toast(
          context,
          'Synced ${(res is Map ? res['accepted'] : null) ?? samples.length} readings.',
        );
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final s = ref.watch(_summaryProvider);
    return ListView(
      padding: const EdgeInsets.fromLTRB(14, 6, 14, 16),
      children: [
        SCard(
          child: Row(
            children: [
              const Icon(Icons.watch_outlined, color: Pal.pri),
              const SizedBox(width: 10),
              const Expanded(
                child: Text(
                  'Sync steps, heart rate, oxygen, weight and more from your phone’s health app.',
                  style: TextStyle(fontSize: 11, color: Pal.mut),
                ),
              ),
              const SizedBox(width: 8),
              FilledButton(
                style: FilledButton.styleFrom(minimumSize: const Size(72, 36)),
                onPressed: _busy ? null : _sync,
                child: Text(_busy ? '…' : 'Sync'),
              ),
            ],
          ),
        ),
        AsyncBody<List<Json>>(
          s,
          onRetry: () => ref.invalidate(_summaryProvider),
          builder: (list) {
            if (list.isEmpty)
              return const Padding(
                padding: EdgeInsets.all(30),
                child: Empty(
                  Icons.monitor_heart_outlined,
                  'No device data yet',
                ),
              );
            return Column(
              children: [
                for (final m in list)
                  SCard(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 9,
                    ),
                    child: Row(
                      children: [
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                '${m['name']}',
                                style: const TextStyle(
                                  fontSize: 12,
                                  fontWeight: FontWeight.w600,
                                ),
                              ),
                              Text(
                                '7-day avg ${fmtNum(numOf(m['mean_7_days']))} · ${m['trend'] ?? ''}',
                                style: const TextStyle(
                                  fontSize: 10,
                                  color: Pal.mut,
                                ),
                              ),
                            ],
                          ),
                        ),
                        Text(
                          '${fmtNum(numOf(m['latest_value']))} ${m['unit']}',
                          style: const TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        if (m['outside_typical_range'] == true) ...[
                          const SizedBox(width: 6),
                          const Pill(
                            'Outside typical',
                            fg: Pal.warn,
                            bg: Pal.warnBg,
                          ),
                        ],
                      ],
                    ),
                  ),
              ],
            );
          },
        ),
      ],
    );
  }
}

final _summaryProvider = FutureProvider<List<Json>>((ref) {
  final id = ref.watch(profileIdProvider);
  if (id == null) return [];
  return ref
      .read(apiProvider)
      .list('/profiles/$id/device-readings/summary', key: 'summaries');
});
