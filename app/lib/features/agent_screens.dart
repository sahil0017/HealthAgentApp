import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:pdf/widgets.dart' as pw;
import 'package:printing/printing.dart';

import '../core/api.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'agent_chat.dart' show AgentChatScreen;

String _base(WidgetRef ref) => '/profiles/${ref.read(profileIdProvider)}/agent';

String _localDay() {
  final n = DateTime.now();
  return '${n.year}-${n.month.toString().padLeft(2, '0')}-${n.day.toString().padLeft(2, '0')}';
}

final _briefingProvider = FutureProvider.autoDispose<Json>((ref) {
  final id = ref.watch(profileIdProvider);
  return ref
      .read(apiProvider)
      .map('/profiles/$id/agent/briefing?day=${_localDay()}');
});

/// The Copilot agent's written briefing. Empty unless the cloud model is allowed; it can take a few seconds.
final _briefingSummaryProvider = FutureProvider.autoDispose<Json>((ref) {
  final id = ref.watch(profileIdProvider);
  return ref.read(apiProvider).map('/profiles/$id/agent/briefing/summary');
});

class _CopilotBrief extends ConsumerWidget {
  const _CopilotBrief();
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(_briefingSummaryProvider);
    final d = s.asData?.value;
    if (s.isLoading) {
      return const SCard(
        child: Row(
          children: [
            SizedBox(
              width: 16,
              height: 16,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
            SizedBox(width: 10),
            Expanded(
              child: Text('Sehat is preparing today’s briefing…', style: _note),
            ),
          ],
        ),
      );
    }
    final text = '${d?['summary'] ?? ''}';
    if (text.isEmpty) return const SizedBox.shrink();
    return SCard(
      color: Pal.priBg,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.auto_awesome, color: Pal.pri, size: 16),
              const SizedBox(width: 6),
              const Pill('Sehat AI'),
            ],
          ),
          const SizedBox(height: 8),
          Text(text, style: const TextStyle(fontSize: 13, height: 1.35)),
          const SizedBox(height: 4),
          const Text('Based on your own records', style: _note),
        ],
      ),
    );
  }
}

final _permissionsProvider = FutureProvider.autoDispose<List<Json>>((ref) {
  final id = ref.watch(profileIdProvider);
  return ref
      .read(apiProvider)
      .list('/profiles/$id/agent/permissions', key: 'permissions');
});

final _actionsProvider = FutureProvider.autoDispose<List<Json>>((ref) {
  final id = ref.watch(profileIdProvider);
  return ref
      .read(apiProvider)
      .list('/profiles/$id/agent/actions?status=proposed', key: 'actions');
});

final _activityProvider = FutureProvider.autoDispose<List<Json>>((ref) {
  final id = ref.watch(profileIdProvider);
  return ref
      .read(apiProvider)
      .list('/profiles/$id/agent/activity', key: 'events');
});

final _visitPrepProvider = FutureProvider.autoDispose<Json>((ref) {
  final id = ref.watch(profileIdProvider);
  return ref.read(apiProvider).map('/profiles/$id/agent/visit-prep');
});

Widget _page(String title, Widget body) => Scaffold(
  appBar: AppBar(title: Text(title)),
  body: body,
);

void _open(BuildContext c, Widget w) =>
    Navigator.push(c, MaterialPageRoute<void>(builder: (_) => w));

const _note = TextStyle(fontSize: 10.5, color: Pal.mut);

