import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'health.dart';
import 'more.dart' show LifestyleScreen;
import 'plan.dart';

String _day() {
  final n = DateTime.now();
  return '${n.year}-${n.month.toString().padLeft(2, '0')}-${n.day.toString().padLeft(2, '0')}';
}

/// The Summary analytics: wellbeing score, habit projections and today's plan, computed server-side from the person's own data.
final wellbeingProvider = FutureProvider.autoDispose<Json>((ref) {
  final id = ref.watch(profileIdProvider);
  return ref
      .read(apiProvider)
      .map('/profiles/$id/agent/wellbeing?day=${_day()}');
});

/// Plan items ticked off in this session.
class _Done extends Notifier<Set<String>> {
  @override
  Set<String> build() => <String>{};
  void toggle(String id) =>
      state = state.contains(id) ? ({...state}..remove(id)) : {...state, id};
}

final _doneProvider = NotifierProvider<_Done, Set<String>>(_Done.new);

List<Json> _maps(Object? v) => (v as List? ?? [])
    .whereType<Map>()
    .map((e) => e.cast<String, dynamic>())
    .toList();

void _push(BuildContext c, Widget w) =>
    Navigator.push(c, MaterialPageRoute<void>(builder: (_) => w));

const _partNames = {
  'labs': ('Lab results', Icons.biotech_outlined),
  'habits': ('Daily habits', Icons.self_improvement),
  'care': ('Care plan', Icons.checklist),
};

(String, Color) _band(double s) => s >= 75
    ? ('Strong', Pal.ok)
    : s >= 50
    ? ('Steady', Pal.pri)
    : ('Needs attention', Pal.warn);

