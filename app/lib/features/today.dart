import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'agent_screens.dart';
import 'health.dart';
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
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SectionTitle(
          'Latest lab results',
          trailing: flagged == 0
              ? const Pill('All in range', fg: Pal.ok, bg: Pal.okBg)
              : Pill('$flagged outside range', fg: Pal.warn, bg: Pal.warnBg),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(2, 0, 2, 8),
          child: Row(
            children: [
              Expanded(
                flex: list.length - flagged == 0 ? 1 : list.length - flagged,
                child: Container(height: 6, color: Pal.ok),
              ),
              if (flagged > 0) ...[
                const SizedBox(width: 2),
                Expanded(
                  flex: flagged,
                  child: Container(height: 6, color: Pal.warn),
                ),
              ],
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
        const BriefingCard(),
        const WellbeingCard(),
        const TodaysPlan(),
        const MedicinesTodayCard(),
        const HabitTrends(),
        const _KeyResults(),
        const SectionTitle('Health record'),
        SCard(
          child: Row(
            children: [
              Ring(value: score / 100, label: '${score.round()}%', size: 56),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '${sum['documents'] ?? 0} reports · ${sum['biomarkers_tracked'] ?? 0} biomarkers tracked',
                      style: const TextStyle(
                        fontSize: 12.5,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Text(
                      missing.isEmpty
                          ? 'Your health record is complete.'
                          : 'Complete your record: ${missing.first['label']}',
                      style: const TextStyle(fontSize: 11, color: Pal.mut),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
        SCard(
          onTap: () => _go(context, const CarePlanScreen()),
          child: Row(
            children: [
              const Icon(Icons.checklist, color: Pal.pri, size: 20),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'Care plan',
                      style: TextStyle(
                        fontSize: 12.5,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      [
                        if (goals.isEmpty)
                          'No treatment goals yet'
                        else
                          '$onTrack of ${goals.length} goals on target',
                        '${open?.length ?? sum['open_follow_ups'] ?? 0} follow-ups pending',
                      ].join(' · '),
                      style: const TextStyle(fontSize: 11, color: Pal.mut),
                    ),
                    if (next.isNotEmpty)
                      Text(
                        'Next follow-up: ${next.first['title']} · ${next.first['due_on']}',
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                  ],
                ),
              ),
              const Icon(Icons.chevron_right, color: Pal.mut),
            ],
          ),
        ),
        const SectionTitle('Care services'),
        GridView.count(
          crossAxisCount: 2,
          shrinkWrap: true,
          physics: const NeverScrollableScrollPhysics(),
          mainAxisSpacing: 8,
          crossAxisSpacing: 8,
          childAspectRatio: 1.55,
          children: [
            _Service(
              Icons.description_outlined,
              'Consultation Summary',
              'Results and questions, ready for your doctor',
              () => _go(context, const VisitPrepScreen()),
            ),
            _Service(
              Icons.medication_outlined,
              'Medications',
              'Dose schedule, reminders and drug information',
              () => _go(context, const PrescriptionsScreen()),
            ),
            _Service(
              Icons.local_hospital_outlined,
              'Care Locator',
              'Hospitals, doctors and emergency care near you',
              () => _go(context, const FindCareScreen()),
            ),
            _Service(
              Icons.family_restroom,
              'Family Profiles',
              'Manage health for parents and children',
              () => _go(context, const FamilyScreen()),
            ),
          ],
        ),
      ],
    );
  }
}

class _Service extends StatelessWidget {
  const _Service(this.icon, this.title, this.body, this.onTap);
  final IconData icon;
  final String title, body;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => InkWell(
    borderRadius: BorderRadius.circular(12),
    onTap: onTap,
    child: Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Pal.card,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, color: Pal.pri, size: 20),
          const Spacer(),
          Text(
            title,
            style: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 2),
          Text(
            body,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontSize: 10.5, color: Pal.mut),
          ),
        ],
      ),
    ),
  );
}
