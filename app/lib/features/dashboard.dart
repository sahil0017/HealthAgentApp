import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'health.dart';
import 'insights.dart' show insightColor, insightIcon;
import 'plan.dart';
import 'summary.dart' show wellbeingProvider;

List<Json> _maps(Object? v) => (v as List? ?? [])
    .whereType<Map>()
    .map((e) => e.cast<String, dynamic>())
    .toList();

String _flag(Json b) => '${(b['latest'] as Map?)?['flag'] ?? ''}';

/// Three numbers that say how things stand: how many results are in range, the wellbeing score
/// and what is waiting in the care plan. Each is a tile with its own ring or bar.
class SnapshotRow extends ConsumerWidget {
  const SnapshotRow({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final labs =
        ref.watch(profileListProvider('/biomarkers/summary')).asData?.value ??
        const <Json>[];
    final score = numOf(ref.watch(wellbeingProvider).asData?.value['score']);
    final open = ref
        .watch(profileListProvider('/follow-ups'))
        .asData
        ?.value
        .where((f) => f['status'] == 'open')
        .length;
    final out = labs.where((b) => outOfRange(_flag(b))).length;
    final inr = labs.where((b) => inRange(_flag(b))).length;
    final rated = out + inr;
    final pct = rated == 0 ? null : inr / rated;
    Widget tile(Widget lead, String big, String small) => Expanded(
      child: Container(
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(
          color: Pal.card,
          borderRadius: BorderRadius.circular(14),
        ),
        child: Column(
          children: [
            lead,
            const SizedBox(height: 6),
            Text(
              big,
              style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
            ),
            Text(
              small,
              textAlign: TextAlign.center,
              style: const TextStyle(fontSize: 10, color: Pal.mut),
            ),
          ],
        ),
      ),
    );
    Color band(double v) => v >= 75
        ? Pal.ok
        : v >= 50
        ? Pal.warn
        : Pal.bad;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        children: [
          tile(
            Ring(
              value: pct ?? 0,
              label: pct == null ? '–' : '${(pct * 100).round()}%',
              size: 46,
              color: out == 0 ? Pal.ok : Pal.warn,
            ),
            pct == null ? 'No ranges yet' : '$inr of $rated',
            'results in range',
          ),
          const SizedBox(width: 8),
          tile(
            Ring(
              value: ((score ?? 0) / 100).toDouble(),
              label: score == null ? '–' : '${score.round()}',
              size: 46,
              color: band((score ?? 0).toDouble()),
            ),
            score == null
                ? 'Not scored'
                : score >= 75
                ? 'Doing well'
                : score >= 50
                ? 'Room to improve'
                : 'Needs attention',
            'wellbeing score',
          ),
          const SizedBox(width: 8),
          tile(
            CircleAvatar(
              radius: 23,
              backgroundColor: Pal.priBg,
              child: Text(
                '${open ?? 0}',
                style: const TextStyle(
                  fontSize: 17,
                  fontWeight: FontWeight.w700,
                  color: Pal.pri,
                ),
              ),
            ),
            (open ?? 0) == 0 ? 'All clear' : 'To do',
            'care plan items',
          ),
        ],
      ),
    );
  }
}

