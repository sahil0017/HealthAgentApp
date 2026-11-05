import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';

import 'places.dart';
import 'plan.dart';
import 'records.dart';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'insights.dart';

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
            ButtonSegment(value: 0, label: Text('Lab Results')),
            ButtonSegment(value: 1, label: Text('Vitals')),
            ButtonSegment(value: 2, label: Text('Wearables')),
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
            label: const Text('Add a result'),
          ),
        ),
      Expanded(
        child: switch (_seg) {
          0 => const _Labs(),
          1 => const _Vitals(),
          _ => const _Devices(),
        },
      ),
    ],
  );
}

class _Labs extends ConsumerStatefulWidget {
  const _Labs();
  @override
  ConsumerState<_Labs> createState() => _LabsState();
}

class _LabsState extends ConsumerState<_Labs> {
  String _show = 'all';

  @override
  Widget build(BuildContext context) {
    final s = ref.watch(profileListProvider('/biomarkers/summary'));
    return RefreshIndicator(
      onRefresh: () async =>
          ref.refresh(profileListProvider('/biomarkers/summary').future),
      child: AsyncBody<List<Json>>(
        s,
        onRetry: () =>
            ref.invalidate(profileListProvider('/biomarkers/summary')),
        builder: (list) {
          if (list.isEmpty) {
            return const Empty(
              Icons.science_outlined,
              'No lab results yet',
              body: 'Confirm a report in the Reports tab and your results appear here, grouped by panel.',
            );
          }
          String flagOf(Json b) => '${(b['latest'] as Map?)?['flag'] ?? ''}';
          final out = list.where((b) => outOfRange(flagOf(b))).length;
          final inr = list.where((b) => inRange(flagOf(b))).length;
          final unrated = list.length - out - inr;
          final shown = list.where((b) {
            final f = flagOf(b);
            return switch (_show) {
              'out' => outOfRange(f),
              'in' => inRange(f),
              _ => true,
            };
          });
          final byPanel = <String, List<Json>>{};
          final labels = <String, String>{};
          for (final b in shown) {
            final k = '${b['category']}';
            byPanel.putIfAbsent(k, () => []).add(b);
            labels[k] = '${b['category_label'] ?? 'Other measurements'}';
          }
          final panels = byPanel.keys.toList()
            ..sort((x, y) {
              final ox = byPanel[x]!.where((b) => outOfRange(flagOf(b))).length;
              final oy = byPanel[y]!.where((b) => outOfRange(flagOf(b))).length;
              if (ox != oy) return oy.compareTo(ox);
              if (x == 'other') return 1;
              if (y == 'other') return -1;
              return labels[x]!.compareTo(labels[y]!);
            });
          Widget chip(String id, String text) => Padding(
            padding: const EdgeInsets.only(right: 8),
            child: ChoiceChip(
              label: Text(text, style: const TextStyle(fontSize: 12)),
              selected: _show == id,
              onSelected: (_) => setState(() => _show = id),
            ),
          );
          return ListView(
            padding: const EdgeInsets.fromLTRB(14, 6, 14, 16),
            children: [
              SCard(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            '${list.length} lab results',
                            style: const TextStyle(
                              fontSize: 14,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                        Pill(
                          out == 0 ? 'All in range' : '$out outside range',
                          fg: out == 0 ? Pal.ok : Pal.warn,
                          bg: out == 0 ? Pal.okBg : Pal.warnBg,
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    ClipRRect(
                      borderRadius: BorderRadius.circular(4),
                      child: SizedBox(
                        height: 8,
                        child: Row(
                          children: [
                            if (inr > 0)
                              Expanded(
                                flex: inr,
                                child: Container(color: Pal.ok),
                              ),
                            if (out > 0) ...[
                              const SizedBox(width: 2),
                              Expanded(
                                flex: out,
                                child: Container(color: Pal.warn),
                              ),
                            ],
                            if (unrated > 0) ...[
                              const SizedBox(width: 2),
                              Expanded(
                                flex: unrated,
                                child: Container(color: Pal.line),
                              ),
                            ],
                          ],
                        ),
                      ),
                    ),
                    const SizedBox(height: 6),
                    Text(
                      [
                        '$inr in range',
                        '$out outside range',
                        if (unrated > 0) '$unrated without a range',
                      ].join(' · '),
                      style: const TextStyle(fontSize: 10.5, color: Pal.mut),
                    ),
                  ],
                ),
              ),
              SizedBox(
                height: 40,
                child: ListView(
                  scrollDirection: Axis.horizontal,
                  children: [
                    chip('all', 'All · ${list.length}'),
                    if (out > 0) chip('out', 'Outside range · $out'),
                    chip('in', 'In range · $inr'),
                  ],
                ),
              ),
              if (panels.isEmpty)
                const Padding(
                  padding: EdgeInsets.all(20),
                  child: Center(
                    child: Text(
                      'Nothing in this view.',
                      style: TextStyle(color: Pal.mut, fontSize: 12),
                    ),
                  ),
                ),
              for (final k in panels) ...[
                SectionTitle(
                  labels[k]!,
                  trailing: Builder(
                    builder: (_) {
                      final o = byPanel[k]!
                          .where((b) => outOfRange(flagOf(b)))
                          .length;
                      return o == 0
                          ? Text(
                              '${byPanel[k]!.length} results',
                              style: const TextStyle(
                                fontSize: 10.5,
                                color: Pal.mut,
                              ),
                            )
                          : Pill(
                              '$o of ${byPanel[k]!.length} outside range',
                              fg: Pal.warn,
                              bg: Pal.warnBg,
                            );
                    },
                  ),
                ),
                for (final b
                    in (byPanel[k]!..sort((x, y) {
                      final ox = outOfRange(flagOf(x)) ? 0 : 1;
                      final oy = outOfRange(flagOf(y)) ? 0 : 1;
                      return ox != oy
                          ? ox.compareTo(oy)
                          : '${x['display_name']}'.compareTo(
                              '${y['display_name']}',
                            );
                    })))
                  _BiomarkerTile(b),
              ],
            ],
          );
        },
      ),
    );
  }
}

