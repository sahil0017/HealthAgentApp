import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'insights.dart';
import 'lock.dart';
import 'environment.dart';
import 'find_care.dart';
import 'investigations.dart';
import 'places.dart';
import 'plan.dart';
import 'prescriptions.dart';
import 'records.dart';
import 'vault.dart' show DocumentScreen;
import 'summary.dart' show wellbeingProvider;

class MoreTab extends ConsumerWidget {
  const MoreTab({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    void open(Widget w) =>
        Navigator.push(context, MaterialPageRoute(builder: (_) => w));
    final groups = <(String, List<(IconData, String, String, Widget)>)>[
      (
        'CARE & TREATMENT',
        [
          (
            Icons.receipt_long_outlined,
            'Medications',
            'Prescriptions, dose schedule, drug information',
            const PrescriptionsScreen(),
          ),
          (
            Icons.checklist,
            'Care Plan',
            'Treatment goals and follow-up visits',
            const CarePlanScreen(),
          ),
          (
            Icons.place_outlined,
            'Care Locator',
            'Hospitals, doctors, labs and pharmacies near you',
            const FindCareScreen(),
          ),
          (
            Icons.emergency_outlined,
            'Emergency Profile',
            'Medical ID, emergency contacts, nearest ER',
            const EmergencyScreen(),
          ),
        ],
      ),
      (
        'HEALTH RECORD',
        [
          (
            Icons.timeline,
            'Clinical Timeline',
            'Reports, results, visits and symptoms in order',
            const TimelineScreen(),
          ),
          (
            Icons.medical_information_outlined,
            'Medical History',
            'Conditions, allergies, surgeries, vaccinations',
            const HistoryScreen(),
          ),
          (
            Icons.self_improvement,
            'Symptoms & Lifestyle',
            'Symptom diary, sleep, activity, nutrition, mood',
            const LifestyleScreen(),
          ),
          (
            Icons.travel_explore,
            'Health Research',
            'Questions to explore, backed by vetted sources',
            const InvestigationsScreen(),
          ),
        ],
      ),
      (
        'PUBLIC HEALTH',
        [
          (
            Icons.campaign_outlined,
            'Public Health Alerts',
            'Outbreaks, advisories and health news for your area',
            const HealthNewsScreen(),
          ),
          (
            Icons.air,
            'Environmental Health',
            'Air quality, heat and pollen near you',
            const EnvironmentScreen(),
          ),
        ],
      ),
      (
        'SHARING & PRIVACY',
        [
          (
            Icons.group_outlined,
            'Family & Caregiver Access',
            'Share your record read-only with people you trust',
            const FamilyScreen(),
          ),
          (
            Icons.hub_outlined,
            'Connected Apps',
            'Let other apps and AI tools read your record',
            const McpScreen(),
          ),
          (
            Icons.shield_outlined,
            'Privacy & Data Rights',
            'Consents, sign-in devices, retention, export, delete',
            const PrivacyScreen(),
          ),
        ],
      ),
    ];
    return ListView(
      padding: const EdgeInsets.fromLTRB(14, 4, 14, 16),
      children: [
        for (final g in groups) ...[
          Padding(
            padding: const EdgeInsets.fromLTRB(4, 12, 0, 6),
            child: Text(
              g.$1,
              style: const TextStyle(
                fontSize: 10.5,
                letterSpacing: 0.8,
                fontWeight: FontWeight.w700,
                color: Pal.mut,
              ),
            ),
          ),
          for (final i in g.$2)
            SCard(
              onTap: () => open(i.$4),
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
              child: Row(
                children: [
                  Container(
                    padding: const EdgeInsets.all(7),
                    decoration: BoxDecoration(
                      color: Pal.priBg,
                      borderRadius: BorderRadius.circular(9),
                    ),
                    child: Icon(i.$1, color: Pal.pri, size: 18),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          i.$2,
                          style: const TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        Text(
                          i.$3,
                          style: const TextStyle(
                            fontSize: 10.5,
                            color: Pal.mut,
                          ),
                        ),
                      ],
                    ),
                  ),
                  const Icon(Icons.chevron_right, size: 18, color: Pal.mut),
                ],
              ),
            ),
        ],
        const SizedBox(height: 10),
        OutlinedButton(
          onPressed: () => ref.read(authProvider.notifier).signOut(),
          child: const Text('Sign out'),
        ),
      ],
    );
  }
}

class _Page extends StatelessWidget {
  const _Page(this.title, this.child);
  final String title;
  final Widget child;
  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(title)),
    body: child,
  );
}

// ---------------------------------------------------------------- Lifestyle

class LifestyleScreen extends ConsumerStatefulWidget {
  const LifestyleScreen({super.key});
  @override
  ConsumerState<LifestyleScreen> createState() => _LifestyleState();
}

const _moods = ['😣', '😕', '😐', '🙂', '😄'];
const _symptomIdeas = [
  'Headache',
  'Fatigue',
  'Fever',
  'Cough',
  'Body ache',
  'Nausea',
  'Acidity',
  'Dizziness',
];