/// Entry card shown at the top of the Agent tab.
class AgentHubCard extends ConsumerWidget {
  const AgentHubCard({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final pending = ref.watch(_actionsProvider).asData?.value.length ?? 0;
    return Column(
      children: [
        SCard(
          onTap: () => _open(context, const BriefingScreen()),
          child: Row(
            children: [
              const Icon(Icons.wb_sunny_outlined, color: Pal.pri),
              const SizedBox(width: 10),
              const Expanded(
                child: Text(
                  'Daily Health Brief',
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                ),
              ),
              if (pending > 0) Pill('$pending to approve'),
              const Icon(Icons.chevron_right, color: Pal.mut),
            ],
          ),
        ),
        Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            ActionChip(
              avatar: const Icon(Icons.auto_awesome, size: 14),
              label: const Text('Run agent', style: TextStyle(fontSize: 11)),
              onPressed: () => _open(context, const RunAgentScreen()),
            ),
            ActionChip(
              avatar: const Icon(Icons.fact_check_outlined, size: 14),
              label: const Text('Approvals', style: TextStyle(fontSize: 11)),
              onPressed: () => _open(context, const ApprovalsScreen()),
            ),
            ActionChip(
              avatar: const Icon(Icons.event_note_outlined, size: 14),
              label: const Text('Visit prep', style: TextStyle(fontSize: 11)),
              onPressed: () => _open(context, const VisitPrepScreen()),
            ),
            ActionChip(
              avatar: const Icon(Icons.lock_outline, size: 14),
              label: const Text('Permissions', style: TextStyle(fontSize: 11)),
              onPressed: () => _open(context, const PermissionsScreen()),
            ),
            ActionChip(
              avatar: const Icon(Icons.history, size: 14),
              label: const Text('Activity', style: TextStyle(fontSize: 11)),
              onPressed: () => _open(context, const ActivityScreen()),
            ),
          ],
        ),
      ],
    );
  }
}