class WellbeingCard extends ConsumerWidget {
  const WellbeingCard({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final d = ref.watch(wellbeingProvider).asData?.value;
    if (d == null) return const SizedBox.shrink();
    final parts = _maps(d['parts']);
    if (parts.isEmpty) {
      return const SCard(
        child: Row(
          children: [
            Icon(Icons.insights, color: Pal.pri),
            SizedBox(width: 10),
            Expanded(
              child: Text(
                'Upload a report or log today’s sleep and your wellbeing score appears here.',
                style: TextStyle(fontSize: 12, height: 1.35),
              ),
            ),
          ],
        ),
      );
    }
    final score = (numOf(d['score']) ?? 0).toDouble();
    final (label, color) = _band(score);
    return SCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Ring(
                value: score / 100,
                label: '${score.round()}',
                size: 72,
                color: color,
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'Wellbeing score',
                      style: TextStyle(fontSize: 11, color: Pal.mut),
                    ),
                    Text(
                      label,
                      style: TextStyle(
                        fontSize: 18,
                        fontWeight: FontWeight.w700,
                        color: color,
                      ),
                    ),
                    Text(
                      '${d['basis'] ?? ''}',
                      maxLines: 3,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 10,
                        color: Pal.mut,
                        height: 1.3,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          for (final p in parts)
            Padding(
              padding: const EdgeInsets.only(bottom: 6),
              child: Row(
                children: [
                  Icon(
                    (_partNames[p['key']]?.$2) ?? Icons.circle,
                    size: 14,
                    color: Pal.mut,
                  ),
                  const SizedBox(width: 8),
                  SizedBox(
                    width: 82,
                    child: Text(
                      _partNames[p['key']]?.$1 ?? '${p['key']}',
                      style: const TextStyle(fontSize: 11.5),
                    ),
                  ),
                  Expanded(
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(4),
                      child: LinearProgressIndicator(
                        value: ((numOf(p['score']) ?? 0) / 100).toDouble(),
                        minHeight: 7,
                        color: _band((numOf(p['score']) ?? 0).toDouble()).$2,
                        backgroundColor: Pal.line,
                      ),
                    ),
                  ),
                  SizedBox(
                    width: 36,
                    child: Text(
                      '${numOf(p['score'])?.round() ?? 0}',
                      textAlign: TextAlign.end,
                      style: const TextStyle(
                        fontSize: 11.5,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

const _kindIcons = {
  'follow_up': Icons.event_repeat,
  'goal': Icons.flag_outlined,
  'lab': Icons.biotech_outlined,
  'habit': Icons.trending_up,
  'log': Icons.edit_note,
};

class TodaysPlan extends ConsumerWidget {
  const TodaysPlan({super.key});

  void _open(BuildContext c, Json action) {
    switch ('${action['type']}') {
      case 'biomarker':
        _push(
          c,
          BiomarkerScreen('${action['ref']}', '${action['name'] ?? 'Result'}'),
        );
      case 'follow_ups':
        _push(c, const CarePlanScreen());
      default:
        _push(c, const LifestyleScreen());
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final d = ref.watch(wellbeingProvider).asData?.value;
    if (d == null || _maps(d['parts']).isEmpty) return const SizedBox.shrink();
    final tasks = _maps(d['tasks']);
    final done = ref.watch(_doneProvider);
    final finished = tasks.where((t) => done.contains(t['id'])).length;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SectionTitle(
          'Today’s plan',
          trailing: tasks.isEmpty
              ? null
              : Pill(
                  '$finished of ${tasks.length} done',
                  fg: finished == tasks.length ? Pal.ok : Pal.pri,
                  bg: finished == tasks.length ? Pal.okBg : Pal.priBg,
                ),
        ),
        if (tasks.isEmpty)
          const SCard(
            color: Pal.okBg,
            child: Row(
              children: [
                Icon(Icons.check_circle, color: Pal.ok, size: 18),
                SizedBox(width: 10),
                Expanded(
                  child: Text(
                    'You are on track. Nothing needs doing today.',
                    style: TextStyle(fontSize: 12),
                  ),
                ),
              ],
            ),
          ),
        for (final t in tasks)
          Builder(
            builder: (_) {
              final isDone = done.contains(t['id']);
              return SCard(
                onTap: () => _open(context, (t['action'] as Map).cast()),
                padding: const EdgeInsets.fromLTRB(4, 6, 10, 6),
                child: Row(
                  children: [
                    IconButton(
                      visualDensity: VisualDensity.compact,
                      icon: Icon(
                        isDone
                            ? Icons.check_circle
                            : Icons.radio_button_unchecked,
                        color: isDone ? Pal.ok : Pal.mut,
                      ),
                      onPressed: () {
                        ref.read(_doneProvider.notifier).toggle('${t['id']}');
                      },
                    ),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '${t['title']}',
                            style: TextStyle(
                              fontSize: 12.5,
                              fontWeight: FontWeight.w600,
                              decoration: isDone
                                  ? TextDecoration.lineThrough
                                  : null,
                              color: isDone ? Pal.mut : null,
                            ),
                          ),
                          const SizedBox(height: 2),
                          Text(
                            '${t['why']}',
                            style: const TextStyle(
                              fontSize: 10.5,
                              color: Pal.mut,
                              height: 1.3,
                            ),
                          ),
                        ],
                      ),
                    ),
                    Icon(
                      _kindIcons['${t['kind']}'] ?? Icons.chevron_right,
                      size: 18,
                      color: Pal.pri,
                    ),
                  ],
                ),
              );
            },
          ),
      ],
    );
  }
}

/// Seven-day habit analytics: 14 days of bars, this week against the person's usual, and next week's projection.
class HabitTrends extends ConsumerWidget {
  const HabitTrends({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final d = ref.watch(wellbeingProvider).asData?.value;
    if (d == null) return const SizedBox.shrink();
    final habits = _maps(d['habits']).where((h) => h['avg_7'] != null).toList();
    if (habits.isEmpty) return const SizedBox.shrink();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SectionTitle('Habit trends'),
        SizedBox(
          height: 156,
          child: ListView.separated(
            scrollDirection: Axis.horizontal,
            itemCount: habits.length,
            separatorBuilder: (_, _) => const SizedBox(width: 8),
            itemBuilder: (_, i) => _HabitCard(
              habits[i],
              onTap: () => _push(context, const LifestyleScreen()),
            ),
          ),
        ),
        const SizedBox(height: 8),
      ],
    );
  }
}

class _HabitCard extends StatelessWidget {
  const _HabitCard(this.h, {required this.onTap});
  final Json h;
  final VoidCallback onTap;

  String _v(Object? v) {
    final n = numOf(v);
    if (n == null) return '–';
    final s = n % 1 == 0 ? n.toInt().toString() : n.toStringAsFixed(1);
    return h['unit'] == 'score' ? '$s/5' : s;
  }

  @override
  Widget build(BuildContext context) {
    final dir = '${h['direction']}';
    final (txt, col, bg, icon) = switch (dir) {
      'improving' => ('Improving', Pal.ok, Pal.okBg, Icons.trending_up),
      'slipping' => ('Slipping', Pal.warn, Pal.warnBg, Icons.trending_down),
      'steady' => ('Steady', Pal.pri, Pal.priBg, Icons.trending_flat),
      _ => ('Building trend', Pal.mut, Pal.line, Icons.hourglass_empty),
    };
    final daily = _maps(h['daily']);
    final vals = [for (final p in daily) numOf(p['v'])?.toDouble()];
    final top = vals.whereType<double>().fold<double>(
      0,
      (a, b) => b > a ? b : a,
    );
    return InkWell(
      borderRadius: BorderRadius.circular(12),
      onTap: onTap,
      child: Container(
        width: 172,
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(
          color: Pal.card,
          borderRadius: BorderRadius.circular(12),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    '${h['name']}',
                    style: const TextStyle(fontSize: 11, color: Pal.mut),
                  ),
                ),
                Icon(icon, size: 14, color: col),
              ],
            ),
            Row(
              crossAxisAlignment: CrossAxisAlignment.baseline,
              textBaseline: TextBaseline.alphabetic,
              children: [
                Text(
                  _v(h['avg_7']),
                  style: const TextStyle(
                    fontSize: 20,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(width: 4),
                Expanded(
                  child: Text(
                    h['unit'] == 'score'
                        ? '7-day avg'
                        : '${h['unit']} · 7-day avg',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(fontSize: 9.5, color: Pal.mut),
                  ),
                ),
              ],
            ),
            const Spacer(),
            SizedBox(
              height: 40,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  for (var i = 0; i < vals.length; i++)
                    Expanded(
                      child: Padding(
                        padding: const EdgeInsets.symmetric(horizontal: 1),
                        child: Container(
                          height: vals[i] == null || top == 0
                              ? 3
                              : (vals[i]! / top * 40).clamp(3, 40),
                          decoration: BoxDecoration(
                            color: vals[i] == null
                                ? Pal.line
                                : i == vals.length - 1
                                ? Pal.pri
                                : Pal.pri.withValues(alpha: .55),
                            borderRadius: BorderRadius.circular(2),
                          ),
                        ),
                      ),
                    ),
                ],
              ),
            ),
            const SizedBox(height: 6),
            Row(
              children: [
                Pill(txt, fg: col, bg: bg),
                const SizedBox(width: 6),
                if (h['projected_7d'] != null)
                  Expanded(
                    child: Text(
                      'Next wk ≈ ${_v(h['projected_7d'])}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      textAlign: TextAlign.end,
                      style: const TextStyle(fontSize: 9.5, color: Pal.mut),
                    ),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
