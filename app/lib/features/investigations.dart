import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'insights.dart';

const _note = TextStyle(fontSize: 10.5, color: Pal.mut);
const _bold = TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600);

/// The list response nests the rows under `investigations`, so it cannot use the plain list
/// provider.
final investigationsProvider = FutureProvider.autoDispose<List<Json>>((
  ref,
) async {
  final id = ref.watch(profileIdProvider);
  if (id == null) return [];
  return ref
      .read(apiProvider)
      .list('/profiles/$id/investigations', key: 'investigations');
});

/// Research questions worth following, drawn from the person's own out-of-range results,
/// active conditions and medicines.
final investigationIdeasProvider = FutureProvider.autoDispose<List<String>>((
  ref,
) async {
  final ideas = <String>[];
  try {
    final labs = await ref.watch(
      profileListProvider('/biomarkers/summary').future,
    );
    for (final b in labs) {
      final flag = '${(b['latest'] as Map?)?['flag'] ?? ''}';
      if (flag.contains('high')) {
        ideas.add('What raises ${b['display_name']} and how can I lower it?');
      } else if (flag.contains('low')) {
        ideas.add(
          'What causes low ${b['display_name']} and how is it treated?',
        );
      }
    }
    final history = await ref.watch(profileListProvider('/history').future);
    for (final e in history) {
      if (e['source'] != 'report' &&
          e['kind'] == 'condition' &&
          e['status'] == 'active') {
        ideas.add('Latest guidance on managing ${e['title']}');
      }
    }
    final rx = await ref.watch(profileListProvider('/prescriptions').future);
    for (final p in rx) {
      for (final m in (p['items'] as List? ?? []).cast<Map>()) {
        ideas.add('Side effects and interactions of ${m['name']}');
      }
    }
  } catch (_) {}
  return ideas.toSet().take(8).toList();
});

/// Saved health questions, each kept up to date with approved research sources.
class InvestigationsScreen extends ConsumerWidget {
  const InvestigationsScreen({super.key});

  Future<void> _save(BuildContext context, WidgetRef ref, String q) async {
    final id = ref.read(profileIdProvider);
    final api = ref.read(apiProvider);
    try {
      final r = await api.post('/profiles/$id/investigations', {'question': q});
      final map = r is Map ? (r['investigation'] ?? r) as Map : const {};
      final newId = map['id'];
      final known = ((map['source_count'] as num?) ?? 0) > 0;
      ref.invalidate(investigationsProvider);
      if (context.mounted) {
        toast(
          context,
          known
              ? 'You already follow this question.'
              : 'Saved. Sehat is finding research and guides for it.',
        );
      }
      if (newId != null && !known) {
        api
            .post('/profiles/$id/investigations/$newId/refresh', {})
            .then((result) {
              ref.invalidate(investigationsProvider);
              if (result is Map &&
                  result['web_unavailable'] == true &&
                  context.mounted) {
                toast(
                  context,
                  'Online research search is unavailable. Try again later.',
                );
              }
            })
            .catchError((Object e) {
              if (context.mounted) {
                toast(context, 'Research search failed: ${friendly(e)}');
              }
            });
      }
    } catch (e) {
      if (context.mounted) toast(context, friendly(e));
    }
  }

  Future<void> _ask(BuildContext context, WidgetRef ref) async {
    final q = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => const _QuestionSheet(),
    );
    if (q != null && q.length >= 2 && context.mounted) {
      await _save(context, ref, q);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final inv = ref.watch(investigationsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Health Research')),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _ask(context, ref),
        icon: const Icon(Icons.add),
        label: const Text('New question'),
      ),
      body: RefreshIndicator(
        onRefresh: () async => ref.refresh(investigationsProvider.future),
        child: AsyncBody<List<Json>>(
          inv,
          onRetry: () => ref.invalidate(investigationsProvider),
          builder: (list) {
            return ListView(
              padding: const EdgeInsets.fromLTRB(14, 8, 14, 90),
              children: [
                const Text(
                  'Follow a question about your health. Sehat keeps finding new, vetted research for it.',
                  style: _note,
                ),
                const SizedBox(height: 8),
                InsightPanel(
                  path: '/agent/insights/research',
                  title: 'Questions worth exploring',
                  icon: Icons.psychology_outlined,
                  itemLabel: 'Follow this question',
                  onItem: (it) => _save(context, ref, '${it['title']}'),
                ),
                SectionTitle('Following · ${list.length}'),
                if (list.isEmpty)
                  const Empty(
                    Icons.travel_explore,
                    'Nothing followed yet',
                    body: 'Pick a suggestion above or ask your own question.',
                  ),
                for (final x in list)
                  SCard(
                    onTap: () async {
                      await Navigator.push(
                        context,
                        MaterialPageRoute<void>(
                          builder: (_) => InvestigationScreen('${x['id']}'),
                        ),
                      );
                      ref.invalidate(investigationsProvider);
                    },
                    child: Row(
                      children: [
                        const CircleAvatar(
                          radius: 18,
                          backgroundColor: Pal.priBg,
                          child: Icon(
                            Icons.travel_explore,
                            size: 18,
                            color: Pal.pri,
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text('${x['title']}', style: _bold),
                              const SizedBox(height: 2),
                              Text(
                                '${x['source_count']} sources · ${x['note_count']} notes',
                                style: _note,
                              ),
                            ],
                          ),
                        ),
                        const Icon(Icons.chevron_right, color: Pal.mut),
                      ],
                    ),
                  ),
              ],
            );
          },
        ),
      ),
    );
  }
}

