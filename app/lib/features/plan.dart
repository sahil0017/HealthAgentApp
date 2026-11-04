import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';

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

class GoalsScreen extends ConsumerWidget {
  const GoalsScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final goals = ref.watch(profileListProvider('/goals'));
    void refresh() => ref.invalidate(profileListProvider('/goals'));
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
            children: [
              for (final g in list)
                SCard(
                  child: Row(
                    children: [
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('${g['title']}', style: _bold),
                            Text(
                              'Target ${g['target_min'] == null ? 'up to' : fmtNum(numOf(g['target_min']))}${g['target_min'] != null && g['target_max'] != null ? '–' : ''}${g['target_max'] == null ? ' or more' : fmtNum(numOf(g['target_max']))} ${g['unit'] ?? ''}',
                              style: _note,
                            ),
                          ],
                        ),
                      ),
                      Pill('${g['status']}'),
                      PopupMenuButton<String>(
                        onSelected: (v) async {
                          final api = ref.read(apiProvider);
                          try {
                            if (v == 'delete') {
                              if (!await _confirm(
                                context,
                                'Delete this goal?',
                              )) {
                                return;
                              }
                              await api.delete('/goals/${g['id']}');
                            } else {
                              await api.patch('/goals/${g['id']}', {
                                'status': v,
                              });
                            }
                            refresh();
                          } catch (e) {
                            if (context.mounted) toast(context, friendly(e));
                          }
                        },
                        itemBuilder: (_) => [
                          PopupMenuItem(
                            value: g['status'] == 'active'
                                ? 'archived'
                                : 'active',
                            child: Text(
                              g['status'] == 'active'
                                  ? 'Archive'
                                  : 'Reactivate',
                            ),
                          ),
                          const PopupMenuItem(
                            value: 'delete',
                            child: Text('Delete'),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
            ],
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
  const GoalForm({super.key});
  @override
  ConsumerState<GoalForm> createState() => _GoalFormState();
}

class _GoalFormState extends ConsumerState<GoalForm> {
  Json? _biomarker;
  final _min = TextEditingController();
  final _max = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _min.dispose();
    _max.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final b = _biomarker;
    if (b == null) {
      setState(() => _error = 'Choose a biomarker.');
      return;
    }
    final lo = _num(_min), hi = _num(_max);
    if (lo == null && hi == null) {
      setState(() => _error = 'Set a minimum, a maximum or both.');
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
          'biomarker_code': b['biomarker_code'],
          'unit': b['unit'],
          'target_min': ?lo,
          'target_max': ?hi,
        },
      );
      if (mounted) Navigator.pop(context, true);
    } catch (e) {
      if (mounted) setState(() => _error = friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final summary = ref.watch(profileListProvider('/biomarkers/summary'));
    return _page(
      'New goal',
      AsyncBody<List<Json>>(
        summary,
        builder: (list) {
          if (list.isEmpty) {
            return const Empty(
              Icons.science_outlined,
              'No biomarkers yet',
              body: 'Add a lab value or upload a report first.',
            );
          }
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [
              DropdownButtonFormField<Json>(
                initialValue: _biomarker,
                decoration: const InputDecoration(labelText: 'Biomarker'),
                items: [
                  for (final b in list)
                    DropdownMenuItem(
                      value: b,
                      child: Text('${b['display_name']} (${b['unit']})'),
                    ),
                ],
                onChanged: (v) => setState(() => _biomarker = v),
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
                      decoration: const InputDecoration(labelText: 'At least'),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: TextField(
                      controller: _max,
                      keyboardType: const TextInputType.numberWithOptions(
                        decimal: true,
                      ),
                      decoration: const InputDecoration(labelText: 'At most'),
                    ),
                  ),
                ],
              ),
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

class InvestigationsScreen extends ConsumerWidget {
  const InvestigationsScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final inv = ref.watch(profileListProvider('/investigations'));
    void refresh() => ref.invalidate(profileListProvider('/investigations'));
    return _page(
      'Saved investigations',
      AsyncBody<List<Json>>(
        inv,
        onRetry: refresh,
        builder: (list) {
          if (list.isEmpty) {
            return const Empty(
              Icons.travel_explore,
              'No investigations',
              body: 'Save a health question and track its sources over time.',
            );
          }
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [
              for (final x in list)
                SCard(
                  child: Row(
                    children: [
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('${x['title']}', style: _bold),
                            Text(
                              '${x['question']}',
                              style: const TextStyle(
                                fontSize: 11,
                                color: Pal.mut,
                              ),
                            ),
                            const SizedBox(height: 4),
                            Text(
                              '${x['source_count']} sources · ${x['note_count']} notes',
                              style: const TextStyle(
                                fontSize: 10,
                                color: Pal.mut,
                              ),
                            ),
                          ],
                        ),
                      ),
                      IconButton(
                        icon: const Icon(Icons.delete_outline, size: 18),
                        onPressed: () async {
                          if (!await _confirm(
                            context,
                            'Delete this investigation?',
                          )) {
                            return;
                          }
                          try {
                            await ref
                                .read(apiProvider)
                                .delete(
                                  '/profiles/${ref.read(profileIdProvider)}/investigations/${x['id']}',
                                );
                            refresh();
                          } catch (e) {
                            if (context.mounted) toast(context, friendly(e));
                          }
                        },
                      ),
                    ],
                  ),
                ),
            ],
          );
        },
      ),
      fab: FloatingActionButton.extended(
        onPressed: () async {
          final q = TextEditingController();
          final text = await showDialog<String>(
            context: context,
            builder: (_) => AlertDialog(
              title: const Text('What do you want to look into?'),
              content: TextField(
                controller: q,
                autofocus: true,
                maxLength: 500,
                minLines: 2,
                maxLines: 4,
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(context),
                  child: const Text('Cancel'),
                ),
                FilledButton(
                  onPressed: () => Navigator.pop(context, q.text.trim()),
                  child: const Text('Save'),
                ),
              ],
            ),
          );
          if (text == null || text.length < 2) return;
          try {
            await ref.read(apiProvider).post(
              '/profiles/${ref.read(profileIdProvider)}/investigations',
              {'question': text},
            );
            refresh();
          } catch (e) {
            if (context.mounted) toast(context, friendly(e));
          }
        },
        icon: const Icon(Icons.add),
        label: const Text('New'),
      ),
    );
  }
}

