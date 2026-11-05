import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'places.dart';

const _note = TextStyle(fontSize: 10.5, color: Pal.mut);
const _bold = TextStyle(fontSize: 12, fontWeight: FontWeight.w600);

Widget _page(String title, Widget body, {Widget? fab}) => Scaffold(
  appBar: AppBar(title: Text(title)),
  body: body,
  floatingActionButton: fab,
);

void _open(BuildContext c, Widget w) =>
    Navigator.push(c, MaterialPageRoute<void>(builder: (_) => w));

double? _num(TextEditingController c) =>
    double.tryParse(c.text.trim().replaceAll(',', '.'));

Future<bool> _confirm(BuildContext context, String title) async =>
    await showDialog<bool>(
      context: context,
      builder: (_) => AlertDialog(
        title: Text(title),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    ) ??
    false;

// ---------------------------------------------------------------- Goals

String _targetText(Json g) {
  final lo = g['target_min'], hi = g['target_max'];
  final u = '${g['unit'] ?? ''}'.trim();
  final range = lo != null && hi != null
      ? '${fmtNum(numOf(lo))}–${fmtNum(numOf(hi))}'
      : lo != null
      ? '${fmtNum(numOf(lo))} or more'
      : 'up to ${fmtNum(numOf(hi))}';
  return 'Target $range $u'.trim();
}

(String, Color, Color, IconData) _goalState(Json g) {
  final st = '${(g['progress'] as Map?)?['status']}';
  if (g['status'] != 'active') {
    return ('Archived', Pal.mut, Pal.line, Icons.inventory_2_outlined);
  }
  return switch (st) {
    'on_target' => ('On target', Pal.ok, Pal.okBg, Icons.check_circle),
    'below_target' => ('Below target', Pal.warn, Pal.warnBg, Icons.south_east),
    'above_target' => ('Above target', Pal.bad, Pal.badBg, Icons.north_east),
    'unit_mismatch' => ('Unit differs', Pal.warn, Pal.warnBg, Icons.straighten),
    _ => ('Awaiting result', Pal.mut, Pal.line, Icons.hourglass_empty),
  };
}

/// One goal: status badge, target and the latest verified result, laid out the same everywhere.
class _GoalCard extends ConsumerWidget {
  const _GoalCard(this.g, {required this.onChanged, this.menu = true});
  final Json g;
  final VoidCallback onChanged;
  final bool menu;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final (label, fg, bg, icon) = _goalState(g);
    final pr = (g['progress'] as Map?)?.cast<String, dynamic>() ?? {};
    final latest = pr['latest_value'] == null
        ? 'No result yet. Add a report or reading to start tracking.'
        : 'Latest ${fmtNum(numOf(pr['latest_value']))} ${pr['latest_unit'] ?? ''}'
              '${pr['latest_on'] == null ? '' : ' · ${'${pr['latest_on']}'.split('T').first}'}';
    return SCard(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          CircleAvatar(
            radius: 18,
            backgroundColor: bg,
            child: Icon(icon, size: 18, color: fg),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('${g['title']}', style: _bold),
                const SizedBox(height: 2),
                Text(_targetText(g), style: _note),
                const SizedBox(height: 2),
                Text(latest, style: _note),
                const SizedBox(height: 6),
                Pill(label, fg: fg, bg: bg),
              ],
            ),
          ),
          if (menu)
            PopupMenuButton<String>(
              onSelected: (v) async {
                final api = ref.read(apiProvider);
                try {
                  if (v == 'delete') {
                    if (!await _confirm(context, 'Delete this goal?')) return;
                    await api.delete('/goals/${g['id']}');
                  } else {
                    await api.patch('/goals/${g['id']}', {'status': v});
                  }
                  onChanged();
                } catch (e) {
                  if (context.mounted) toast(context, friendly(e));
                }
              },
              itemBuilder: (_) => [
                PopupMenuItem(
                  value: g['status'] == 'active' ? 'archived' : 'active',
                  child: Text(
                    g['status'] == 'active' ? 'Archive' : 'Reactivate',
                  ),
                ),
                const PopupMenuItem(value: 'delete', child: Text('Delete')),
              ],
            ),
        ],
      ),
    );
  }
}