class _LifestyleState extends ConsumerState<LifestyleScreen> {
  final _done = <String>{};

  Future<void> _log(String cat, num value, {String? tick}) async {
    try {
      await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/lifestyle-logs',
        {'category': cat, 'value': value},
      );
      ref.invalidate(profileMapProvider('/lifestyle/summary?days=30'));
      ref.invalidate(profileMapProvider('/agent/insights/lifestyle'));
      ref.invalidate(wellbeingProvider);
      ref.invalidate(profileMapProvider('/lifestyle/correlations?days=60'));
      if (mounted) {
        setState(() => _done.add(tick ?? cat));
        toast(context, 'Logged for today.');
      }
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  Future<void> _logSymptom([String? preset]) async {
    final res = await showModalBottomSheet<(String, int)>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => _SymptomSheet(preset),
    );
    if (res == null) return;
    try {
      await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/symptoms',
        {'name': res.$1, 'severity': res.$2},
      );
      ref.invalidate(profileListProvider('/symptoms'));
      ref.invalidate(profileMapProvider('/agent/insights/lifestyle'));
      ref.invalidate(wellbeingProvider);
      if (mounted) toast(context, '${res.$1} noted.');
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  Widget _row(
    String label,
    IconData icon,
    List<Widget> chips, {
    bool ok = false,
  }) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(icon, size: 16, color: Pal.pri),
            const SizedBox(width: 6),
            Text(
              label,
              style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
            ),
            if (ok) ...[
              const SizedBox(width: 6),
              const Icon(Icons.check_circle, size: 14, color: Pal.ok),
            ],
          ],
        ),
        const SizedBox(height: 6),
        Wrap(spacing: 8, runSpacing: 6, children: chips),
      ],
    ),
  );

  Widget _chip(String text, VoidCallback onTap) => ActionChip(
    label: Text(text, style: const TextStyle(fontSize: 12)),
    onPressed: onTap,
  );

  @override
  Widget build(BuildContext context) {
    final sum = ref.watch(profileMapProvider('/lifestyle/summary?days=30'));
    final symptoms = ref.watch(profileListProvider('/symptoms'));
    final past = <String>[];
    for (final s in symptoms.value ?? const <Json>[]) {
      final n = '${s['name']}';
      if (!past.contains(n)) past.add(n);
    }
    final ideas = [
      ...past.take(4),
      ..._symptomIdeas.where((i) => !past.contains(i)),
    ].take(8).toList();
    return Scaffold(
      appBar: AppBar(title: const Text('Symptoms & Lifestyle')),
      body: ListView(
        padding: const EdgeInsets.all(14),
        children: [
          const SCard(
            color: Pal.priBg,
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(Icons.insights, color: Pal.pri, size: 20),
                SizedBox(width: 10),
                Expanded(
                  child: Text(
                    'A 20-second daily check-in. Sehat turns it into patterns, such as poor sleep before headaches, and a symptom summary you can hand to your doctor.',
                    style: TextStyle(fontSize: 12),
                  ),
                ),
              ],
            ),
          ),
          const InsightPanel(
            path: '/agent/insights/lifestyle',
            title: 'Your coach',
            icon: Icons.auto_awesome,
          ),
          const SectionTitle('Today’s check-in'),
          SCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _row('How is your mood?', Icons.mood, [
                  for (var i = 0; i < 5; i++)
                    InkWell(
                      onTap: () => _log('mood', i + 1),
                      child: Padding(
                        padding: const EdgeInsets.all(4),
                        child: Text(
                          _moods[i],
                          style: const TextStyle(fontSize: 26),
                        ),
                      ),
                    ),
                ], ok: _done.contains('mood')),
                _row('Stress level', Icons.spa_outlined, [
                  for (final e in const [
                    (1, 'Calm'),
                    (2, 'Mild'),
                    (3, 'Moderate'),
                    (4, 'High'),
                    (5, 'Very high'),
                  ])
                    _chip(e.$2, () => _log('stress', e.$1)),
                ], ok: _done.contains('stress')),
                _row('Sleep last night', Icons.bedtime_outlined, [
                  for (final h in const [5, 6, 7, 8, 9])
                    _chip('$h h', () => _log('sleep', h)),
                ], ok: _done.contains('sleep')),
                _row('Water', Icons.water_drop_outlined, [
                  _chip('+ 1 glass', () => _log('hydration', 250)),
                  _chip('+ 500 ml', () => _log('hydration', 500)),
                  _chip('+ 1 litre', () => _log('hydration', 1000)),
                ], ok: _done.contains('hydration')),
                _row('Exercise', Icons.directions_run, [
                  for (final m in const [15, 30, 45, 60])
                    _chip('$m min', () => _log('exercise', m)),
                ], ok: _done.contains('exercise')),
                _row('Feeling unwell?', Icons.healing_outlined, [
                  for (final i in ideas) _chip(i, () => _logSymptom(i)),
                  ActionChip(
                    avatar: const Icon(Icons.add, size: 14),
                    label: const Text('Other'),
                    onPressed: _logSymptom,
                  ),
                ]),
              ],
            ),
          ),
          const SectionTitle('Last 30 days'),
          AsyncBody<Json>(
            sum,
            onRetry: () => ref.invalidate(
              profileMapProvider('/lifestyle/summary?days=30'),
            ),
            builder: (d) {
              final list = (d['categories'] as List? ?? [])
                  .whereType<Map>()
                  .where((c) => (numOf(c['days_logged']) ?? 0) > 0)
                  .toList();
              if (list.isEmpty) {
                return const Text(
                  'Tap the chips above and your trends appear here.',
                  style: TextStyle(fontSize: 11, color: Pal.mut),
                );
              }
              return Column(
                children: [
                  for (final c in list)
                    SCard(
                      child: Row(
                        children: [
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  '${c['name']}'.split(' (').first,
                                  style: const TextStyle(
                                    fontSize: 12,
                                    fontWeight: FontWeight.w600,
                                  ),
                                ),
                                Text(
                                  '${c['days_logged']} days · avg ${fmtNum(numOf(c['average_per_day']))} ${c['unit']}',
                                  style: const TextStyle(
                                    fontSize: 10,
                                    color: Pal.mut,
                                  ),
                                ),
                              ],
                            ),
                          ),
                          SizedBox(
                            width: 90,
                            child: Sparkline([
                              for (final x in (c['daily'] as List? ?? []))
                                (numOf((x as Map)['value']) ?? 0).toDouble(),
                            ]),
                          ),
                        ],
                      ),
                    ),
                ],
              );
            },
          ),
          const SectionTitle('Patterns'),
          _Patterns(
            ref.watch(profileMapProvider('/lifestyle/correlations?days=60')),
          ),
          const SectionTitle('Symptom journal'),
          AsyncBody<List<Json>>(
            symptoms,
            onRetry: () => ref.invalidate(profileListProvider('/symptoms')),
            builder: (list) {
              if (list.isEmpty) {
                return const Text(
                  'Nothing logged. Tap a symptom above when you feel unwell.',
                  style: TextStyle(fontSize: 11, color: Pal.mut),
                );
              }
              return Column(
                children: [
                  for (final s in list.take(15))
                    SCard(
                      padding: const EdgeInsets.fromLTRB(12, 8, 6, 8),
                      child: Row(
                        children: [
                          _SeverityDot(numOf(s['severity'])?.round() ?? 0),
                          const SizedBox(width: 12),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  '${s['name']}',
                                  style: const TextStyle(
                                    fontSize: 12.5,
                                    fontWeight: FontWeight.w600,
                                  ),
                                ),
                                Text(
                                  '${_sevWord(numOf(s['severity'])?.round() ?? 0)} · ${'${s['started_at']}'.split('T').first}',
                                  style: _kNote,
                                ),
                              ],
                            ),
                          ),
                          if (s['active'] == true)
                            TextButton(
                              onPressed: () async {
                                try {
                                  await ref.read(apiProvider).patch(
                                    '/profiles/${ref.read(profileIdProvider)}/symptoms/${s['id']}',
                                    {
                                      'ended_at': DateTime.now()
                                          .toUtc()
                                          .toIso8601String(),
                                    },
                                  );
                                  ref.invalidate(
                                    profileListProvider('/symptoms'),
                                  );
                                } catch (e) {
                                  if (context.mounted) {
                                    toast(context, friendly(e));
                                  }
                                }
                              },
                              child: const Text('Better now'),
                            )
                          else
                            const Pill('Resolved', fg: Pal.ok, bg: Pal.okBg),
                        ],
                      ),
                    ),
                ],
              );
            },
          ),
        ],
      ),
    );
  }
}