// ------------------------------------------------------------- Care plan

class CarePlanScreen extends ConsumerWidget {
  const CarePlanScreen({super.key});

  Future<void> _add(BuildContext context, WidgetRef ref) async {
    final title = TextEditingController();
    final due = TextEditingController();
    var kind = 'other';
    final ok = await showDialog<bool>(
      context: context,
      builder: (_) => StatefulBuilder(
        builder: (ctx, set) => AlertDialog(
          title: const Text('New follow-up'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: title,
                autofocus: true,
                decoration: const InputDecoration(labelText: 'What to do'),
              ),
              DropdownButtonFormField<String>(
                initialValue: kind,
                decoration: const InputDecoration(labelText: 'Type'),
                items: [
                  for (final k in const [
                    'investigation',
                    'appointment',
                    'medication',
                    'other',
                  ])
                    DropdownMenuItem(value: k, child: Text(k)),
                ],
                onChanged: (v) => set(() => kind = v ?? 'other'),
              ),
              TextField(
                controller: due,
                decoration: const InputDecoration(
                  labelText: 'Due (YYYY-MM-DD, optional)',
                ),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Add'),
            ),
          ],
        ),
      ),
    );
    if (ok != true || title.text.trim().isEmpty) return;
    try {
      await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/follow-ups',
        {
          'kind': kind,
          'title': title.text.trim(),
          if (due.text.trim().isNotEmpty) 'due_on': due.text.trim(),
        },
      );
      ref.invalidate(profileListProvider('/follow-ups'));
    } catch (e) {
      if (context.mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final goals = ref.watch(profileListProvider('/goals'));
    final fus = ref.watch(profileListProvider('/follow-ups'));
    return _page(
      'Care plan',
      RefreshIndicator(
        onRefresh: () async {
          ref.invalidate(profileListProvider('/goals'));
          ref.invalidate(profileListProvider('/follow-ups'));
        },
        child: ListView(
          padding: const EdgeInsets.all(14),
          children: [
            const Text(
              'Goals and follow-ups in one place. The agent proposes, you approve.',
              style: _note,
            ),
            SectionTitle(
              'Goals',
              trailing: TextButton(
                onPressed: () => _open(context, const GoalsScreen()),
                child: const Text('Manage'),
              ),
            ),
            AsyncBody<List<Json>>(
              goals,
              builder: (list) {
                final active = list.where((g) => g['status'] == 'active');
                if (active.isEmpty) {
                  return const Text('No active goals.', style: _note);
                }
                return Column(
                  children: [
                    for (final g in active)
                      SCard(child: Text('${g['title']}', style: _bold)),
                  ],
                );
              },
            ),
            SectionTitle(
              'Follow-ups',
              trailing: TextButton(
                onPressed: () => _add(context, ref),
                child: const Text('Add'),
              ),
            ),
            AsyncBody<List<Json>>(
              fus,
              onRetry: () => ref.invalidate(profileListProvider('/follow-ups')),
              builder: (list) {
                final open = list.where((f) => f['status'] == 'open').toList();
                if (open.isEmpty) {
                  return const Text('Nothing open.', style: _note);
                }
                return Column(
                  children: [
                    for (final f in open)
                      SCard(
                        child: Row(
                          children: [
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text('${f['title']}', style: _bold),
                                  if (f['due_on'] != null)
                                    Text('Due ${f['due_on']}', style: _note),
                                ],
                              ),
                            ),
                            TextButton(
                              onPressed: () async {
                                try {
                                  await ref.read(apiProvider).patch(
                                    '/follow-ups/${f['id']}',
                                    {'status': 'done'},
                                  );
                                  ref.invalidate(
                                    profileListProvider('/follow-ups'),
                                  );
                                } catch (e) {
                                  if (context.mounted) {
                                    toast(context, friendly(e));
                                  }
                                }
                              },
                              child: const Text('Done'),
                            ),
                          ],
                        ),
                      ),
                  ],
                );
              },
            ),
          ],
        ),
      ),
    );
  }
}