class GoalsScreen extends ConsumerWidget {
  const GoalsScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final goals = ref.watch(profileListProvider('/goals'));
    void refresh() => refreshCarePlan(ref);
    return _page(
      'Goals',
      AsyncBody<List<Json>>(
        goals,
        onRetry: refresh,
        builder: (list) {
          if (list.isEmpty) {
            return const Empty(
              Icons.flag_outlined,
              'No goals yet',
              body: 'Add a target for a biomarker you track.',
            );
          }
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [for (final g in list) _GoalCard(g, onChanged: refresh)],
          );
        },
      ),
      fab: FloatingActionButton.extended(
        onPressed: () async {
          final ok = await Navigator.push<bool>(
            context,
            MaterialPageRoute(builder: (_) => const GoalForm()),
          );
          if (ok == true) refresh();
        },
        icon: const Icon(Icons.add),
        label: const Text('New goal'),
      ),
    );
  }
}

class GoalForm extends ConsumerStatefulWidget {
  const GoalForm({super.key, this.initialCode});
  final String? initialCode;
  @override
  ConsumerState<GoalForm> createState() => _GoalFormState();
}

class _GoalFormState extends ConsumerState<GoalForm> {
  Json? _test;
  String? _unit;
  final _search = TextEditingController();
  final _min = TextEditingController();
  final _max = TextEditingController();
  bool _busy = false;
  bool _seeded = false;
  String? _error;

  @override
  void dispose() {
    _search.dispose();
    _min.dispose();
    _max.dispose();
    super.dispose();
  }

  List<String> _units(Json t) =>
      ((t['supported_units'] as List?) ?? []).map((e) => '$e').toList();

  void _pick(Json t, Json? tracked) {
    final units = _units(t);
    setState(() {
      _test = t;
      _error = null;
      final latest = '${tracked?['unit'] ?? ''}';
      _unit = units.contains(latest)
          ? latest
          : (units.isNotEmpty ? units.first : (latest.isEmpty ? null : latest));
    });
  }

  Future<void> _save() async {
    final t = _test;
    if (t == null) {
      setState(() => _error = 'Pick the test you want to set a target for.');
      return;
    }
    final unit = _unit;
    if (unit == null || unit.isEmpty) {
      setState(() => _error = 'Choose a unit.');
      return;
    }
    final lo = _num(_min), hi = _num(_max);
    if (lo == null && hi == null) {
      setState(() => _error = 'Set a lower limit, an upper limit or both.');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/goals',
        {
          'biomarker_code': t['code'],
          'unit': unit,
          'target_min': ?lo,
          'target_max': ?hi,
        },
      );
      ref.invalidate(profileMapProvider('/overview'));
      if (mounted) Navigator.pop(context, true);
    } catch (e) {
      if (mounted) setState(() => _error = friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final catalog = ref.watch(profileListProvider('/biomarkers'));
    final tracked = ref.watch(profileListProvider('/biomarkers/summary'));
    return _page(
      'New goal',
      AsyncBody<List<Json>>(
        catalog,
        onRetry: () => ref.invalidate(profileListProvider('/biomarkers')),
        builder: (all) {
          final mine = {
            for (final b in tracked.asData?.value ?? <Json>[])
              '${b['biomarker_code']}': b,
          };
          if (!_seeded && widget.initialCode != null) {
            _seeded = true;
            for (final t in all) {
              if (t['code'] == widget.initialCode) {
                _test = t;
                _unit = '${mine[t['code']]?['unit'] ?? _units(t).firstOrNull}';
              }
            }
          }
          final q = _search.text.trim().toLowerCase();
          final matches = all
              .where((t) {
                if (q.isEmpty) return mine.containsKey(t['code']);
                final hay = [
                  '${t['display_name']}',
                  ...((t['synonyms'] as List?) ?? []).map((e) => '$e'),
                ].join(' ').toLowerCase();
                return hay.contains(q);
              })
              .take(10)
              .toList();
          final picked = _test;
          final latest = picked == null ? null : mine[picked['code']];
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [
              const Text(
                'Set a target range for a lab test. Your results are checked against it and the agent tracks your progress.',
                style: _note,
              ),
              const SizedBox(height: 12),
              if (picked == null) ...[
                TextField(
                  controller: _search,
                  onChanged: (_) => setState(() {}),
                  decoration: const InputDecoration(
                    labelText: 'Search for a test',
                    hintText: 'HbA1c, LDL cholesterol, Vitamin D…',
                    prefixIcon: Icon(Icons.search),
                  ),
                ),
                const SizedBox(height: 8),
                if (q.isEmpty && matches.isNotEmpty)
                  const Padding(
                    padding: EdgeInsets.only(bottom: 4),
                    child: Text('From your results', style: _note),
                  ),
                if (matches.isEmpty)
                  const Padding(
                    padding: EdgeInsets.all(8),
                    child: Text(
                      'No matching test. Try another name.',
                      style: _note,
                    ),
                  ),
                for (final t in matches)
                  SCard(
                    onTap: () => _pick(t, mine[t['code']]),
                    child: Row(
                      children: [
                        Expanded(
                          child: Text('${t['display_name']}', style: _bold),
                        ),
                        if (mine[t['code']] != null)
                          Text(
                            'Latest ${fmtNum(numOf((mine[t['code']]!['latest'] as Map?)?['value']))} ${mine[t['code']]!['unit'] ?? ''}',
                            style: _note,
                          ),
                      ],
                    ),
                  ),
              ] else ...[
                SCard(
                  child: Row(
                    children: [
                      const Icon(Icons.science_outlined, color: Pal.pri),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('${picked['display_name']}', style: _bold),
                            if (latest != null)
                              Text(
                                'Your latest: ${fmtNum(numOf((latest['latest'] as Map?)?['value']))} ${latest['unit'] ?? ''}',
                                style: _note,
                              ),
                          ],
                        ),
                      ),
                      TextButton(
                        onPressed: () => setState(() {
                          _test = null;
                          _unit = null;
                        }),
                        child: const Text('Change'),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 6),
                if (_units(picked).length > 1) ...[
                  const Text('Unit', style: _note),
                  Wrap(
                    spacing: 8,
                    children: [
                      for (final u in _units(picked))
                        ChoiceChip(
                          label: Text(u),
                          selected: _unit == u,
                          onSelected: (_) => setState(() => _unit = u),
                        ),
                    ],
                  ),
                ] else if (_units(picked).length == 1)
                  Text('Unit: ${_units(picked).first}', style: _note)
                else
                  TextFormField(
                    key: ValueKey('unit-${picked['code']}'),
                    initialValue: _unit,
                    onChanged: (v) => _unit = v.trim(),
                    decoration: const InputDecoration(
                      labelText: 'Unit',
                      hintText: 'e.g. %, mg/dL',
                    ),
                  ),
                const SizedBox(height: 10),
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _min,
                        keyboardType: const TextInputType.numberWithOptions(
                          decimal: true,
                        ),
                        decoration: const InputDecoration(
                          labelText: 'Lower limit',
                          hintText: 'optional',
                        ),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: TextField(
                        controller: _max,
                        keyboardType: const TextInputType.numberWithOptions(
                          decimal: true,
                        ),
                        decoration: const InputDecoration(
                          labelText: 'Upper limit',
                          hintText: 'optional',
                        ),
                      ),
                    ),
                  ],
                ),
              ],
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.only(top: 10),
                  child: Text(
                    _error!,
                    style: const TextStyle(color: Pal.bad, fontSize: 12),
                  ),
                ),
              const SizedBox(height: 14),
              FilledButton(
                onPressed: _busy ? null : _save,
                child: const Text('Save goal'),
              ),
            ],
          );
        },
      ),
    );
  }
}