String _sevWord(int s) => s <= 3
    ? 'Mild'
    : s <= 6
    ? 'Moderate'
    : 'Severe';

class _SeverityDot extends StatelessWidget {
  const _SeverityDot(this.s);
  final int s;
  @override
  Widget build(BuildContext context) {
    final c = s <= 3 ? Pal.ok : (s <= 6 ? Pal.warn : Pal.bad);
    return CircleAvatar(
      radius: 16,
      backgroundColor: c.withValues(alpha: 0.15),
      child: Text(
        '$s',
        style: TextStyle(color: c, fontWeight: FontWeight.w700, fontSize: 13),
      ),
    );
  }
}

class _SymptomSheet extends StatefulWidget {
  const _SymptomSheet(this.preset);
  final String? preset;
  @override
  State<_SymptomSheet> createState() => _SymptomSheetState();
}

class _SymptomSheetState extends State<_SymptomSheet> {
  late final _name = TextEditingController(text: widget.preset);

  @override
  Widget build(BuildContext context) => Padding(
    padding: EdgeInsets.fromLTRB(
      16,
      0,
      16,
      MediaQuery.of(context).viewInsets.bottom + 16,
    ),
    child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          widget.preset ?? 'What are you feeling?',
          style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
        ),
        if (widget.preset == null) ...[
          const SizedBox(height: 10),
          TextField(
            controller: _name,
            autofocus: true,
            textCapitalization: TextCapitalization.sentences,
            decoration: const InputDecoration(hintText: 'e.g. Sore throat'),
          ),
        ],
        const SizedBox(height: 14),
        const Text('How strong is it?', style: _kNote),
        const SizedBox(height: 8),
        Row(
          children: [
            for (final o in const [
              ('Mild', 3, Pal.ok),
              ('Moderate', 5, Pal.warn),
              ('Severe', 8, Pal.bad),
            ])
              Expanded(
                child: Padding(
                  padding: const EdgeInsets.only(right: 8),
                  child: OutlinedButton(
                    style: OutlinedButton.styleFrom(foregroundColor: o.$3),
                    onPressed: () {
                      final n = _name.text.trim();
                      if (n.isEmpty) {
                        toast(context, 'Say what you are feeling.');
                        return;
                      }
                      Navigator.pop(context, (n, o.$2));
                    },
                    child: Text(o.$1),
                  ),
                ),
              ),
          ],
        ),
      ],
    ),
  );
}

