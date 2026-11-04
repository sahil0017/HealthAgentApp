import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';

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
  'history': 'Medical history',
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
      'Family sharing',
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
  static const _options = <int?>[null, 30, 90, 365, 730];
  int? _ai, _dev;
  bool _loaded = false, _busy = false;

  String _name(int? d) => d == null ? 'Keep' : '$d days';

  Future<void> _save() async {
    setState(() => _busy = true);
    try {
      await ref.read(apiProvider).put('/profiles/${_pid(ref)}/retention', {
        'ai_history_days': _ai,
        'device_readings_days': _dev,
      });
      if (mounted) toast(context, 'Saved');
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _apply() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (_) => AlertDialog(
        title: const Text('Delete older data now?'),
        content: const Text(
          'Questions and device readings older than your limits are permanently deleted.',
        ),
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
    );
    if (ok != true) return;
    try {
      await _save();
      final r =
          await ref
                  .read(apiProvider)
                  .post('/profiles/${_pid(ref)}/retention/apply')
              as Map?;
      if (mounted) toast(context, 'Done${r == null ? '' : ': $r'}');
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  Future<void> _export() async {
    try {
      final d = await ref
          .read(apiProvider)
          .get('/profiles/${_pid(ref)}/export');
      await Clipboard.setData(
        ClipboardData(text: const JsonEncoder.withIndent('  ').convert(d)),
      );
      if (mounted) toast(context, 'Export copied to clipboard');
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final r = ref.watch(profileMapProvider('/retention'));
    return _page(
      'Data & retention',
      AsyncBody<Json>(
        r,
        onRetry: () => ref.invalidate(profileMapProvider('/retention')),
        builder: (p) {
          if (!_loaded) {
            _ai = (p['ai_history_days'] as num?)?.toInt();
            _dev = (p['device_readings_days'] as num?)?.toInt();
            _loaded = true;
          }
          Widget pick(String label, int? v, ValueChanged<int?> on) => Row(
            children: [
              Expanded(child: Text(label, style: _bold)),
              DropdownButton<int?>(
                value: _options.contains(v) ? v : null,
                items: [
                  for (final o in _options)
                    DropdownMenuItem(value: o, child: Text(_name(o))),
                ],
                onChanged: (x) => setState(() => on(x)),
              ),
            ],
          );
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [
              SCard(
                child: Column(
                  children: [
                    pick('Agent questions', _ai, (x) => _ai = x),
                    pick('Device readings', _dev, (x) => _dev = x),
                    Row(
                      children: [
                        Expanded(
                          child: FilledButton(
                            onPressed: _busy ? null : _save,
                            child: const Text('Save'),
                          ),
                        ),
                        const SizedBox(width: 8),
                        Expanded(
                          child: OutlinedButton(
                            onPressed: _busy ? null : _apply,
                            child: const Text('Apply now'),
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
              const SectionTitle('Your data'),
              SCard(
                child: Row(
                  children: [
                    const Expanded(
                      child: Text(
                        'Export everything as JSON (copied to clipboard).',
                        style: TextStyle(fontSize: 11.5),
                      ),
                    ),
                    OutlinedButton(
                      onPressed: _export,
                      child: const Text('Export'),
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
    return Scaffold(
      appBar: AppBar(title: const Text('Medical history')),
      floatingActionButton: FloatingActionButton.small(
        onPressed: () => _add(context, ref),
        child: const Icon(Icons.add),
      ),
      body: AsyncBody<List<Json>>(
        h,
        onRetry: () => ref.invalidate(profileListProvider('/history')),
        builder: (l) => l.isEmpty
            ? const Empty(
                Icons.medical_information_outlined,
                'No history yet',
                body: 'Conditions, allergies, surgeries and medications.',
              )
            : ListView(
                padding: const EdgeInsets.all(14),
                children: [
                  for (final e in l)
                    SCard(
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
                          Pill('${e['status']}'),
                          IconButton(
                            icon: const Icon(Icons.delete_outline, size: 18),
                            onPressed: () async {
                              await ref
                                  .read(apiProvider)
                                  .delete('/history/${e['id']}');
                              ref.invalidate(profileListProvider('/history'));
                            },
                          ),
                        ],
                      ),
                    ),
                ],
              ),
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
    'Connect AI tools',
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

// ------------------------------------------------- Add biomarker value (labs)

Future<void> showAddBiomarker(BuildContext context, WidgetRef ref) async {
  final name = TextEditingController();
  final value = TextEditingController();
  final unit = TextEditingController();
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: const Text('Add a lab value'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: name,
            decoration: const InputDecoration(labelText: 'Test, e.g. HbA1c'),
          ),
          TextField(
            controller: value,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: const InputDecoration(labelText: 'Value'),
          ),
          TextField(
            controller: unit,
            decoration: const InputDecoration(labelText: 'Unit, e.g. %'),
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
          child: const Text('Save'),
        ),
      ],
    ),
  );
  if (ok != true) return;
  final v = double.tryParse(value.text.trim());
  if (v == null || name.text.trim().isEmpty || unit.text.trim().isEmpty) {
    if (context.mounted) toast(context, 'Enter a test, a number and a unit.');
    return;
  }
  try {
    await ref.read(apiProvider).post('/profiles/${_pid(ref)}/observations', {
      'display_name': name.text.trim(),
      'value_numeric': v,
      'unit': unit.text.trim(),
      'observed_at': DateTime.now().toUtc().toIso8601String(),
    });
    ref.invalidate(profileListProvider('/biomarkers/summary'));
    if (context.mounted) toast(context, 'Saved');
  } catch (e) {
    if (context.mounted) toast(context, friendly(e));
  }
}