/// Written readings of the person's own results and habits as a row of cards, so the front page
/// says what matters without a wall of text. Tapping a card opens the full reading and, where it
/// makes sense, adds it to the care plan.
class InsightCards extends ConsumerWidget {
  const InsightCards({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final labs = ref.watch(profileMapProvider('/agent/insights/labs'));
    final life = ref.watch(profileMapProvider('/agent/insights/lifestyle'));
    final items = [
      ..._maps(labs.asData?.value['items']),
      ..._maps(life.asData?.value['items']),
    ];
    const order = {'act': 0, 'watch': 1, 'info': 2, 'good': 3};
    items.sort(
      (a, b) =>
          (order['${a['tone']}'] ?? 2).compareTo(order['${b['tone']}'] ?? 2),
    );
    if (items.isEmpty) {
      return labs.isLoading || life.isLoading
          ? const SCard(
              child: Row(
                children: [
                  SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  SizedBox(width: 10),
                  Text(
                    'Reading your records…',
                    style: TextStyle(fontSize: 12, color: Pal.mut),
                  ),
                ],
              ),
            )
          : const SizedBox.shrink();
    }
    final headline = '${labs.asData?.value['headline'] ?? ''}';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SectionTitle(
          'Insights for you',
          trailing: labs.asData?.value['source'] == 'ai'
              ? const Pill('Sehat AI')
              : null,
        ),
        if (headline.isNotEmpty)
          Padding(
            padding: const EdgeInsets.fromLTRB(2, 0, 2, 8),
            child: Text(
              headline,
              style: const TextStyle(
                fontSize: 13,
                color: Pal.mut,
                height: 1.35,
              ),
            ),
          ),
        SizedBox(
          height: 138,
          child: ListView.separated(
            scrollDirection: Axis.horizontal,
            itemCount: items.length.clamp(0, 8),
            separatorBuilder: (_, _) => const SizedBox(width: 8),
            itemBuilder: (context, i) {
              final it = items[i];
              final tone = '${it['tone']}';
              final c = insightColor(tone);
              return InkWell(
                borderRadius: BorderRadius.circular(14),
                onTap: () => _open(context, ref, it),
                child: Container(
                  width: 220,
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: Pal.card,
                    borderRadius: BorderRadius.circular(14),
                    border: Border(left: BorderSide(color: c, width: 3)),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Icon(insightIcon(tone), size: 15, color: c),
                          const SizedBox(width: 6),
                          Text(
                            switch (tone) {
                              'act' => 'Act on this',
                              'watch' => 'Keep an eye',
                              'good' => 'Going well',
                              _ => 'Good to know',
                            },
                            style: TextStyle(
                              fontSize: 10.5,
                              fontWeight: FontWeight.w700,
                              color: c,
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 6),
                      Text(
                        '${it['title']}',
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 13,
                          fontWeight: FontWeight.w700,
                          height: 1.25,
                        ),
                      ),
                      const SizedBox(height: 4),
                      Expanded(
                        child: Text(
                          '${it['detail'] ?? ''}',
                          maxLines: 3,
                          overflow: TextOverflow.ellipsis,
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
              );
            },
          ),
        ),
      ],
    );
  }

  void _open(BuildContext context, WidgetRef ref, Json it) {
    final tone = '${it['tone']}';
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (ctx) => Padding(
        padding: const EdgeInsets.fromLTRB(18, 0, 18, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(insightIcon(tone), color: insightColor(tone)),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    '${it['title']}',
                    style: const TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            Text(
              '${it['detail'] ?? ''}',
              style: const TextStyle(fontSize: 13, height: 1.45),
            ),
            if (tone == 'act' || tone == 'watch') ...[
              const SizedBox(height: 16),
              FilledButton.icon(
                icon: const Icon(Icons.add_task, size: 18),
                label: const Text('Add to my care plan'),
                onPressed: () async {
                  Navigator.pop(ctx);
                  try {
                    await ref.read(apiProvider).post(
                      '/profiles/${ref.read(profileIdProvider)}/follow-ups',
                      {'kind': 'other', 'title': '${it['title']}'},
                    );
                    refreshCarePlan(ref);
                    if (context.mounted) {
                      toast(context, 'Added to your care plan');
                    }
                  } catch (e) {
                    if (context.mounted) toast(context, friendly(e));
                  }
                },
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// The tests that moved most since the previous report, with the size and direction of the move.
class WhatChanged extends ConsumerWidget {
  const WhatChanged({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final labs =
        ref.watch(profileListProvider('/biomarkers/summary')).asData?.value ??
        const <Json>[];
    final moves = <(Json, double, double)>[];
    for (final b in labs) {
      final prev = numOf(b['previous_value']);
      final now = numOf((b['latest'] as Map?)?['value']);
      if (prev == null || now == null || prev == 0) continue;
      final pct = (now - prev) / prev.abs() * 100;
      if (pct.abs() >= 1) moves.add((b, pct, now.toDouble()));
    }
    if (moves.isEmpty) return const SizedBox.shrink();
    moves.sort((a, b) => b.$2.abs().compareTo(a.$2.abs()));
    final top = moves.take(5).toList();
    final biggest = top.first.$2.abs();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SectionTitle('What changed since last report'),
        SCard(
          child: Column(
            children: [
              for (final (b, pct, now) in top)
                InkWell(
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => BiomarkerScreen(
                        '${b['biomarker_code']}',
                        '${b['display_name']}',
                      ),
                    ),
                  ),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 6),
                    child: Row(
                      children: [
                        Icon(
                          pct > 0 ? Icons.arrow_upward : Icons.arrow_downward,
                          size: 16,
                          color: outOfRange(_flag(b)) ? Pal.warn : Pal.ok,
                        ),
                        const SizedBox(width: 8),
                        Expanded(
                          flex: 5,
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                '${b['display_name']}',
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                  fontSize: 12.5,
                                  fontWeight: FontWeight.w600,
                                ),
                              ),
                              Text(
                                '${fmtNum(numOf(b['previous_value']))} → ${fmtNum(now)} ${b['unit'] ?? ''}',
                                style: const TextStyle(
                                  fontSize: 10.5,
                                  color: Pal.mut,
                                ),
                              ),
                            ],
                          ),
                        ),
                        Expanded(
                          flex: 3,
                          child: ClipRRect(
                            borderRadius: BorderRadius.circular(3),
                            child: LinearProgressIndicator(
                              value: (pct.abs() / biggest).clamp(0.05, 1),
                              minHeight: 6,
                              backgroundColor: Pal.line,
                              color: outOfRange(_flag(b)) ? Pal.warn : Pal.pri,
                            ),
                          ),
                        ),
                        SizedBox(
                          width: 54,
                          child: Text(
                            '${pct > 0 ? '+' : ''}${pct.toStringAsFixed(pct.abs() < 10 ? 1 : 0)}%',
                            textAlign: TextAlign.end,
                            style: const TextStyle(
                              fontSize: 12,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ),
      ],
    );
  }
}