class _QuestionSheet extends StatefulWidget {
  const _QuestionSheet();
  @override
  State<_QuestionSheet> createState() => _QuestionSheetState();
}

class _QuestionSheetState extends State<_QuestionSheet> {
  final _q = TextEditingController();
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
        const Text(
          'What do you want to understand?',
          style: TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _q,
          autofocus: true,
          maxLength: 500,
          minLines: 2,
          maxLines: 4,
          textCapitalization: TextCapitalization.sentences,
          decoration: const InputDecoration(
            hintText: 'e.g. Does vitamin D help with low energy?',
          ),
        ),
        SizedBox(
          width: double.infinity,
          child: FilledButton(
            onPressed: () => Navigator.pop(context, _q.text.trim()),
            child: const Text('Follow this question'),
          ),
        ),
      ],
    ),
  );
}

/// One followed question with its sources and notes.
class InvestigationScreen extends ConsumerStatefulWidget {
  const InvestigationScreen(this.id, {super.key});
  final String id;
  @override
  ConsumerState<InvestigationScreen> createState() => _InvestigationState();
}

class _InvestigationState extends ConsumerState<InvestigationScreen> {
  Json? _data;
  Object? _error;
  bool _busy = false;
  String? _generalAnswer;

  String get _base =>
      '/profiles/${ref.read(profileIdProvider)}/investigations/${widget.id}';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final d = await ref.read(apiProvider).map(_base);
      if (mounted) {
        setState(() {
          _data = d;
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<bool> _run(Future<void> Function() f, [String? done]) async {
    setState(() => _busy = true);
    try {
      await f();
      await _load();
      if (done != null && mounted) toast(context, done);
      return true;
    } catch (e) {
      if (mounted) toast(context, friendly(e));
      return false;
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _refreshSources() async {
    var added = 0;
    var unavailable = false;
    final success = await _run(() async {
      final r = await ref.read(apiProvider).post('$_base/refresh', {});
      added = r is Map ? (r['added'] as num?)?.toInt() ?? 0 : 0;
      unavailable = r is Map && r['web_unavailable'] == true;
    });
    if (!mounted || !success) return;
    toast(
      context,
      unavailable
          ? 'Online search is unavailable. Try again later.'
          : added == 0
          ? 'No new sources found.'
          : '$added new source${added == 1 ? '' : 's'} found.',
    );
  }

  Future<void> _askCopilot() async {
    setState(() => _busy = true);
    try {
      final result = await ref
          .read(apiProvider)
          .post('$_base/general-answer', {});
      if (result is! Map || result['answer'] is! String) {
        throw StateError('Copilot returned an invalid answer.');
      }
      if (mounted) setState(() => _generalAnswer = result['answer'] as String);
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _addNote() async {
    final c = TextEditingController();
    final text = await showDialog<String>(
      context: context,
      builder: (_) => AlertDialog(
        title: const Text('Add a note'),
        content: TextField(controller: c, autofocus: true, maxLines: 4),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, c.text.trim()),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (text == null || text.isEmpty) return;
    await _run(
      () => ref.read(apiProvider).post('$_base/items', {'note': text}),
    );
  }

  Future<void> _delete() async {
    final sure = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Stop following this question?'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(c, false),
            child: const Text('Keep'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (sure != true) return;
    try {
      await ref.read(apiProvider).delete(_base);
      if (mounted) Navigator.pop(context);
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final inv = (_data?['investigation'] as Map?)?.cast<String, dynamic>();
    final items = ((_data?['items'] as List?) ?? []).cast<Json>();
    final sources = items.where((i) => i['kind'] != 'note').toList();
    final notes = items.where((i) => i['kind'] == 'note').toList();
    return Scaffold(
      appBar: AppBar(
        title: const Text('Research'),
        actions: [
          IconButton(
            tooltip: 'Stop following',
            icon: const Icon(Icons.delete_outline),
            onPressed: _delete,
          ),
        ],
      ),
      body: _error != null
          ? ErrorView(_error!, onRetry: _load)
          : inv == null
          ? const Padding(padding: EdgeInsets.all(20), child: Skeleton(rows: 3))
          : ListView(
              padding: const EdgeInsets.all(14),
              children: [
                Text(
                  '${inv['question']}',
                  style: const TextStyle(
                    fontSize: 16,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 10),
                Row(
                  children: [
                    FilledButton.icon(
                      onPressed: _busy ? null : _refreshSources,
                      icon: const Icon(Icons.refresh, size: 18),
                      label: const Text('Find new research'),
                    ),
                    const SizedBox(width: 8),
                    OutlinedButton.icon(
                      onPressed: _busy ? null : _addNote,
                      icon: const Icon(Icons.edit_note, size: 18),
                      label: const Text('Add note'),
                    ),
                  ],
                ),
                if (_busy)
                  const Padding(
                    padding: EdgeInsets.only(top: 10),
                    child: LinearProgressIndicator(),
                  ),
                SectionTitle('Sources · ${sources.length}'),
                if (sources.isEmpty)
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        'No sources yet. Search again, or ask Copilot for general information. Only your question is sent; AI consent and cloud model permission are required.',
                        style: _note,
                      ),
                      TextButton.icon(
                        onPressed: _busy ? null : _askCopilot,
                        icon: const Icon(Icons.auto_awesome),
                        label: const Text('Ask Copilot'),
                      ),
                      if (_generalAnswer != null)
                        SCard(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              const Text(
                                'Copilot · uncited general information',
                                style: _bold,
                              ),
                              const SizedBox(height: 4),
                              const Text(
                                'Not a research source or medical advice. Verify with a clinician.',
                                style: _note,
                              ),
                              const SizedBox(height: 8),
                              Text(_generalAnswer!),
                            ],
                          ),
                        ),
                    ],
                  ),
                for (final s in sources)
                  SCard(
                    onTap: s['url'] == null
                        ? null
                        : () => launchUrl(
                            Uri.parse('${s['url']}'),
                            mode: LaunchMode.externalApplication,
                          ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('${s['title'] ?? s['identifier']}', style: _bold),
                        if (s['excerpt'] != null)
                          Padding(
                            padding: const EdgeInsets.only(top: 4),
                            child: Text(
                              '${s['excerpt']}',
                              maxLines: 4,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(
                                fontSize: 11.5,
                                color: Pal.mut,
                              ),
                            ),
                          ),
                        Padding(
                          padding: const EdgeInsets.only(top: 4),
                          child: Text(
                            [
                                  switch (s['web_kind']) {
                                    'paper' => 'Research paper',
                                    'trial' => 'Clinical trial',
                                    'guide' => 'Health guide',
                                    _ => null,
                                  },
                                  s['publisher'],
                                  s['year'],
                                  s['identifier'],
                                ]
                                .where((e) => e != null && '$e'.isNotEmpty)
                                .join(' · '),
                            style: _note,
                          ),
                        ),
                      ],
                    ),
                  ),
                if (notes.isNotEmpty) SectionTitle('Notes · ${notes.length}'),
                for (final n in notes)
                  SCard(
                    child: Row(
                      children: [
                        Expanded(
                          child: Text(
                            '${n['note']}',
                            style: const TextStyle(fontSize: 12),
                          ),
                        ),
                        IconButton(
                          icon: const Icon(Icons.close, size: 16),
                          onPressed: () => _run(
                            () => ref
                                .read(apiProvider)
                                .delete('$_base/items/${n['id']}'),
                          ),
                        ),
                      ],
                    ),
                  ),
                const Padding(
                  padding: EdgeInsets.only(top: 12),
                  child: Text(
                    'Research summaries are general information from approved public sources, not a substitute for your doctor.',
                    style: _note,
                  ),
                ),
              ],
            ),
    );
  }
}
