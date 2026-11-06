import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:share_plus/share_plus.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'vault.dart' show DocumentScreen;

Widget _page(String title, Widget body, {List<Widget>? actions}) => Scaffold(
  appBar: AppBar(title: Text(title), actions: actions),
  body: body,
);

const _small = TextStyle(fontSize: 10.5, color: Pal.mut);
const _bold = TextStyle(fontSize: 12, fontWeight: FontWeight.w600);

String? _pid(WidgetRef ref) => ref.read(profileIdProvider);

Future<String?> _ask(
  BuildContext context,
  String title, {
  String label = '',
  TextInputType? type,
}) {
  final c = TextEditingController();
  return showDialog<String>(
    context: context,
    builder: (_) => AlertDialog(
      title: Text(title),
      content: TextField(
        controller: c,
        keyboardType: type,
        decoration: InputDecoration(labelText: label),
        autofocus: true,
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(context, c.text.trim()),
          child: const Text('OK'),
        ),
      ],
    ),
  );
}

// ------------------------------------------------------------ Family sharing

final _sharesProvider = FutureProvider.autoDispose<List<Json>>((ref) {
  final id = ref.watch(profileIdProvider);
  if (id == null) return <Json>[];
  return ref.read(apiProvider).list('/profiles/$id/shares', key: 'shares');
});

final _sharedWithMeProvider = FutureProvider.autoDispose<List<Json>>(
  (ref) => ref.read(apiProvider).list('/shared-with-me', key: 'shares'),
);

const _scopeLabels = {
  'summary': 'Basic summary',
  'labs': 'Lab results',
  'history': 'Medical History',
  'follow_ups': 'Follow-ups',
};

class FamilyScreen extends ConsumerWidget {
  const FamilyScreen({super.key});