// -------------------------------------------------------- Investigations

// ------------------------------------------------------------- Care plan

/// Refreshes every screen that shows goals or follow-ups so they never disagree.
void refreshCarePlan(WidgetRef ref) {
  ref.invalidate(profileListProvider('/follow-ups'));
  ref.invalidate(profileListProvider('/goals'));
  ref.invalidate(profileMapProvider('/overview'));
}

Future<void> setFollowUpStatus(
  BuildContext context,
  WidgetRef ref,
  Json f,
  String status,
) async {
  try {
    await ref.read(apiProvider).patch('/follow-ups/${f['id']}', {
      'status': status,
    });
    refreshCarePlan(ref);
  } catch (e) {
    if (context.mounted) toast(context, friendly(e));
  }
}

Future<void> deleteFollowUp(BuildContext context, WidgetRef ref, Json f) async {
  if (!await _confirm(context, 'Delete this follow-up?')) return;
  try {
    await ref.read(apiProvider).delete('/follow-ups/${f['id']}');
    refreshCarePlan(ref);
  } catch (e) {
    if (context.mounted) toast(context, friendly(e));
  }
}

/// One open follow-up with complete, dismiss and delete.
class FollowUpTile extends ConsumerWidget {
  const FollowUpTile(this.f, {super.key});
  final Json f;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final due = f['due_on'] == null
        ? null
        : DateTime.tryParse('${f['due_on']}');
    final overdue =
        due != null &&
        due.isBefore(DateTime.now().subtract(const Duration(days: 1)));
    return SCard(
      padding: const EdgeInsets.fromLTRB(4, 2, 4, 2),
      child: Row(
        children: [
          IconButton(
            tooltip: 'Mark done',
            icon: const Icon(Icons.radio_button_unchecked, size: 20),
            onPressed: () => setFollowUpStatus(context, ref, f, 'done'),
          ),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('${f['title']}', style: _bold),
                Text(
                  due == null
                      ? 'No due date'
                      : '${overdue ? 'Overdue · ' : 'Due '}${f['due_on']}',
                  style: TextStyle(
                    fontSize: 11,
                    color: overdue ? Pal.warn : Pal.mut,
                  ),
                ),
              ],
            ),
          ),
          PopupMenuButton<String>(
            onSelected: (v) => switch (v) {
              'delete' => deleteFollowUp(context, ref, f),
              'doctor' ||
              'lab' => _open(context, DoctorsScreen(initialKind: v)),
              _ => setFollowUpStatus(context, ref, f, v),
            },
            itemBuilder: (_) => [
              if (f['kind'] == 'appointment')
                const PopupMenuItem(
                  value: 'doctor',
                  child: Text('Find a doctor'),
                ),
              if (f['kind'] == 'investigation')
                const PopupMenuItem(value: 'lab', child: Text('Find a lab')),
              const PopupMenuItem(value: 'done', child: Text('Mark done')),
              PopupMenuItem(value: 'dismissed', child: Text('Dismiss')),
              PopupMenuItem(value: 'delete', child: Text('Delete')),
            ],
          ),
        ],
      ),
    );
  }
}

