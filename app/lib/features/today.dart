import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'agent_screens.dart';
import 'dashboard.dart';
import 'health.dart';
import 'lab_reports.dart' show labReportsProvider;
import 'notifications.dart';
import 'find_care.dart';
import 'plan.dart';
import 'prescriptions.dart';
import 'records.dart';
import 'summary.dart';

class TodayTab extends ConsumerWidget {
  const TodayTab({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final ov = ref.watch(profileMapProvider('/overview'));
    return RefreshIndicator(
      onRefresh: () async {
        refreshCarePlan(ref);
        ref.invalidate(wellbeingProvider);
        ref.invalidate(doseScheduleProvider);
        ref.invalidate(profileListProvider('/biomarkers/summary'));
        ref.invalidate(labReportsProvider);
        ref.invalidate(profileMapProvider('/agent/insights/labs'));
        ref.invalidate(profileMapProvider('/agent/insights/lifestyle'));
        await ref.read(profileMapProvider('/overview').future);
      },
      child: AsyncBody<Json>(
        ov,
        onRetry: () => ref.invalidate(profileMapProvider('/overview')),
        builder: (d) => _Body(d),
      ),
    );
  }
}

List<Json> _list(Object? v) => (v as List? ?? [])
    .whereType<Map>()
    .map((e) => e.cast<String, dynamic>())
    .toList();

void _go(BuildContext context, Widget w) =>
    Navigator.push(context, MaterialPageRoute<void>(builder: (_) => w));

/// Top results as small visual cards: out-of-range tests first, each with its place in the reference range.
class _KeyResults extends ConsumerWidget {
  const _KeyResults();
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref
        .watch(profileListProvider('/biomarkers/summary'))
        .asData
        ?.value;
    if (list == null || list.isEmpty) return const SizedBox.shrink();
    int rank(Json b) =>
        outOfRange((b['latest'] as Map?)?['flag'] as String?) ? 0 : 1;
    final top = [...list]..sort((a, b) => rank(a) - rank(b));
    final flagged = list.where((b) => rank(b) == 0).length;
    final inRangeCount = list
        .where((b) => inRange((b['latest'] as Map?)?['flag'] as String?))
        .length;
    final unrated = list.length - flagged - inRangeCount;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SectionTitle(
          'Latest lab results',
          trailing: flagged > 0
              ? Pill('$flagged outside range', fg: Pal.warn, bg: Pal.warnBg)
              : unrated > 0
              ? Text(
                  '$unrated without a range',
                  style: const TextStyle(fontSize: 10.5, color: Pal.mut),
                )
              : const Pill('All in range', fg: Pal.ok, bg: Pal.okBg),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(2, 0, 2, 8),
          child: Row(
            children: [
              if (inRangeCount > 0)
                Expanded(
                  flex: inRangeCount,
                  child: Container(height: 6, color: Pal.ok),
                ),
              if (flagged > 0)
                Expanded(
                  flex: flagged,
                  child: Container(height: 6, color: Pal.warn),
                ),
              if (unrated > 0)
                Expanded(
                  flex: unrated,
                  child: Container(height: 6, color: Pal.line),
                ),
            ],
          ),
        ),
        SizedBox(
          height: 112,
          child: ListView.separated(
            scrollDirection: Axis.horizontal,
            itemCount: top.length.clamp(0, 8),
            separatorBuilder: (_, _) => const SizedBox(width: 8),
            itemBuilder: (context, i) {
              final b = top[i];
              final latest = (b['latest'] as Map?)?.cast<String, dynamic>();
              final range = (latest?['range'] as Map?)?.cast<String, dynamic>();
              final v = numOf(latest?['value']);
              return InkWell(
                borderRadius: BorderRadius.circular(12),
                onTap: () => _go(
                  context,
                  BiomarkerScreen(
                    '${b['biomarker_code']}',
                    '${b['display_name']}',
                  ),
                ),
                child: Container(
                  width: 150,
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: Pal.card,
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '${b['display_name']}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                      const SizedBox(height: 4),
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.baseline,
                        textBaseline: TextBaseline.alphabetic,
                        children: [
                          Text(
                            fmtNum(v),
                            style: const TextStyle(
                              fontSize: 20,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                          const SizedBox(width: 4),
                          Expanded(
                            child: Text(
                              '${b['unit'] ?? ''}',
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(
                                fontSize: 10,
                                color: Pal.mut,
                              ),
                            ),
                          ),
                        ],
                      ),
                      const Spacer(),
                      if (v != null)
                        RangeBar(
                          value: v.toDouble(),
                          low: numOf(range?['low'])?.toDouble(),
                          high: numOf(range?['high'])?.toDouble(),
                        ),
                      const SizedBox(height: 6),
                      Pill.flag(latest?['flag'] as String?),
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
}

class _Body extends ConsumerWidget {
  const _Body(this.d);
  final Json d;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sum = (d['summary'] as Map?)?.cast<String, dynamic>() ?? {};
    final comp = (d['completeness'] as Map?)?.cast<String, dynamic>() ?? {};
    final profile = (d['profile'] as Map?)?.cast<String, dynamic>() ?? {};
    final score = (numOf(comp['score']) ?? 0).toDouble();
    final goals = _list(d['goals']);
    final missing = _list(comp['items'])
        .where((i) => i['done'] != true)
        .toList();
    final open = ref
        .watch(profileListProvider('/follow-ups'))
        .asData
        ?.value
        .where((f) => f['status'] == 'open')
        .toList();
    final next = (open ?? []).where((f) => f['due_on'] != null).toList()
      ..sort((a, b) => '${a['due_on']}'.compareTo('${b['due_on']}'));
    final onTrack = goals
        .where((g) => '${(g['progress'] as Map?)?['status']}' == 'on_target')
        .length;
    final name = '${profile['display_name'] ?? ''}'.split(' ').first;

    return ListView(
      padding: const EdgeInsets.fromLTRB(14, 8, 14, 24),
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(2, 2, 2, 6),
          child: Text(
            name.isEmpty ? 'Today' : 'Hello, $name',
            style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
          ),
        ),
        const UrgentStrip(),
        const WellbeingCard(),
        const TodaysPlan(),
        const MedicinesTodayCard(),
        const SectionTitle('Explore your health'),
        _SummaryLink(
          icon: Icons.wb_sunny_outlined,
          title: 'Daily health brief',
          subtitle: 'A closer look at your health today',
          onTap: () => _go(context, const BriefingScreen()),
        ),
        _SummaryLink(
          icon: Icons.insights_outlined,
          title: 'Insights and trends',
          subtitle: 'Your results, changes and habits',
          onTap: () => _go(context, const _InsightsScreen()),
        ),
        const SectionTitle('Your health'),
        const _LatestResultsInsight(),
        const SizedBox(height: 4),
        IntrinsicHeight(
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Expanded(
                child: SCard(
                  padding: const EdgeInsets.all(10),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Ring(
                            value: score / 100,
                            label: '${score.round()}%',
                            size: 44,
                          ),
                          const SizedBox(width: 10),
                          const Expanded(
                            child: Text(
                              'Health record',
                              style: TextStyle(
                                fontSize: 12.5,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 6),
                      Text(
                        '${sum['documents'] ?? 0} reports · ${sum['biomarkers_tracked'] ?? 0} biomarkers',
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                      Text(
                        missing.isEmpty
                            ? (score >= 100
                                  ? 'Record complete'
                                  : 'Add more information to your record')
                            : 'Next: ${missing.first['label']}',
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: SCard(
                  padding: const EdgeInsets.all(10),
                  onTap: () => _go(context, const CarePlanScreen()),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Row(
                        children: [
                          Icon(Icons.checklist, color: Pal.pri, size: 20),
                          SizedBox(width: 8),
                          Expanded(
                            child: Text(
                              'Care plan',
                              style: TextStyle(
                                fontSize: 12.5,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                          ),
                          Icon(Icons.chevron_right, color: Pal.mut, size: 18),
                        ],
                      ),
                      const SizedBox(height: 6),
                      Text(
                        goals.isEmpty
                            ? 'No goals yet'
                            : '$onTrack on target · ${goals.length} active goals',
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                      Text(
                        next.isNotEmpty
                            ? 'Next: ${next.first['title']}'
                            : '${open?.length ?? sum['open_follow_ups'] ?? 0} follow-ups pending',
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
        const SectionTitle('Care services'),
        Row(
          children: [
            _Service(
              Icons.description_outlined,
              'Visit summary',
              () => _go(context, const VisitPrepScreen()),
            ),
            _Service(
              Icons.medication_outlined,
              'Medicines',
              () => _go(context, const PrescriptionsScreen()),
            ),
            _Service(
              Icons.local_hospital_outlined,
              'Care locator',
              () => _go(context, const FindCareScreen()),
            ),
            _Service(
              Icons.family_restroom,
              'Family',
              () => _go(context, const FamilyScreen()),
            ),
          ],
        ),
      ],
    );
  }
}

class _LatestResultsInsight extends ConsumerWidget {
  const _LatestResultsInsight();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final results = ref.watch(profileListProvider('/biomarkers/summary'));
    return results.when(
      loading: () => const SCard(child: Text('Loading your latest results…')),
      error: (_, _) => SCard(
        child: Row(
          children: [
            const Expanded(child: Text('Latest results could not load.')),
            TextButton(
              onPressed: () =>
                  ref.invalidate(profileListProvider('/biomarkers/summary')),
              child: const Text('Retry'),
            ),
          ],
        ),
      ),
      data: (list) {
        final outside = list
            .where((b) => outOfRange((b['latest'] as Map?)?['flag'] as String?))
            .toList();
        final inside = list
            .where((b) => inRange((b['latest'] as Map?)?['flag'] as String?))
            .length;
        final unknown = list.length - outside.length - inside;
        final text = list.isEmpty
            ? 'Verify a report to see your latest results here.'
            : outside.isNotEmpty
            ? '${outside.length} result${outside.length == 1 ? '' : 's'} outside the reference range. Review ${outside.take(2).map((b) => '${b['display_name']}').join(' and ')}.'
            : inside > 0
            ? '$inside result${inside == 1 ? '' : 's'} within the reference range.'
            : 'No results have a reference range for comparison.';
        return SCard(
          onTap: () => _go(
            context,
            Scaffold(
              appBar: AppBar(title: const Text('Lab Results')),
              body: const SafeArea(child: HealthTab()),
            ),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(
                Icons.biotech_outlined,
                size: 20,
                color: outside.isNotEmpty ? Pal.warn : Pal.pri,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'Latest lab results',
                      style: TextStyle(
                        fontSize: 12.5,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Text(
                      text,
                      style: const TextStyle(
                        fontSize: 11.5,
                        color: Pal.mut,
                        height: 1.35,
                      ),
                    ),
                    if (unknown > 0 && (outside.isNotEmpty || inside > 0))
                      Text(
                        '$unknown without a reference range.',
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                  ],
                ),
              ),
              const Icon(Icons.chevron_right, color: Pal.mut),
            ],
          ),
        );
      },
    );
  }
}

class _InsightsScreen extends StatelessWidget {
  const _InsightsScreen();

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Insights and trends')),
    body: ListView(
      padding: const EdgeInsets.fromLTRB(14, 8, 14, 24),
      children: const [
        Padding(
          padding: EdgeInsets.fromLTRB(2, 0, 2, 12),
          child: Text(
            'Explore insights from your results and daily habits.',
            style: TextStyle(fontSize: 12, color: Pal.mut),
          ),
        ),
        InsightCards(),
        _KeyResults(),
        WhatChanged(),
        HabitTrends(),
      ],
    ),
  );
}

class _SummaryLink extends StatelessWidget {
  const _SummaryLink({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.onTap,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => SCard(
    onTap: onTap,
    child: Row(
      children: [
        CircleAvatar(
          radius: 19,
          backgroundColor: Pal.priBg,
          child: Icon(icon, size: 19, color: Pal.pri),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                title,
                style: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 2),
              Text(
                subtitle,
                style: const TextStyle(fontSize: 11, color: Pal.mut),
              ),
            ],
          ),
        ),
        const Icon(Icons.chevron_right, color: Pal.mut),
      ],
    ),
  );
}

class _Service extends StatelessWidget {
  const _Service(this.icon, this.title, this.onTap);
  final IconData icon;
  final String title;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => Expanded(
    child: Padding(
      padding: const EdgeInsets.symmetric(horizontal: 3),
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 12, horizontal: 4),
          decoration: BoxDecoration(
            color: Pal.card,
            borderRadius: BorderRadius.circular(12),
          ),
          child: Column(
            children: [
              Icon(icon, color: Pal.pri, size: 22),
              const SizedBox(height: 6),
              Text(
                title,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.center,
                style: const TextStyle(
                  fontSize: 11,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}
