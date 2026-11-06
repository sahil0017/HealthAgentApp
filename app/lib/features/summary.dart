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

class WellbeingCard extends ConsumerWidget {
  const WellbeingCard({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final value = ref.watch(wellbeingProvider);
    if (value.hasError) {
      return SCard(
        child: Row(
          children: [
            const Expanded(child: Text('Your wellbeing score could not load.')),
            TextButton(
              onPressed: () => ref.invalidate(wellbeingProvider),
              child: const Text('Retry'),
            ),
          ],
        ),
      );
    }
    if (value.isLoading) {
      return const SCard(child: Text('Loading your wellbeing score…'));
    }
    final d = value.requireValue;
    final parts = _maps(d['parts']);
    if (parts.isEmpty) {
      return SCard(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Row(
              children: [
                Icon(Icons.insights, color: Pal.pri),
                SizedBox(width: 10),
                Expanded(
                  child: Text(
                    'No score yet. Verify a report with reference ranges, or log habits over several days to build a personal baseline.',
                    style: TextStyle(fontSize: 12, height: 1.35),
                  ),
                ),
              ],
            ),
            TextButton(
              onPressed: () => _push(context, const LifestyleScreen()),
              child: const Text('Log habits'),
            ),
          ],
        ),
      );
    }
    final score = numOf(d['score']);
    if (score == null) {
      return const SCard(child: Text('Your wellbeing score is unavailable.'));
    }
    final labs = (d['labs'] as Map?)?.cast<String, dynamic>() ?? {};
    final care = (d['care'] as Map?)?.cast<String, dynamic>() ?? {};
    final included = parts.map((p) => '${p['key']}').toSet();
    final totalWeight = parts.fold<double>(
      0,
      (sum, p) => sum + (numOf(p['weight'])?.toDouble() ?? 0),
    );
    String detail(String key) {
      if (key == 'labs') {
        if (numOf(labs['in_range']) == null ||
            numOf(labs['out_of_range']) == null) {
          return 'Based on results with reference ranges.';
        }
        final inside = numOf(labs['in_range'])?.toInt() ?? 0;
        final outside = numOf(labs['out_of_range'])?.toInt() ?? 0;
        final unrated = numOf(labs['unrated'])?.toInt() ?? 0;
        return '$inside of ${inside + outside} results with ranges are in range'
            '${unrated > 0 ? '; $unrated without a range not scored' : ''}.';
      }
      if (key == 'care') {
        final assessed = numOf(care['assessed_goals'])?.toInt() ?? 0;
        final goals = numOf(care['goals'])?.toInt() ?? 0;
        final onTarget = numOf(care['on_target'])?.toInt() ?? 0;
        final dated = numOf(care['dated_follow_ups'])?.toInt() ?? 0;
        final overdue = numOf(care['overdue_follow_ups'])?.toInt();
        return [
          if (assessed > 0) '$onTarget of $assessed measurable goals on target',
          if (goals > assessed)
            '${goals - assessed} goals without comparable results',
          if (dated > 0 && overdue != null)
            '$overdue of $dated dated follow-ups overdue',
          if (dated > 0 && overdue == null)
            '$dated dated follow-ups considered',
        ].join(' · ');
      }
      if (key == 'habits') {
        return 'Based on recent logging and averages compared with your own best days.';
      }
      return 'Based on available information.';
    }

    final missing = [
      if (!included.contains('labs')) 'lab results with reference ranges',
      if (!included.contains('habits')) 'habit history',
      if (!included.contains('care')) 'measurable goals or dated follow-ups',
    ];
    return SCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Ring(
                value: (score / 100).clamp(0, 1).toDouble(),
                label: '${score.round()}',
                size: 72,
                color: Pal.pri,
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
                      'Your personal snapshot',
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    Text(
                      'Based on ${parts.length} of 3 areas. Not a diagnosis or a measure of overall health.',
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
              padding: const EdgeInsets.only(bottom: 10),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Icon(
                        _partNames[p['key']]?.$2 ?? Icons.circle,
                        size: 15,
                        color: Pal.pri,
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          _partNames[p['key']]?.$1 ?? '${p['key']}',
                          style: const TextStyle(
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                      Text(
                        '${numOf(p['score'])?.round() ?? '–'}/100'
                        '${totalWeight > 0 ? ' · ${((numOf(p['weight']) ?? 0) / totalWeight * 100).round()}% weight' : ''}',
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                    ],
                  ),
                  Padding(
                    padding: const EdgeInsets.only(left: 23, top: 3),
                    child: Text(
                      detail('${p['key']}'),
                      style: const TextStyle(
                        fontSize: 11,
                        color: Pal.mut,
                        height: 1.3,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          if (missing.isNotEmpty)
            Text(
              'Not included: ${missing.join(', ')}. Available areas are reweighted.',
              style: const TextStyle(
                fontSize: 10.5,
                color: Pal.mut,
                height: 1.35,
              ),
            ),
          if (!included.contains('habits') || !included.contains('care'))
            Wrap(
              spacing: 8,
              children: [
                if (!included.contains('habits'))
                  TextButton(
                    onPressed: () => _push(context, const LifestyleScreen()),
                    child: const Text('Log habits'),
                  ),
                if (!included.contains('care'))
                  TextButton(
                    onPressed: () => _push(context, const CarePlanScreen()),
                    child: const Text('Open care plan'),
                  ),
              ],
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
                    'No actions suggested for today. This does not mean every area has been assessed.',
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