const _fuKinds = [
  ('investigation', 'Test', Icons.biotech_outlined),
  ('appointment', 'Visit', Icons.event_available_outlined),
  ('medication', 'Medicine', Icons.medication_outlined),
  ('other', 'Other', Icons.checklist),
];

const _fuIdeas = {
  'investigation': [
    'Repeat blood test',
    'Fasting blood sugar',
    'Lipid profile',
  ],
  'appointment': ['Doctor review', 'Specialist consult', 'Dental check-up'],
  'medication': ['Refill prescription', 'Review dose with doctor'],
  'other': ['Update my reports', 'Check blood pressure'],
};

Future<void> addFollowUp(
  BuildContext context,
  WidgetRef ref, {
  String kind = 'investigation',
  String? title,
}) async {
  final res = await showModalBottomSheet<Map<String, String>>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (_) => _FollowUpSheet(kind: kind, title: title),
  );
  if (res == null) return;
  try {
    await ref
        .read(apiProvider)
        .post('/profiles/${ref.read(profileIdProvider)}/follow-ups', res);
    refreshCarePlan(ref);
    if (context.mounted) toast(context, 'Follow-up added to your care plan.');
  } catch (e) {
    if (context.mounted) toast(context, friendly(e));
  }
}

class _FollowUpSheet extends StatefulWidget {
  const _FollowUpSheet({required this.kind, this.title});
  final String kind;
  final String? title;
  @override
  State<_FollowUpSheet> createState() => _FollowUpSheetState();
}

class _FollowUpSheetState extends State<_FollowUpSheet> {
  late String _kind = widget.kind;
  late final _title = TextEditingController(text: widget.title);
  DateTime? _due;

  String _iso(DateTime d) =>
      '${d.year}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