  Future<void> _create(BuildContext context, WidgetRef ref) async {
    final label = await _ask(
      context,
      'Who is this for?',
      label: 'Name, e.g. Dr. Rao',
    );
    if (label == null || label.isEmpty || !context.mounted) return;
    final scopes = <String>{'summary', 'labs'};
    final days = ValueNotifier<int>(30);
    final ok = await showDialog<bool>(
      context: context,
      builder: (_) => StatefulBuilder(
        builder: (ctx, set) => AlertDialog(
          title: const Text('What can they see?'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              for (final e in _scopeLabels.entries)
                CheckboxListTile(
                  dense: true,
                  value: scopes.contains(e.key),
                  title: Text(e.value, style: const TextStyle(fontSize: 12)),
                  onChanged: (v) =>
                      set(() => v! ? scopes.add(e.key) : scopes.remove(e.key)),
                ),
              DropdownButton<int>(
                value: days.value,
                items: const [
                  DropdownMenuItem(value: 7, child: Text('Expires in 7 days')),
                  DropdownMenuItem(
                    value: 30,
                    child: Text('Expires in 30 days'),
                  ),
                  DropdownMenuItem(
                    value: 90,
                    child: Text('Expires in 90 days'),
                  ),
                ],
                onChanged: (v) => set(() => days.value = v!),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: scopes.isEmpty ? null : () => Navigator.pop(ctx, true),
              child: const Text('Create code'),
            ),
          ],
        ),
      ),
    );
    if (ok != true || !context.mounted) return;
    try {
      final r = await ref.read(apiProvider).post(
        '/profiles/${_pid(ref)}/shares',
        {
          'label': label,
          'scopes': scopes.toList(),
          'expires_in_days': days.value,
        },
      ) as Map;
      ref.invalidate(_sharesProvider);
      if (!context.mounted) return;
      await showDialog<void>(
        context: context,
        builder: (_) => AlertDialog(
          title: const Text('Share code'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              SelectableText(
                '${r['code']}',
                style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
              ),
              const SizedBox(height: 8),
              const Text(
                'Shown once. Send it to them privately; it works for one person.',
                style: _small,
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () {
                Clipboard.setData(ClipboardData(text: '${r['code']}'));
                toast(context, 'Copied');
              },
              child: const Text('Copy'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('Done'),
            ),
          ],
        ),
      );
    } catch (e) {
      if (context.mounted) toast(context, friendly(e));
    }
  }

  Future<void> _accept(BuildContext context, WidgetRef ref) async {
    final code = await _ask(
      context,
      'Enter a share code',
      label: 'Code they gave you',
    );
    if (code == null || code.isEmpty) return;
    try {
      await ref.read(apiProvider).post('/shares/accept', {'code': code});
      ref.invalidate(_sharedWithMeProvider);
      if (context.mounted) toast(context, 'Added to Shared with me');
    } catch (e) {
      if (context.mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final mine = ref.watch(_sharesProvider);
    final theirs = ref.watch(_sharedWithMeProvider);
    return _page(
      'Family & Caregiver Access',
      ListView(
        padding: const EdgeInsets.all(14),
        children: [
          Row(
            children: [
              Expanded(
                child: FilledButton(
                  onPressed: () => _create(context, ref),
                  child: const Text('Share my profile'),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: OutlinedButton(
                  onPressed: () => _accept(context, ref),
                  child: const Text('Enter a code'),
                ),
              ),
            ],
          ),
          const SectionTitle('Shared by me'),
          AsyncBody<List<Json>>(
            mine,
            onRetry: () => ref.invalidate(_sharesProvider),
            builder: (l) => l.isEmpty
                ? const Empty(
                    Icons.group_outlined,
                    'Not shared with anyone',
                    body: 'Create a code to give someone read-only access.',
                  )
                : Column(
                    children: [
                      for (final s in l)
                        SCard(
                          child: Row(
                            children: [
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text('${s['label']}', style: _bold),
                                    Text(
                                      ((s['scopes'] as List?) ?? [])
                                          .map((x) => _scopeLabels[x] ?? '$x')
                                          .join(' · '),
                                      style: _small,
                                    ),
                                  ],
                                ),
                              ),
                              Pill('${s['status']}'),
                              if (s['status'] != 'revoked')
                                IconButton(
                                  tooltip: 'Revoke',
                                  icon: const Icon(Icons.block, size: 18),
                                  onPressed: () async {
                                    await ref
                                        .read(apiProvider)
                                        .delete(
                                          '/profiles/${_pid(ref)}/shares/${s['id']}',
                                        );
                                    ref.invalidate(_sharesProvider);
                                  },
                                ),
                            ],
                          ),
                        ),
                    ],
                  ),
          ),
          const SectionTitle('Shared with me'),
          AsyncBody<List<Json>>(
            theirs,
            onRetry: () => ref.invalidate(_sharedWithMeProvider),
            builder: (l) => l.isEmpty
                ? const Empty(
                    Icons.inbox_outlined,
                    'Nothing shared with you',
                    body: 'Enter a code someone sent you.',
                  )
                : Column(
                    children: [
                      for (final s in l)
                        SCard(
                          onTap: () => Navigator.push(
                            context,
                            MaterialPageRoute(
                              builder: (_) => SharedViewScreen('${s['id']}'),
                            ),
                          ),
                          child: Row(
                            children: [
                              Expanded(
                                child: Text(
                                  '${s['display_name']}',
                                  style: _bold,
                                ),
                              ),
                              IconButton(
                                tooltip: 'Leave',
                                icon: const Icon(Icons.logout, size: 18),
                                onPressed: () async {
                                  await ref
                                      .read(apiProvider)
                                      .delete('/shared-with-me/${s['id']}');
                                  ref.invalidate(_sharedWithMeProvider);
                                },
                              ),
                            ],
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

class SharedViewScreen extends ConsumerWidget {
  const SharedViewScreen(this.id, {super.key});
  final String id;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final v = ref.watch(_sharedViewProvider(id));
    return _page(
      'Shared profile',
      AsyncBody<Json>(
        v,
        onRetry: () => ref.invalidate(_sharedViewProvider(id)),
        builder: (d) => ListView(
          padding: const EdgeInsets.all(14),
          children: [
            Text(
              '${d['display_name']}',
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
            const Text('Read-only view', style: _small),
            for (final k in ['summary', 'labs', 'history', 'follow_ups'])
              if (d[k] != null) ...[
                SectionTitle(_scopeLabels[k]!),
                SCard(child: _Dump(d[k])),
              ],
          ],
        ),
      ),
    );
  }
}

final _sharedViewProvider = FutureProvider.autoDispose.family<Json, String>(
  (ref, id) => ref.read(apiProvider).map('/shared-with-me/$id'),
);

class _Dump extends StatelessWidget {
  const _Dump(this.v);
  final Object? v;
  @override
  Widget build(BuildContext context) {
    final rows = <String>[];
    if (v is List) {
      for (final e in (v as List)) {
        rows.add(
          e is Map
              ? e.entries
                    .where((x) => x.value != null)
                    .map((x) => '${x.value}')
                    .join(' · ')
              : '$e',
        );
      }
    } else if (v is Map) {
      for (final e in (v as Map).entries.where((x) => x.value != null)) {
        rows.add('${e.key}: ${e.value}');
      }
    }
    if (rows.isEmpty) return const Text('Nothing shared.', style: _small);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final r in rows)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 2),
            child: Text(r, style: const TextStyle(fontSize: 11.5)),
          ),
      ],
    );
  }
}

// --------------------------------------------------------- Data & retention

class DataControlScreen extends ConsumerStatefulWidget {
  const DataControlScreen({super.key});
  @override
  ConsumerState<DataControlScreen> createState() => _DataControlState();
}

class _DataControlState extends ConsumerState<DataControlScreen> {
  static const _options = <(int?, String)>[
    (null, 'Keep'),
    (730, '2 years'),
    (365, '1 year'),
    (90, '90 days'),
    (30, '30 days'),
  ];
  int? _ai, _dev;
  bool _loaded = false, _busy = false, _exporting = false;
  Json? _summary;

  Future<void> _save() async {
    try {
      await ref.read(apiProvider).put('/profiles/${_pid(ref)}/retention', {
        'ai_history_days': _ai,
        'device_readings_days': _dev,
      });
      ref.invalidate(profileMapProvider('/retention'));
      if (mounted) toast(context, 'Retention updated');
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  Future<void> _cleanUp() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (_) => AlertDialog(
        title: const Text('Clean up now?'),
        content: const Text(
          'Assistant history and device readings older than your limits are permanently deleted. Your reports and results are never touched.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete older data'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    setState(() => _busy = true);
    try {
      final r =
          await ref
                  .read(apiProvider)
                  .post('/profiles/${_pid(ref)}/retention/apply')
              as Map?;
      final n = (r?.values ?? []).whereType<num>().fold<num>(
        0,
        (a, b) => a + b,
      );
      ref.invalidate(profileMapProvider('/retention'));
      if (mounted) {
        toast(
          context,
          n == 0
              ? 'Nothing was older than your limits'
              : '$n old items deleted',
        );
      }
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<String?> _fetchExport() async {
    setState(() => _exporting = true);
    try {
      final d = await ref
          .read(apiProvider)
          .get('/profiles/${_pid(ref)}/export');
      final m = d is Map ? d.cast<String, dynamic>() : <String, dynamic>{};
      if (mounted) setState(() => _summary = m);
      return const JsonEncoder.withIndent('  ').convert(d);
    } catch (e) {
      if (mounted) toast(context, friendly(e));
      return null;
    } finally {
      if (mounted) setState(() => _exporting = false);
    }
  }

  Future<void> _copy() async {
    final text = await _fetchExport();
    if (text == null) return;
    await Clipboard.setData(ClipboardData(text: text));
    if (mounted) toast(context, 'Copied. Paste it anywhere you keep backups.');
  }

  Future<void> _share() async {
    final text = await _fetchExport();
    if (text == null) return;
    try {
      await SharePlus.instance.share(
        ShareParams(
          files: [
            XFile.fromData(
              utf8.encode(text),
              mimeType: 'application/json',
              name: 'sehat-health-data.json',
            ),
          ],
          fileNameOverrides: ['sehat-health-data.json'],
        ),
      );
    } catch (_) {
      await Clipboard.setData(ClipboardData(text: text));
      if (mounted)
        toast(context, 'Sharing is not available here. Copied instead.');
    }
  }

  static const _labels = {
    'documents': 'Reports',
    'observations': 'Lab results',
    'lifestyle_logs': 'Habit logs',
    'symptom_entries': 'Symptoms',
    'history_entries': 'History',
    'goals': 'Goals',
    'follow_ups': 'Follow-ups',
    'health_questions': 'Assistant questions',
    'research_investigations': 'Research',
    'device_readings': 'Device readings',
  };

  Widget _policy(
    IconData icon,
    String title,
    String body,
    int? value,
    ValueChanged<int?> on,
  ) => SCard(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(icon, size: 18, color: Pal.pri),
            const SizedBox(width: 8),
            Text(title, style: _bold),
          ],
        ),
        const SizedBox(height: 4),
        Text(body, style: _small),
        const SizedBox(height: 8),
        Wrap(
          spacing: 6,
          runSpacing: 4,
          children: [
            for (final (days, label) in _options)
              ChoiceChip(
                label: Text(label, style: const TextStyle(fontSize: 11)),
                selected: value == days,
                onSelected: (_) {
                  setState(() => on(days));
                  _save();
                },
              ),
          ],
        ),
      ],
    ),
  );

  @override
  Widget build(BuildContext context) {
    final r = ref.watch(profileMapProvider('/retention'));
    return _page(
      'Data Retention & Export',
      AsyncBody<Json>(
        r,
        onRetry: () => ref.invalidate(profileMapProvider('/retention')),
        builder: (p) {
          if (!_loaded) {
            _ai = (p['ai_history_days'] as num?)?.toInt();
            _dev = (p['device_readings_days'] as num?)?.toInt();
            _loaded = true;
          }
          final last = DateTime.tryParse('${p['last_applied_at'] ?? ''}');
          final counts = <MapEntry<String, int>>[
            for (final e in _labels.entries)
              if (((_summary?[e.key] as List?)?.length ?? 0) > 0)
                MapEntry(e.value, (_summary![e.key] as List).length),
          ];
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [
              const SectionTitle('How long we keep it'),
              _policy(
                Icons.chat_bubble_outline,
                'Assistant history',
                'Questions you asked Sehat and its answers.',
                _ai,
                (x) => _ai = x,
              ),
              _policy(
                Icons.watch_outlined,
                'Device readings',
                'Steps, heart rate and other readings from connected devices.',
                _dev,
                (x) => _dev = x,
              ),
              SCard(
                color: Pal.priBg,
                child: Row(
                  children: [
                    const Icon(Icons.lock_outline, size: 16, color: Pal.pri),
                    const SizedBox(width: 8),
                    const Expanded(
                      child: Text(
                        'Reports, lab results and medical history are kept until you delete them.',
                        style: TextStyle(fontSize: 11, height: 1.3),
                      ),
                    ),
                  ],
                ),
              ),
              OutlinedButton.icon(
                onPressed: _busy ? null : _cleanUp,
                icon: const Icon(Icons.cleaning_services_outlined, size: 16),
                label: Text(
                  last == null
                      ? 'Clean up now'
                      : 'Clean up now · last run ${last.toLocal().day}/${last.toLocal().month}/${last.toLocal().year}',
                ),
              ),
              const SectionTitle('Take your data with you'),
              SCard(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'A complete copy of your profile, reports, results, habits, care plan and consents in one JSON file.',
                      style: TextStyle(fontSize: 11.5, height: 1.35),
                    ),
                    if (counts.isNotEmpty) ...[
                      const SizedBox(height: 10),
                      Wrap(
                        spacing: 6,
                        runSpacing: 6,
                        children: [
                          for (final c in counts) Pill('${c.value} ${c.key}'),
                        ],
                      ),
                    ],
                    const SizedBox(height: 10),
                    Row(
                      children: [
                        Expanded(
                          child: FilledButton.icon(
                            onPressed: _exporting ? null : _share,
                            icon: _exporting
                                ? const SizedBox(
                                    width: 14,
                                    height: 14,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                    ),
                                  )
                                : const Icon(Icons.ios_share, size: 16),
                            label: const Text('Share as file'),
                          ),
                        ),
                        const SizedBox(width: 8),
                        OutlinedButton(
                          onPressed: _exporting ? null : _copy,
                          child: const Text('Copy'),
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
    );
  }
}

// ------------------------------------------------------------ Medical history

const _kinds = {
  'condition': 'Condition',
  'allergy': 'Allergy',
  'surgery': 'Surgery',
  'family_history': 'Family history',
  'medication': 'Medication',
  'vaccination': 'Vaccination',
};

final _historyDocumentsProvider = FutureProvider.autoDispose<List<Json>>((
  ref,
) async {
  final id = ref.watch(profileIdProvider);
  if (id == null) return [];
  final documents = <Json>[];
  for (var offset = 0; offset <= 1000000; offset += 100) {
    final page = await ref
        .read(apiProvider)
        .list('/profiles/$id/documents?limit=100&offset=$offset');
    documents.addAll(page.where((d) => d['status'] == 'approved'));
    if (page.length < 100) return documents;
  }
  throw StateError('Too many documents to load.');
});

class HistoryScreen extends ConsumerWidget {
  const HistoryScreen({super.key});

  Future<void> _add(BuildContext context, WidgetRef ref) async {
    var kind = 'condition';
    final title = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (_) => StatefulBuilder(
        builder: (ctx, set) => AlertDialog(
          title: const Text('Add to history'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              DropdownButton<String>(
                isExpanded: true,
                value: kind,
                items: [
                  for (final e in _kinds.entries)
                    DropdownMenuItem(value: e.key, child: Text(e.value)),
                ],
                onChanged: (v) => set(() => kind = v!),
              ),
              TextField(
                controller: title,
                decoration: const InputDecoration(labelText: 'What'),
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
      await ref.read(apiProvider).post('/profiles/${_pid(ref)}/history', {
        'kind': kind,
        'title': title.text.trim(),
      });
      ref.invalidate(profileListProvider('/history'));
    } catch (e) {
      if (context.mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final h = ref.watch(profileListProvider('/history'));
    final documents = ref.watch(_historyDocumentsProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Medical History')),
      floatingActionButton: FloatingActionButton.small(
        onPressed: () => _add(context, ref),
        child: const Icon(Icons.add),
      ),
      body: AsyncBody<List<Json>>(
        h,
        onRetry: () => ref.invalidate(profileListProvider('/history')),
        builder: (l) {
          final fromReports = [
            for (final e in l)
              if (e['source'] == 'report') e,
          ];
          final byYou = [
            for (final e in l)
              if (e['source'] != 'report') e,
          ];
          Widget row(Json e) => SCard(
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('${e['title']}', style: _bold),
                      Text(
                        _kinds['${e['kind']}'] ?? '${e['kind']}',
                        style: _small,
                      ),
                    ],
                  ),
                ),
                Pill(e['source'] == 'report' ? 'Unverified' : '${e['status']}'),
                IconButton(
                  tooltip: 'Remove',
                  icon: const Icon(Icons.delete_outline, size: 18),
                  onPressed: () async {
                    await ref.read(apiProvider).delete('/history/${e['id']}');
                    ref.invalidate(profileListProvider('/history'));
                  },
                ),
              ],
            ),
          );
          return RefreshIndicator(
            onRefresh: () async {
              ref.invalidate(profileListProvider('/history'));
              ref.invalidate(_historyDocumentsProvider);
              await Future.wait([
                ref.read(profileListProvider('/history').future),
                ref.read(_historyDocumentsProvider.future),
              ]);
            },
            child: ListView(
              padding: const EdgeInsets.all(14),
              children: [
                const SCard(
                  child: Row(
                    children: [
                      Icon(Icons.auto_awesome, size: 18, color: Pal.pri),
                      SizedBox(width: 10),
                      Expanded(
                        child: Text(
                          'Your verified reports are listed below. Automatic history extraction is paused. Earlier AI-generated findings are unverified, not diagnoses.',
                          style: TextStyle(
                            fontSize: 12,
                            color: Pal.mut,
                            height: 1.35,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
                if (l.isEmpty)
                  const Empty(
                    Icons.medical_information_outlined,
                    'Nothing saved yet',
                    body: 'Add conditions, allergies and surgeries yourself. Your verified reports are listed below.',
                  ),
                if (fromReports.isNotEmpty) ...[
                  const SectionTitle('Report-generated findings · review'),
                  for (final e in fromReports) row(e),
                ],
                if (byYou.isNotEmpty) ...[
                  const SectionTitle('Added by you'),
                  for (final e in byYou) row(e),
                ],
                const SectionTitle('Verified reports'),
                AsyncBody<List<Json>>(
                  documents,
                  onRetry: () => ref.invalidate(_historyDocumentsProvider),
                  builder: (docs) {
                    if (docs.isEmpty) {
                      return const Text(
                        'No verified reports yet.',
                        style: _small,
                      );
                    }
                    return Column(
                      children: [
                        for (final d in docs)
                          SCard(
                            onTap: () => Navigator.push(
                              context,
                              MaterialPageRoute<void>(
                                builder: (_) => DocumentScreen('${d['id']}'),
                              ),
                            ),
                            child: Row(
                              children: [
                                const Icon(
                                  Icons.description_outlined,
                                  color: Pal.pri,
                                ),
                                const SizedBox(width: 10),
                                Expanded(
                                  child: Column(
                                    crossAxisAlignment:
                                        CrossAxisAlignment.start,
                                    children: [
                                      Text('${d['file_name']}', style: _bold),
                                      if (DateTime.tryParse(
                                            '${d['created_at']}',
                                          ) !=
                                          null)
                                        Text(
                                          'Uploaded ${'${d['created_at']}'.substring(0, 10)}',
                                          style: _small,
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
              ],
            ),
          );
        },
      ),
    );
  }
}

// ------------------------------------------------------------------ MCP tools

class McpScreen extends ConsumerStatefulWidget {
  const McpScreen({super.key});
  @override
  ConsumerState<McpScreen> createState() => _McpState();
}

class _McpState extends ConsumerState<McpScreen> {
  Json? _tok;
  bool _busy = false;

  Future<void> _mint() async {
    setState(() => _busy = true);
    try {
      final r = await ref
          .read(apiProvider)
          .post('/profiles/${_pid(ref)}/mcp-tokens');
      setState(() => _tok = (r as Map).cast<String, dynamic>());
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => _page(
    'Connected Apps',
    ListView(
      padding: const EdgeInsets.all(14),
      children: [
        const Text(
          'Let an AI assistant that supports MCP read this profile. The token is read-only, tied to this profile and short-lived.',
          style: TextStyle(fontSize: 11.5, color: Pal.mut),
        ),
        const SizedBox(height: 10),
        FilledButton(
          onPressed: _busy ? null : _mint,
          child: const Text('Create access token'),
        ),
        if (_tok != null) ...[
          const SectionTitle('Endpoint'),
          SCard(
            child: SelectableText(
              '${_tok!['endpoint']}',
              style: const TextStyle(fontFamily: 'monospace', fontSize: 11),
            ),
          ),
          const SectionTitle('Token'),
          SCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SelectableText(
                  '${_tok!['token']}',
                  style: const TextStyle(fontFamily: 'monospace', fontSize: 10),
                ),
                const SizedBox(height: 6),
                Text('Expires ${_tok!['expires_at']}', style: _small),
                TextButton(
                  onPressed: () {
                    Clipboard.setData(ClipboardData(text: '${_tok!['token']}'));
                    toast(context, 'Copied');
                  },
                  child: const Text('Copy token'),
                ),
              ],
            ),
          ),
        ],
      ],
    ),
  );
}