String _rangeText(Map? r, String unit) {
  final lo = numOf(r?['low']), hi = numOf(r?['high']);
  final u = unit.isEmpty ? '' : ' $unit';
  if (lo != null && hi != null) return '${fmtNum(lo)}–${fmtNum(hi)}$u';
  if (hi != null) return 'up to ${fmtNum(hi)}$u';
  if (lo != null) return 'at least ${fmtNum(lo)}$u';
  return '';
}

String _shortDate(String? iso) {
  final d = DateTime.tryParse(iso ?? '');
  if (d == null) return '';
  const m = [
    'Jan',
    'Feb',
    'Mar',
    'Apr',
    'May',
    'Jun',
    'Jul',
    'Aug',
    'Sep',
    'Oct',
    'Nov',
    'Dec',
  ];
  return '${d.day} ${m[d.month - 1]} ${d.year}';
}

class _BiomarkerTile extends StatelessWidget {
  const _BiomarkerTile(this.b);
  final Json b;
  @override
  Widget build(BuildContext context) {
    final latest = (b['latest'] as Map?)?.cast<String, dynamic>();
    final range = (latest?['range'] as Map?)?.cast<String, dynamic>();
    final v = numOf(latest?['value']);
    final unit = '${b['unit'] ?? ''}';
    final rt = _rangeText(range, unit);
    final dir = '${(b['trend'] as Map?)?['direction'] ?? b['trend'] ?? ''}';
    final trendIcon = switch (dir) {
      'rising' => Icons.trending_up,
      'falling' => Icons.trending_down,
      'stable' => Icons.trending_flat,
      _ => null,
    };
    return SCard(
      onTap: () => Navigator.push(
        context,
        MaterialPageRoute(
          builder: (_) =>
              BiomarkerScreen('${b['biomarker_code']}', '${b['display_name']}'),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  '${b['display_name']}',
                  style: const TextStyle(
                    fontSize: 12.5,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              Pill.flag(latest?['flag'] as String?),
            ],
          ),
          const SizedBox(height: 4),
          Row(
            crossAxisAlignment: CrossAxisAlignment.baseline,
            textBaseline: TextBaseline.alphabetic,
            children: [
              Text(
                fmtNum(v),
                style: const TextStyle(
                  fontSize: 19,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(width: 4),
              Expanded(
                child: Text(
                  unit,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontSize: 11, color: Pal.mut),
                ),
              ),
              if (trendIcon != null) Icon(trendIcon, size: 16, color: Pal.mut),
            ],
          ),
          if (v != null) ...[
            const SizedBox(height: 6),
            RangeBar(
              value: v.toDouble(),
              low: numOf(range?['low'])?.toDouble(),
              high: numOf(range?['high'])?.toDouble(),
            ),
          ],
          const SizedBox(height: 5),
          Text(
            [
              if (rt.isNotEmpty) 'Reference $rt' else 'No reference range',
              if (_shortDate(latest?['observed_at'] as String?).isNotEmpty)
                'Tested ${_shortDate(latest?['observed_at'] as String?)}',
            ].join(' · '),
            style: const TextStyle(fontSize: 10, color: Pal.mut),
          ),
        ],
      ),
    );
  }
}

final _trendProvider = FutureProvider.family<Json, String>((ref, code) {
  final id = ref.watch(profileIdProvider);
  return ref.read(apiProvider).map('/profiles/$id/biomarkers/$code/trend');
});

/// What a single test connects to: its goal, a repeat-test follow-up and labs nearby.
class _LinkedActions extends ConsumerWidget {
  const _LinkedActions(this.code, this.name, this.goal);
  final String code, name;
  final Json? goal;