  @override
  Widget build(BuildContext context) {
    final now = DateTime.now();
    final quick = [
      ('1 week', now.add(const Duration(days: 7))),
      ('1 month', DateTime(now.year, now.month + 1, now.day)),
      ('3 months', DateTime(now.year, now.month + 3, now.day)),
    ];
    return Padding(
      padding: EdgeInsets.fromLTRB(
        16,
        0,
        16,
        MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text(
              'New follow-up',
              style: TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 14),
            const Text('What kind?', style: _note),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              children: [
                for (final k in _fuKinds)
                  ChoiceChip(
                    avatar: Icon(k.$3, size: 16),
                    label: Text(k.$2),
                    selected: _kind == k.$1,
                    onSelected: (_) => setState(() => _kind = k.$1),
                  ),
              ],
            ),
            const SizedBox(height: 14),
            TextField(
              controller: _title,
              textCapitalization: TextCapitalization.sentences,
              decoration: const InputDecoration(
                labelText: 'What needs to be done?',
              ),
            ),
            const SizedBox(height: 8),
            Wrap(
              spacing: 6,
              runSpacing: 0,
              children: [
                for (final i in _fuIdeas[_kind]!)
                  ActionChip(
                    label: Text(i, style: const TextStyle(fontSize: 11)),
                    onPressed: () => setState(() => _title.text = i),
                  ),
              ],
            ),
            const SizedBox(height: 14),
            const Text('When?', style: _note),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              children: [
                for (final q in quick)
                  ChoiceChip(
                    label: Text('In ${q.$1}'),
                    selected: _due != null && _iso(_due!) == _iso(q.$2),
                    onSelected: (_) => setState(() => _due = q.$2),
                  ),
                ActionChip(
                  avatar: const Icon(Icons.calendar_month, size: 16),
                  label: Text(_due == null ? 'Pick a date' : _iso(_due!)),
                  onPressed: () async {
                    final d = await showDatePicker(
                      context: context,
                      initialDate: _due ?? now,
                      firstDate: now.subtract(const Duration(days: 1)),
                      lastDate: DateTime(now.year + 5),
                    );
                    if (d != null) setState(() => _due = d);
                  },
                ),
              ],
            ),
            const SizedBox(height: 18),
            SizedBox(
              width: double.infinity,
              child: FilledButton(
                onPressed: () {
                  if (_title.text.trim().isEmpty) {
                    toast(context, 'Say what needs to be done.');
                    return;
                  }
                  Navigator.pop(context, {
                    'kind': _kind,
                    'title': _title.text.trim(),
                    if (_due != null) 'due_on': _iso(_due!),
                  });
                },
                child: const Text('Add to care plan'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Stat extends StatelessWidget {
  const _Stat(this.value, this.label, this.color);
  final int value;
  final String label;
  final Color color;
  @override
  Widget build(BuildContext context) => Expanded(
    child: Column(
      children: [
        Text(
          '$value',
          style: TextStyle(
            fontSize: 22,
            fontWeight: FontWeight.w800,
            color: color,
          ),
        ),
        Text(label, style: _note, textAlign: TextAlign.center),
      ],
    ),
  );
}

class CarePlanScreen extends ConsumerWidget {
  const CarePlanScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final goals = ref.watch(profileListProvider('/goals'));
    final fus = ref.watch(profileListProvider('/follow-ups'));
    final activeGoals = (goals.value ?? [])
        .where((g) => g['status'] == 'active')
        .toList();
    final open = (fus.value ?? []).where((f) => f['status'] == 'open').toList();
    final now = DateTime.now().subtract(const Duration(days: 1));
    final overdue = open.where((f) {
      final d = DateTime.tryParse('${f['due_on'] ?? ''}');
      return d != null && d.isBefore(now);
    }).length;
    final onTarget = activeGoals
        .where((g) => (g['progress'] as Map?)?['status'] == 'on_target')
        .length;
    return Scaffold(
      appBar: AppBar(title: const Text('Care Plan')),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => addFollowUp(context, ref),
        icon: const Icon(Icons.add_task),
        label: const Text('Add follow-up'),
      ),
      body: RefreshIndicator(
        onRefresh: () async => refreshCarePlan(ref),
        child: ListView(
          padding: const EdgeInsets.fromLTRB(14, 8, 14, 90),
          children: [
            SCard(
              child: Row(
                children: [
                  _Stat(onTarget, 'Goals on target', Pal.ok),
                  _Stat(open.length, 'Open follow-ups', Pal.pri),
                  _Stat(overdue, 'Overdue', overdue > 0 ? Pal.warn : Pal.mut),
                ],
              ),
            ),
            SectionTitle(
              'Health goals',
              trailing: TextButton(
                onPressed: () => _open(context, const GoalsScreen()),
                child: const Text('See all'),
              ),
            ),
            AsyncBody<List<Json>>(
              goals,
              builder: (_) => activeGoals.isEmpty
                  ? SCard(
                      onTap: () => _open(context, const GoalForm()),
                      child: const Row(
                        children: [
                          Icon(Icons.flag_outlined, color: Pal.pri),
                          SizedBox(width: 12),
                          Expanded(
                            child: Text(
                              'Set a target for a test you track, like HbA1c or LDL.',
                              style: _bold,
                            ),
                          ),
                          Icon(Icons.chevron_right, color: Pal.mut),
                        ],
                      ),
                    )
                  : Column(
                      children: [
                        for (final g in activeGoals.take(4))
                          _GoalCard(
                            g,
                            menu: false,
                            onChanged: () => refreshCarePlan(ref),
                          ),
                      ],
                    ),
            ),
            const SectionTitle('Follow-ups'),
            AsyncBody<List<Json>>(
              fus,
              onRetry: () => ref.invalidate(profileListProvider('/follow-ups')),
              builder: (_) => open.isEmpty
                  ? const Empty(
                      Icons.event_available_outlined,
                      'You are all caught up',
                      body: 'Add a repeat test, a visit or a refill, and it shows up here and in your reminders.',
                    )
                  : Column(children: [for (final f in open) FollowUpTile(f)]),
            ),
          ],
        ),
      ),
    );
  }
}