// ---------------------------------------------------------------- Timeline / Goals / Investigations

const _timelineKinds = <(String, String, IconData, Color)>[
  ('document', 'Reports', Icons.description_outlined, Pal.pri),
  ('observation', 'Results', Icons.biotech_outlined, Pal.ok),
  ('history', 'History', Icons.medical_information_outlined, Pal.warn),
  ('follow_up', 'Follow-ups', Icons.event_available_outlined, Pal.pri),
  ('goal', 'Goals', Icons.flag_outlined, Pal.ok),
  ('device', 'Devices', Icons.watch_outlined, Pal.mut),
];

const _months = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
];

class TimelineScreen extends ConsumerStatefulWidget {
  const TimelineScreen({super.key});
  @override
  ConsumerState<TimelineScreen> createState() => _TimelineState();
}

class _TimelineState extends ConsumerState<TimelineScreen> {
  String? _kind;
  int _page = 0;

  @override
  Widget build(BuildContext context) {
    final path =
        '/timeline?limit=100&offset=${_page * 100}${_kind == null ? '' : '&kind=$_kind'}';
    final t = ref.watch(profileListProvider(path));
    return Scaffold(
      appBar: AppBar(title: const Text('Clinical Timeline')),
      body: Column(
        children: [
          SizedBox(
            height: 46,
            child: ListView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
              children: [
                Padding(
                  padding: const EdgeInsets.only(right: 8),
                  child: ChoiceChip(
                    label: Text('All', style: const TextStyle(fontSize: 12)),
                    selected: _kind == null,
                    onSelected: (_) => setState(() {
                      _kind = null;
                      _page = 0;
                    }),
                  ),
                ),
                for (final k in _timelineKinds)
                  Padding(
                    padding: const EdgeInsets.only(right: 8),
                    child: ChoiceChip(
                      avatar: Icon(k.$3, size: 14, color: k.$4),
                      label: Text(k.$2, style: const TextStyle(fontSize: 12)),
                      selected: _kind == k.$1,
                      onSelected: (_) => setState(() {
                        _kind = k.$1;
                        _page = 0;
                      }),
                    ),
                  ),
              ],
            ),
          ),
          Expanded(
            child: AsyncBody<List<Json>>(
              t,
              onRetry: () => ref.invalidate(profileListProvider(path)),
              builder: (list) {
                if (list.isEmpty && _page == 0) {
                  return const Empty(
                    Icons.timeline,
                    'No events to show',
                    body: 'Reports, results, history, follow-ups and goals appear here in date order.',
                  );
                }
                final children = <Widget>[];
                String? month;
                for (var i = 0; i < list.length; i++) {
                  final at = DateTime.tryParse('${list[i]['occurred_at']}')
                      ?.toLocal();
                  final m = at == null
                      ? ''
                      : '${_months[at.month - 1]} ${at.year}';
                  if (m != month) {
                    month = m;
                    children.add(
                      Padding(
                        padding: const EdgeInsets.fromLTRB(2, 14, 2, 6),
                        child: Text(
                          m,
                          style: const TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w700,
                            color: Pal.pri,
                          ),
                        ),
                      ),
                    );
                  }
                  children.add(
                    Row(
                      mainAxisAlignment: MainAxisAlignment.spaceBetween,
                      children: [
                        TextButton.icon(
                          onPressed: _page == 0
                              ? null
                              : () => setState(() => _page--),
                          icon: const Icon(Icons.arrow_back),
                          label: const Text('Newer'),
                        ),
                        Text(
                          'Page ${_page + 1}',
                          style: const TextStyle(color: Pal.mut),
                        ),
                        TextButton.icon(
                          onPressed: list.length < 100
                              ? null
                              : () => setState(() => _page++),
                          icon: const Icon(Icons.arrow_forward),
                          label: const Text('Older'),
                        ),
                      ],
                    ),
                  );
                  children.add(
                    _TimelineEntry(
                      list[i],
                      at,
                      last: i == list.length - 1 || _monthOf(list[i + 1]) != m,
                    ),
                  );
                }
                return ListView(
                  padding: const EdgeInsets.fromLTRB(14, 0, 14, 24),
                  children: children,
                );
              },
            ),
          ),
        ],
      ),
    );
  }

  String _monthOf(Json i) {
    final at = DateTime.tryParse('${i['occurred_at']}')?.toLocal();
    return at == null ? '' : '${_months[at.month - 1]} ${at.year}';
  }
}