/// Today-tab summary of the agent's daily briefing.
class BriefingCard extends ConsumerWidget {
  const BriefingCard({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final b = ref.watch(_briefingProvider).asData?.value;
    if (b == null) return const SizedBox.shrink();
    final items = [
      for (final s in (b['sections'] as List? ?? []).cast<Json>())
        for (final i in (s['items'] as List? ?? []).cast<Json>()) i,
    ];
    final attention = items.where((i) => i['tone'] == 'attention').toList();
    final pending = numOf(b['pending_approvals'])?.toInt() ?? 0;
    return Column(
      children: [
        const _CopilotBrief(),
        SCard(
          onTap: () => _open(context, const BriefingScreen()),
          child: Row(
            children: [
              const Icon(Icons.auto_awesome, color: Pal.pri, size: 18),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'Daily Health Brief',
                      style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    if (attention.isEmpty)
                      Text(
                        items.isEmpty
                            ? 'Nothing new today.'
                            : '${items.length} items from your record. Tap for the full briefing.',
                        style: _note,
                      )
                    else
                      for (final a in attention.take(3))
                        Padding(
                          padding: const EdgeInsets.only(top: 4),
                          child: Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              const Padding(
                                padding: EdgeInsets.only(top: 2),
                                child: Icon(
                                  Icons.circle,
                                  size: 7,
                                  color: Pal.warn,
                                ),
                              ),
                              const SizedBox(width: 7),
                              Expanded(
                                child: Text(
                                  '${a['text']}',
                                  style: const TextStyle(
                                    fontSize: 11.5,
                                    height: 1.3,
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                    if (attention.length > 3)
                      Text('+${attention.length - 3} more', style: _note),
                    if (pending > 0)
                      Text('$pending waiting for your approval', style: _note),
                  ],
                ),
              ),
              const Icon(Icons.chevron_right, color: Pal.mut),
            ],
          ),
        ),
      ],
    );
  }
}

const _sectionIcons = {
  'labs': Icons.biotech_outlined,
  'follow_ups': Icons.event_repeat,
  'symptoms': Icons.sick_outlined,
  'medicines': Icons.medication_outlined,
  'documents': Icons.description_outlined,
};

class BriefingScreen extends ConsumerWidget {
  const BriefingScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) => _page(
    'Daily Health Brief',
    AsyncBody<Json>(
      ref.watch(_briefingProvider),
      onRetry: () => ref.invalidate(_briefingProvider),
      builder: (b) {
        final sections = (b['sections'] as List? ?? []).cast<Json>();
        final pending = numOf(b['pending_approvals'])?.toInt() ?? 0;
        final filled = sections
            .where((s) => (s['items'] as List? ?? []).isNotEmpty)
            .toList();
        final clear = sections
            .where((s) => (s['items'] as List? ?? []).isEmpty)
            .map((s) => '${s['title']}'.toLowerCase())
            .toList();
        final attention = filled
            .expand((s) => (s['items'] as List).cast<Json>())
            .where((i) => i['tone'] == 'attention')
            .length;
        return ListView(
          padding: const EdgeInsets.all(14),
          children: [
            const _CopilotBrief(),
            SCard(
              child: Row(
                children: [
                  Icon(
                    attention == 0
                        ? Icons.check_circle_outline
                        : Icons.priority_high,
                    color: attention == 0 ? Pal.ok : Pal.warn,
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(
                      attention == 0
                          ? 'Nothing needs attention today.'
                          : '$attention ${attention == 1 ? 'item needs' : 'items need'} your attention today.',
                      style: const TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            if (pending > 0)
              SCard(
                color: Pal.priBg,
                onTap: () => _open(context, const ApprovalsScreen()),
                child: Row(
                  children: [
                    const Icon(
                      Icons.fact_check_outlined,
                      color: Pal.pri,
                      size: 18,
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        '$pending ${pending == 1 ? 'suggestion is' : 'suggestions are'} waiting for your approval',
                        style: const TextStyle(fontSize: 12, color: Pal.pri),
                      ),
                    ),
                    const Icon(Icons.chevron_right, color: Pal.pri),
                  ],
                ),
              ),
            for (final s in filled) ...[
              SectionTitle('${s['title']}'),
              for (final i in (s['items'] as List).cast<Json>())
                SCard(
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Icon(
                        _sectionIcons['${s['key']}'] ?? Icons.circle_outlined,
                        size: 16,
                        color: i['tone'] == 'attention' ? Pal.warn : Pal.mut,
                      ),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Text(
                          '${i['text']}',
                          style: const TextStyle(fontSize: 12.5, height: 1.35),
                        ),
                      ),
                    ],
                  ),
                ),
            ],
            if (clear.isNotEmpty) ...[
              const SectionTitle('All clear'),
              Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [
                  for (final c in clear) Pill(c, fg: Pal.ok, bg: Pal.okBg),
                ],
              ),
            ],
            const SizedBox(height: 8),
            OutlinedButton.icon(
              onPressed: () => _open(context, const AgentChatScreen()),
              icon: const Icon(Icons.auto_awesome, size: 16),
              label: const Text('Ask Sehat about today'),
            ),
          ],
        );
      },
    ),
  );
}

class RunAgentScreen extends ConsumerStatefulWidget {
  const RunAgentScreen({super.key});
  @override
  ConsumerState<RunAgentScreen> createState() => _RunState();
}

class _RunState extends ConsumerState<RunAgentScreen> {
  bool _busy = false;
  Json? _result;
  String? _error;
  bool _needsPermission = false;

  Future<void> _run() async {
    setState(() {
      _busy = true;
      _error = null;
      _needsPermission = false;
    });
    try {
      final r = await ref.read(apiProvider).post('${_base(ref)}/plan');
      if (!mounted) return;
      setState(() => _result = r is Map ? r.cast<String, dynamic>() : null);
      ref.invalidate(_actionsProvider);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = friendly(e);
        _needsPermission = e is ApiError && e.code == 'PERMISSION_REQUIRED';
      });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final steps = (_result?['steps'] as List? ?? []).cast<Json>();
    final proposed = numOf(_result?['proposed'])?.toInt() ?? 0;
    return _page(
      'Run agent',
      ListView(
        padding: const EdgeInsets.all(14),
        children: [
          const SCard(
            child: Text(
              'The agent reads your lab results, compares them with their reference ranges and suggests follow-ups. It never acts without your approval.',
              style: TextStyle(fontSize: 11, color: Pal.mut),
            ),
          ),
          FilledButton.icon(
            onPressed: _busy ? null : _run,
            icon: _busy
                ? const SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.auto_awesome),
            label: Text(_busy ? 'Working…' : 'Run now'),
          ),
          if (_error != null)
            SCard(
              color: Pal.badBg,
              child: Text(
                _error!,
                style: const TextStyle(color: Pal.bad, fontSize: 12),
              ),
            ),
          if (_needsPermission)
            TextButton(
              onPressed: () => _open(context, const PermissionsScreen()),
              child: const Text('Open permissions'),
            ),
          if (_result != null) ...[
            const SectionTitle('What it did'),
            for (final s in steps)
              SCard(
                child: Row(
                  children: [
                    const Icon(Icons.check_circle, size: 16, color: Pal.ok),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        '${s['label']}',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                    Text('${s['detail']}', style: _note),
                  ],
                ),
              ),
            if (proposed == 0)
              const Text('Nothing new to suggest.', style: _note)
            else
              FilledButton.tonal(
                onPressed: () => _open(context, const ApprovalsScreen()),
                child: Text('Review $proposed suggestion(s)'),
              ),
          ],
        ],
      ),
    );
  }
}