  Future<void> _addRepeat(BuildContext context, WidgetRef ref) async {
    try {
      await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/follow-ups',
        {'kind': 'investigation', 'title': 'Repeat $name'},
      );
      refreshCarePlan(ref);
      if (context.mounted) toast(context, 'Added to your care plan');
    } catch (e) {
      if (context.mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    void open(Widget w) =>
        Navigator.push(context, MaterialPageRoute<void>(builder: (_) => w));
    final lo = numOf(goal?['target_min']), hi = numOf(goal?['target_max']);
    final status = '${(goal?['progress'] as Map?)?['status'] ?? ''}'.replaceAll(
      '_',
      ' ',
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (goal != null)
          SCard(
            onTap: () => open(const GoalsScreen()),
            child: Row(
              children: [
                const Icon(Icons.flag_outlined, color: Pal.pri, size: 18),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    'Your target: ${lo != null && hi != null
                        ? '${fmtNum(lo)}–${fmtNum(hi)}'
                        : hi != null
                        ? 'up to ${fmtNum(hi)}'
                        : 'at least ${fmtNum(lo)}'} ${goal?['unit'] ?? ''}',
                    style: const TextStyle(
                      fontSize: 12,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                if (status.isNotEmpty) Pill(status),
              ],
            ),
          ),
        Wrap(
          spacing: 8,
          children: [
            if (goal == null)
              ActionChip(
                avatar: const Icon(Icons.flag_outlined, size: 16),
                label: const Text('Set a goal'),
                onPressed: () => open(GoalForm(initialCode: code)),
              ),
            ActionChip(
              avatar: const Icon(Icons.add_task, size: 16),
              label: const Text('Plan a repeat test'),
              onPressed: () => _addRepeat(context, ref),
            ),
            ActionChip(
              avatar: const Icon(Icons.biotech_outlined, size: 16),
              label: const Text('Find a lab'),
              onPressed: () => open(const DoctorsScreen(initialKind: 'lab')),
            ),
          ],
        ),
      ],
    );
  }
}

class BiomarkerScreen extends ConsumerWidget {
  const BiomarkerScreen(this.code, this.name, {super.key});
  final String code, name;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = ref.watch(_trendProvider(code));
    final goal = (ref.watch(profileListProvider('/goals')).asData?.value ?? [])
        .where((g) => g['biomarker_code'] == code && g['status'] == 'active')
        .firstOrNull;
    final gMin = numOf(goal?['target_min'])?.toDouble();
    final gMax = numOf(goal?['target_max'])?.toDouble();
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
              InsightPanel(
                path: '/agent/insights/biomarker/$code',
                title: 'What this means for you',
                icon: Icons.lightbulb_outline,
              ),
              _LinkedActions(code, name, goal),
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
                        extraLinesData: ExtraLinesData(
                          horizontalLines: [
                            for (final v in [gMin, gMax])
                              if (v != null)
                                HorizontalLine(
                                  y: v,
                                  color: Pal.ok,
                                  strokeWidth: 1,
                                  dashArray: [5, 4],
                                ),
                          ],
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
                    'Add another result to see how this changes over time.',
                    style: TextStyle(fontSize: 12, color: Pal.mut),
                  ),
                ),
              if ('$trend'.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.all(4),
                  child: Text(
                    'Trend: ${switch ('$trend') {
                      'rising' => 'Rising',
                      'falling' => 'Falling',
                      'stable' => 'Stable',
                      _ => 'Not enough results yet',
                    }}',
                    style: const TextStyle(fontSize: 11, color: Pal.mut),
                  ),
                ),
              const SectionTitle('Result history'),
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
                  'Reference ranges come from your lab report. Your doctor interprets them for you.',
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

/// Latest heart, blood pressure, oxygen, temperature, weight and glucose readings from the phone.
class _Vitals extends ConsumerWidget {
  const _Vitals();
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(_summaryProvider);
    return AsyncBody<List<Json>>(
      s,
      onRetry: () => ref.invalidate(_summaryProvider),
      builder: (all) {
        final list = all
            .where(
              (m) => const {'vitals', 'body', 'metabolic'}.contains(m['group']),
            )
            .toList();
        if (list.isEmpty) {
          return const Empty(
            Icons.favorite_border,
            'No vitals yet',
            body: 'Sync from your phone’s health app in the Devices tab.',
          );
        }
        return ListView(
          padding: const EdgeInsets.fromLTRB(14, 6, 14, 16),
          children: [
            for (final m in list)
              SCard(
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
                            [
                              '7-day avg ${fmtNum(numOf(m['mean_7_days']))}',
                              if (m['typical_adult_range'] is Map)
                                'typical ${fmtNum(numOf((m['typical_adult_range'] as Map)['low']))}–${fmtNum(numOf((m['typical_adult_range'] as Map)['high']))}',
                            ].join(' · '),
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
                      style: TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w700,
                        color: m['outside_typical_range'] == true
                            ? Pal.warn
                            : null,
                      ),
                    ),
                  ],
                ),
              ),
            const Padding(
              padding: EdgeInsets.all(8),
              child: Text(
                'Typical ranges are general adult ranges, not a diagnosis. Device readings are not lab results.',
                style: TextStyle(fontSize: 10, color: Pal.mut),
              ),
            ),
          ],
        );
      },
    );
  }
}