class _TimelineEntry extends StatelessWidget {
  const _TimelineEntry(this.item, this.at, {required this.last});
  final Json item;
  final DateTime? at;
  final bool last;

  @override
  Widget build(BuildContext context) {
    final k = _timelineKinds.firstWhere(
      (x) => x.$1 == '${item['kind']}',
      orElse: () => _timelineKinds.first,
    );
    final id = '${item['id']}';
    final opensDocument =
        (item['kind'] == 'document' || item['kind'] == 'observation') &&
        RegExp(r'^[0-9a-f-]{36}$').hasMatch(id);
    final title = '${item['title']}';
    final detail = '${item['detail'] ?? ''}';
    final lines = item['kind'] == 'observation'
        ? detail.split(' · ')
        : <String>[];
    return IntrinsicHeight(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SizedBox(
            width: 34,
            child: Column(
              children: [
                CircleAvatar(
                  radius: 14,
                  backgroundColor: k.$4.withValues(alpha: .15),
                  child: Icon(k.$3, size: 15, color: k.$4),
                ),
                if (!last)
                  Expanded(child: Container(width: 2, color: Pal.line)),
              ],
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: SCard(
              onTap: opensDocument
                  ? () => Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => DocumentScreen(id),
                      ),
                    )
                  : null,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Text(
                        k.$2.toUpperCase(),
                        style: TextStyle(
                          fontSize: 9.5,
                          fontWeight: FontWeight.w700,
                          color: k.$4,
                          letterSpacing: .6,
                        ),
                      ),
                      const Spacer(),
                      if (at != null)
                        Text(
                          '${at!.day} ${_months[at!.month - 1].substring(0, 3)}',
                          style: const TextStyle(
                            fontSize: 10.5,
                            color: Pal.mut,
                          ),
                        ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(
                    title,
                    style: const TextStyle(
                      fontSize: 12.5,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  if (lines.length > 1)
                    Padding(
                      padding: const EdgeInsets.only(top: 6),
                      child: Wrap(
                        spacing: 6,
                        runSpacing: 4,
                        children: [
                          for (final l in lines.take(8))
                            Pill(l, fg: Pal.mut, bg: Pal.line),
                        ],
                      ),
                    )
                  else if (detail.isNotEmpty && item['kind'] != 'observation')
                    Text(
                      detail,
                      style: const TextStyle(fontSize: 11, color: Pal.mut),
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------- Privacy

class PrivacyScreen extends ConsumerStatefulWidget {
  const PrivacyScreen({super.key});
  @override
  ConsumerState<PrivacyScreen> createState() => _PrivacyState();
}

final _sessions = FutureProvider<List<Json>>(
  (ref) => ref.read(apiProvider).list('/auth/sessions'),
);

const _consentLabels = {
  'health_data': 'Store my health data',
  'medical_record_storage': 'Keep my uploaded records',
  'document_processing': 'Read my documents',
  'ai_processing': 'AI answers',
  'research': 'Research',
  'notifications': 'Reminders',
  'data_sharing': 'Data sharing',
};

class _PrivacyState extends ConsumerState<PrivacyScreen> {
  bool _allDevices = false;
  bool _allActivity = false;

  Future<void> _toggle(String purpose, bool on) async {
    final id = ref.read(profileIdProvider);
    if (purpose == 'health_data' && !on) {
      toast(
        context,
        'Health data consent is required. Delete the profile to withdraw it.',
      );
      return;
    }
    try {
      await ref.read(apiProvider).put('/profiles/$id/consents/$purpose', {
        'granted': on,
        'policy_version': '1.0',
      });
      if (purpose == 'ai_processing') {
        await ref.read(apiProvider).put(
          '/profiles/$id/agent/permissions/cloud_model',
          {'granted': on},
        );
      }
      ref.invalidate(profileListProvider('/consents'));
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  Future<void> _delete() async {
    final c = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text(
          'Delete this profile?',
          style: TextStyle(fontSize: 15),
        ),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text(
              'All records for this profile are permanently deleted. Type DELETE to confirm.',
              style: TextStyle(fontSize: 12),
            ),
            TextField(controller: c),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, c.text == 'DELETE'),
            child: const Text('Delete', style: TextStyle(color: Pal.bad)),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await ref.read(apiProvider).delete(
        '/profiles/${ref.read(profileIdProvider)}',
        {'confirm': 'DELETE'},
      );
      ref.read(activeProfileProvider.notifier).clear();
      ref.invalidate(profilesProvider);
      if (mounted) Navigator.of(context).popUntil((r) => r.isFirst);
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final consents = ref.watch(profileListProvider('/consents'));
    final sessions = ref.watch(_sessions);
    final access = ref.watch(profileListProvider('/access-history'));
    return _Page(
      'Privacy & Data Rights',
      ListView(
        padding: const EdgeInsets.all(14),
        children: [
          _PrivacyHeader(
            consents: consents.value ?? const [],
            devices: sessions.value?.length,
            locked: ref.watch(appLockEnabledProvider).asData?.value ?? false,
          ),
          SCard(
            onTap: () => Navigator.push(
              context,
              MaterialPageRoute<void>(
                builder: (_) => const DataControlScreen(),
              ),
            ),
            child: const Row(
              children: [
                Icon(Icons.storage_outlined, color: Pal.pri, size: 20),
                SizedBox(width: 12),
                Expanded(
                  child: Text(
                    'Data Retention & Export',
                    style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                  ),
                ),
                Icon(Icons.chevron_right, color: Pal.mut),
              ],
            ),
          ),
          const SectionTitle('App lock'),
          SCard(
            padding: const EdgeInsets.symmetric(horizontal: 12),
            child: SwitchListTile(
              contentPadding: EdgeInsets.zero,
              dense: true,
              title: const Text(
                'Lock app with Face ID / fingerprint',
                style: TextStyle(fontSize: 12.5),
              ),
              subtitle: const Text(
                'Asked each time the app opens. Stored on this phone only.',
                style: TextStyle(fontSize: 10.5),
              ),
              value: ref.watch(appLockEnabledProvider).asData?.value ?? false,
              onChanged: (v) async {
                final bio = ref.read(biometricProvider);
                if (v) {
                  if (!await bio.available()) {
                    if (context.mounted) {
                      toast(
                        context,
                        'This phone has no Face ID or fingerprint set up.',
                      );
                    }
                    return;
                  }
                  if (!await bio.confirm('Turn on app lock')) return;
                }
                await setAppLock(ref, v);
              },
            ),
          ),
          const SectionTitle('Your permissions'),
          AsyncBody<List<Json>>(
            consents,
            onRetry: () => ref.invalidate(profileListProvider('/consents')),
            builder: (list) {
              final on = {
                for (final c in list) '${c['purpose']}': c['granted'] == true,
              };
              return Column(
                children: [
                  for (final g in _consentGroups)
                    SCard(
                      padding: const EdgeInsets.fromLTRB(12, 10, 12, 4),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            g.$1,
                            style: const TextStyle(
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                              color: Pal.pri,
                            ),
                          ),
                          for (final p in g.$2)
                            SwitchListTile(
                              contentPadding: EdgeInsets.zero,
                              dense: true,
                              title: Text(
                                _consentLabels[p]!,
                                style: const TextStyle(fontSize: 12.5),
                              ),
                              subtitle: Text(
                                _consentHints[p] ?? '',
                                style: const TextStyle(fontSize: 10.5),
                              ),
                              value: on[p] ?? false,
                              onChanged: (v) => _toggle(p, v),
                            ),
                        ],
                      ),
                    ),
                ],
              );
            },
          ),
          SectionTitle(
            'Where you are signed in',
            trailing: TextButton(
              onPressed: () async {
                await ref.read(identityProvider).signOut();
                ref.read(authProvider.notifier).signOut();
              },
              child: const Text('Sign out everywhere'),
            ),
          ),
          AsyncBody<List<Json>>(
            sessions,
            onRetry: () => ref.invalidate(_sessions),
            builder: (list) {
              final shown = _allDevices ? list : list.take(3).toList();
              return Column(
                children: [
                  for (final s in shown)
                    _DeviceCard(
                      s,
                      onRevoke: () async {
                        try {
                          await ref
                              .read(apiProvider)
                              .delete('/auth/sessions/${s['id']}');
                          ref.invalidate(_sessions);
                        } catch (e) {
                          if (context.mounted) toast(context, friendly(e));
                        }
                      },
                    ),
                  if (list.length > 3)
                    TextButton(
                      onPressed: () =>
                          setState(() => _allDevices = !_allDevices),
                      child: Text(
                        _allDevices
                            ? 'Show fewer'
                            : 'Show ${list.length - 3} more',
                      ),
                    ),
                ],
              );
            },
          ),
          const SectionTitle('Activity on your records'),
          AsyncBody<List<Json>>(
            access,
            builder: (list) {
              final groups = _groupActivity(list);
              if (groups.isEmpty) {
                return const Text('No activity yet.', style: _kNote);
              }
              final shown = _allActivity ? groups : groups.take(4).toList();
              return SCard(
                child: Column(
                  children: [
                    for (final g in shown)
                      Padding(
                        padding: const EdgeInsets.symmetric(vertical: 6),
                        child: Row(
                          children: [
                            Icon(g.icon, size: 18, color: Pal.pri),
                            const SizedBox(width: 10),
                            Expanded(
                              child: Text(
                                g.count > 1
                                    ? '${g.label} · ${g.count} times'
                                    : g.label,
                                style: const TextStyle(fontSize: 12),
                              ),
                            ),
                            Text(_since(g.last), style: _kNote),
                          ],
                        ),
                      ),
                    if (groups.length > 4)
                      TextButton(
                        onPressed: () =>
                            setState(() => _allActivity = !_allActivity),
                        child: Text(
                          _allActivity
                              ? 'Show less'
                              : 'Show all ${groups.length}',
                        ),
                      ),
                  ],
                ),
              );
            },
          ),
          const Padding(
            padding: EdgeInsets.only(top: 6),
            child: Text(
              'Only you can see this list. Every change to your records is logged here.',
              style: _kNote,
            ),
          ),
          const SizedBox(height: 14),
          OutlinedButton(
            style: OutlinedButton.styleFrom(foregroundColor: Pal.bad),
            onPressed: _delete,
            child: const Text('Delete this profile'),
          ),
        ],
      ),
    );
  }
}

const _kNote = TextStyle(fontSize: 10.5, color: Pal.mut);

const _consentGroups = [
  (
    'YOUR DATA',
    ['health_data', 'medical_record_storage', 'document_processing'],
  ),
  ('SMART FEATURES', ['ai_processing', 'notifications']),
  ('SHARING', ['research', 'data_sharing']),
];

const _consentHints = {
  'health_data': 'Needed to keep your profile. Delete the profile to withdraw.',
  'medical_record_storage': 'Keeps the reports and files you upload.',
  'document_processing': 'Reads values and medicines out of your reports. With AI answers on, report text goes to the cloud AI model with your name, phone, email and ID numbers removed.',
  'ai_processing': 'Lets Sehat answer and write insights from your results, symptoms and habits using a cloud AI model. Your name and contact details are never sent.',
  'research': 'Lets anonymised trends help improve care research.',
  'notifications': 'Medicine and follow-up reminders.',
  'data_sharing': 'Sharing with doctors or family you choose.',
};

String _since(DateTime? t) {
  if (t == null) return '';
  final d = DateTime.now().difference(t.toLocal());
  if (d.inMinutes < 2) return 'Just now';
  if (d.inMinutes < 60) return '${d.inMinutes} min ago';
  if (d.inHours < 24) return '${d.inHours} h ago';
  if (d.inDays < 7) return '${d.inDays} d ago';
  return '${t.toLocal().day}/${t.toLocal().month}/${t.toLocal().year}';
}

/// A readable device name from the user agent the API recorded.
(String, IconData) _deviceName(Json s) {
  final ua = '${s['user_agent'] ?? ''}';
  final browser = ua.contains('CriOS') || ua.contains('Chrome')
      ? 'Chrome'
      : ua.contains('Safari') && !ua.contains('Chrome')
      ? 'Safari'
      : ua.contains('Firefox')
      ? 'Firefox'
      : null;
  if (ua.contains('iPhone')) {
    return (
      'iPhone${browser == null ? '' : ' · $browser'}',
      Icons.phone_iphone,
    );
  }
  if (ua.contains('iPad')) return ('iPad', Icons.tablet_mac);
  if (ua.contains('Android')) {
    return (
      'Android${browser == null ? '' : ' · $browser'}',
      Icons.phone_android,
    );
  }
  if (ua.contains('Macintosh')) {
    return ('Mac${browser == null ? '' : ' · $browser'}', Icons.laptop_mac);
  }
  if (ua.contains('Windows')) {
    return (
      'Windows${browser == null ? '' : ' · $browser'}',
      Icons.laptop_windows,
    );
  }
  if (ua.isEmpty || s['device'] == 'api') return ('API client', Icons.terminal);
  return ('${s['device'] ?? 'Device'}', Icons.devices);
}

class _DeviceCard extends StatelessWidget {
  const _DeviceCard(this.s, {required this.onRevoke});
  final Json s;
  final VoidCallback onRevoke;
  @override
  Widget build(BuildContext context) {
    final (name, icon) = _deviceName(s);
    final current = s['current'] == true;
    return SCard(
      padding: const EdgeInsets.fromLTRB(12, 8, 6, 8),
      child: Row(
        children: [
          CircleAvatar(
            radius: 18,
            backgroundColor: current ? Pal.okBg : Pal.line,
            child: Icon(icon, size: 18, color: current ? Pal.ok : Pal.mut),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  name,
                  style: const TextStyle(
                    fontSize: 12.5,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                Text(
                  current
                      ? 'This device · active now'
                      : 'Last active ${_since(DateTime.tryParse('${s['last_seen_at']}'))}',
                  style: _kNote,
                ),
              ],
            ),
          ),
          if (!current)
            TextButton(onPressed: onRevoke, child: const Text('Sign out')),
        ],
      ),
    );
  }
}

class _Activity {
  _Activity(this.label, this.icon, this.last);
  final String label;
  final IconData icon;
  DateTime? last;
  int count = 1;
}

/// Collapses the raw audit rows into one line per kind of action, newest first.
List<_Activity> _groupActivity(List<Json> rows) {
  const nouns = {
    'document': 'Report',
    'biomarker': 'Lab result',
    'prescription': 'Prescription',
    'goal': 'Goal',
    'follow_up': 'Follow-up',
    'consent': 'Permission',
    'profile': 'Profile',
    'history': 'Medical history',
    'observation': 'Reading',
  };
  const verbs = {
    'approved': ('approved', Icons.verified_outlined),
    'uploaded': ('added', Icons.upload_file),
    'created': ('added', Icons.add_circle_outline),
    'updated': ('updated', Icons.edit_outlined),
    'deleted': ('deleted', Icons.delete_outline),
    'viewed': ('viewed', Icons.visibility_outlined),
    'exported': ('exported', Icons.ios_share),
  };
  final out = <String, _Activity>{};
  for (final r in rows) {
    final parts = '${r['action']}'.split('.');
    final noun = nouns[parts.first] ?? parts.first.replaceAll('_', ' ');
    final verb = parts.length > 1 ? parts.last : 'accessed';
    final v = verbs[verb] ?? (verb.replaceAll('_', ' '), Icons.history);
    final label = '${noun[0].toUpperCase()}${noun.substring(1)} ${v.$1}';
    final at = DateTime.tryParse('${r['created_at']}');
    final g = out[label];
    if (g == null) {
      out[label] = _Activity(label, v.$2, at);
    } else {
      g.count++;
      if (at != null && (g.last == null || at.isAfter(g.last!))) g.last = at;
    }
  }
  return out.values.toList()
    ..sort((a, b) => (b.last ?? DateTime(0)).compareTo(a.last ?? DateTime(0)));
}

/// Plain-language summary of how protected the account is.
class _PrivacyHeader extends StatelessWidget {
  const _PrivacyHeader({
    required this.consents,
    required this.devices,
    required this.locked,
  });
  final List<Json> consents;
  final int? devices;
  final bool locked;
  @override
  Widget build(BuildContext context) {
    final granted = consents.where((c) => c['granted'] == true).length;
    Widget stat(IconData i, String v, String l, Color c) => Expanded(
      child: Column(
        children: [
          Icon(i, size: 20, color: c),
          const SizedBox(height: 4),
          Text(v, style: const TextStyle(fontWeight: FontWeight.w700)),
          Text(l, style: _kNote, textAlign: TextAlign.center),
        ],
      ),
    );
    return SCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Row(
            children: [
              Icon(Icons.shield_outlined, color: Pal.ok),
              SizedBox(width: 8),
              Text(
                'Your health data is private to you',
                style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              stat(
                locked ? Icons.lock : Icons.lock_open,
                locked ? 'On' : 'Off',
                'App lock',
                locked ? Pal.ok : Pal.warn,
              ),
              stat(
                Icons.tune,
                '$granted of ${_consentLabels.length}',
                'Permissions on',
                Pal.pri,
              ),
              stat(
                Icons.devices,
                '${devices ?? '–'}',
                'Signed-in devices',
                Pal.pri,
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// Associations between logged lifestyle and symptom severity. Never presented as causes.
class _Patterns extends StatelessWidget {
  const _Patterns(this.data);
  final AsyncValue<Json> data;
  @override
  Widget build(BuildContext context) => AsyncBody<Json>(
    data,
    builder: (d) {
      final rows = ((d['correlations'] as List?) ?? [])
          .cast<Json>()
          .where((c) => c['status'] == 'ok')
          .toList();
      return Column(
        children: [
          const SCard(
            color: Pal.warnBg,
            child: Text(
              'Correlation, not cause. These are patterns in what you logged. They can be coincidence.',
              style: TextStyle(fontSize: 11, color: Pal.warn),
            ),
          ),
          if (rows.isEmpty)
            const Padding(
              padding: EdgeInsets.all(8),
              child: Text(
                'Log lifestyle and symptoms on at least 7 days with varying values to see patterns.',
                style: TextStyle(fontSize: 11, color: Pal.mut),
              ),
            ),
          for (final c in rows)
            SCard(
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '${c['description']}',
                      style: const TextStyle(fontSize: 12),
                    ),
                  ),
                  Pill('${c['strength']}'),
                ],
              ),
            ),
        ],
      );
    },
  );
}