class ApprovalsScreen extends ConsumerWidget {
  const ApprovalsScreen({super.key});

  Future<void> _decide(
    BuildContext context,
    WidgetRef ref,
    Json a,
    String verb,
  ) async {
    try {
      await ref
          .read(apiProvider)
          .post('${_base(ref)}/actions/${a['id']}/$verb');
      ref.invalidate(_actionsProvider);
      ref.invalidate(_activityProvider);
      ref.invalidate(_briefingProvider);
      if (context.mounted) {
        toast(
          context,
          verb == 'approve' ? 'Added to your follow-ups' : 'Dismissed',
        );
      }
    } catch (e) {
      if (context.mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) => _page(
    'Approve actions',
    AsyncBody<List<Json>>(
      ref.watch(_actionsProvider),
      onRetry: () => ref.invalidate(_actionsProvider),
      builder: (list) {
        if (list.isEmpty) {
          return const Empty(
            Icons.task_alt,
            'Nothing to approve',
            body: 'Run the agent to get suggestions based on your results.',
          );
        }
        return ListView(
          padding: const EdgeInsets.all(14),
          children: [
            for (final a in list)
              SCard(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '${a['title']}',
                      style: const TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Text('${a['reason']}', style: _note),
                    const SizedBox(height: 8),
                    Row(
                      children: [
                        FilledButton(
                          onPressed: () => _decide(context, ref, a, 'approve'),
                          child: const Text('Approve'),
                        ),
                        const SizedBox(width: 8),
                        OutlinedButton(
                          onPressed: () => _decide(context, ref, a, 'reject'),
                          child: const Text('Dismiss'),
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

class PermissionsScreen extends ConsumerWidget {
  const PermissionsScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) => _page(
    'Agent permissions',
    AsyncBody<List<Json>>(
      ref.watch(_permissionsProvider),
      onRetry: () => ref.invalidate(_permissionsProvider),
      builder: (list) => ListView(
        padding: const EdgeInsets.all(14),
        children: [
          const Padding(
            padding: EdgeInsets.only(bottom: 8),
            child: Text(
              'Choose what the agent may do. You can change this any time.',
              style: _note,
            ),
          ),
          for (final p in list)
            SCard(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
              child: SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(
                  '${p['label']}',
                  style: const TextStyle(fontSize: 12.5),
                ),
                value: p['granted'] == true,
                onChanged: (v) async {
                  try {
                    await ref.read(apiProvider).put(
                      '${_base(ref)}/permissions/${p['permission']}',
                      {'granted': v},
                    );
                    ref.invalidate(_permissionsProvider);
                    ref.invalidate(_briefingProvider);
                  } catch (e) {
                    if (context.mounted) toast(context, friendly(e));
                  }
                },
              ),
            ),
        ],
      ),
    ),
  );
}

class ActivityScreen extends ConsumerWidget {
  const ActivityScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) => _page(
    'Agent activity',
    AsyncBody<List<Json>>(
      ref.watch(_activityProvider),
      onRetry: () => ref.invalidate(_activityProvider),
      builder: (list) {
        if (list.isEmpty) {
          return const Empty(
            Icons.history,
            'No activity yet',
            body: 'Suggestions and your decisions will appear here.',
          );
        }
        return ListView(
          padding: const EdgeInsets.all(14),
          children: [
            for (final e in list)
              SCard(
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        '${e['title']}',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                    Pill('${e['event']}'.replaceAll('_', ' ')),
                  ],
                ),
              ),
          ],
        );
      },
    ),
  );
}

class VisitPrepScreen extends ConsumerWidget {
  const VisitPrepScreen({super.key});

  static String _lab(Json l) =>
      '${l['name']}: ${fmtNum(numOf(l['value']))} ${l['unit'] ?? ''} (${'${l['flag']}'.replaceAll('_', ' ')})';

  static String _change(Json l) {
    final prev = numOf(l['previous_value']);
    return prev == null
        ? '${l['name']}: ${fmtNum(numOf(l['value']))} ${l['unit'] ?? ''}'
        : '${l['name']}: ${fmtNum(prev)} → ${fmtNum(numOf(l['value']))} ${l['unit'] ?? ''}';
  }

  static List<(String, List<String>)> _sections(Json v) {
    List<String> strs(String k) => [
      for (final i in (v[k] as List? ?? []))
        i is Map ? '${i['name'] ?? i['title']}' : '$i',
    ];
    final labs = (v['labs_to_discuss'] as List? ?? []).cast<Json>();
    return [
      ('Key changes', [for (final l in labs) _change(l)]),
      ('Results to discuss', [for (final l in labs) _lab(l)]),
      ('Questions to ask', strs('questions')),
      ('Active symptoms', strs('active_symptoms')),
      ('Medications', strs('medications')),
      ('Conditions & allergies', strs('conditions')),
      ('Open follow-ups', strs('open_follow_ups')),
      ('Goals', strs('goals')),
    ];
  }

  Future<void> _export(BuildContext context, Json v) async {
    final doc = pw.Document();
    doc.addPage(
      pw.MultiPage(
        build: (_) => [
          pw.Text(
            'Visit summary',
            style: pw.TextStyle(fontSize: 22, fontWeight: pw.FontWeight.bold),
          ),
          pw.Text('Prepared from my own records with Sehat.'),
          for (final (title, items) in _sections(v))
            if (items.isNotEmpty) ...[
              pw.SizedBox(height: 12),
              pw.Text(
                title,
                style: pw.TextStyle(
                  fontSize: 14,
                  fontWeight: pw.FontWeight.bold,
                ),
              ),
              for (final i in items) pw.Bullet(text: i),
            ],
          pw.SizedBox(height: 16),
          pw.Text('${v['note'] ?? ''}', style: const pw.TextStyle(fontSize: 9)),
        ],
      ),
    );
    try {
      await Printing.sharePdf(
        bytes: await doc.save(),
        filename: 'visit-summary.pdf',
      );
    } catch (e) {
      if (context.mounted) toast(context, 'Could not export the PDF.');
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) => _page(
    'Visit summary',
    AsyncBody<Json>(
      ref.watch(_visitPrepProvider),
      onRetry: () => ref.invalidate(_visitPrepProvider),
      builder: (v) => ListView(
        padding: const EdgeInsets.all(14),
        children: [
          for (final (title, items) in _sections(v))
            if (items.isNotEmpty) ...[
              SectionTitle(title),
              for (final i in items)
                SCard(child: Text(i, style: const TextStyle(fontSize: 12))),
            ],
          FilledButton.icon(
            onPressed: () => _export(context, v),
            icon: const Icon(Icons.picture_as_pdf_outlined),
            label: const Text('Export PDF / share'),
          ),
          Padding(
            padding: const EdgeInsets.all(8),
            child: Text('${v['note'] ?? ''}', style: _note),
          ),
        ],
      ),
    ),
  );
}
