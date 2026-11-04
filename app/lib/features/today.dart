import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';

class TodayTab extends ConsumerWidget {
  const TodayTab({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final ov = ref.watch(profileMapProvider('/overview'));
    return RefreshIndicator(
      onRefresh: () async =>
          ref.refresh(profileMapProvider('/overview').future),
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

class _Body extends ConsumerWidget {
  const _Body(this.d);
  final Json d;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sum = (d['summary'] as Map?)?.cast<String, dynamic>() ?? {};
    final comp = (d['completeness'] as Map?)?.cast<String, dynamic>() ?? {};
    final score = (numOf(comp['score']) ?? 0).toDouble();
    final attention = _list(d['attention']);
    final goals = _list(d['goals']);
    final reminders = _list(d['reminders']);
    final changes = _list(d['recent_changes']);
    final missing = _list(comp['items'])
        .where((i) => i['done'] != true)
        .toList();

    return ListView(
      padding: const EdgeInsets.fromLTRB(14, 8, 14, 20),
      children: [
        SCard(
          child: Row(
            children: [
              Ring(value: score / 100, label: '${score.round()}%', size: 58),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'Profile completeness',
                      style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      missing.isEmpty
                          ? 'Everything is in place.'
                          : 'Next: ${missing.first['label']}',
                      style: const TextStyle(fontSize: 11, color: Pal.mut),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
        Row(
          children: [
            _Stat('Documents', sum['documents']),
            _Stat('Biomarkers', sum['biomarkers_tracked']),
            _Stat('Goals', sum['active_goals']),
            _Stat('Follow-ups', sum['open_follow_ups']),
          ],
        ),
        if (attention.isNotEmpty) ...[
          const SectionTitle('Needs attention'),
          for (final a in attention)
            SCard(
              color: a['severity'] == 'action' ? Pal.warnBg : null,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    a['severity'] == 'action'
                        ? Icons.priority_high_rounded
                        : Icons.info_outline,
                    size: 16,
                    color: a['severity'] == 'action' ? Pal.warn : Pal.pri,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${a['title']}',
                          style: const TextStyle(
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        Text(
                          '${a['detail']}',
                          style: const TextStyle(fontSize: 11, color: Pal.mut),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
        ],
        if (changes.isNotEmpty) ...[
          const SectionTitle('Recent changes'),
          for (final c in changes.take(5))
            SCard(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '${c['name']}',
                      style: const TextStyle(fontSize: 12),
                    ),
                  ),
                  Text(
                    '${fmtNum(numOf(c['value']))} ${c['unit']}',
                    style: const TextStyle(
                      fontSize: 12,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  if (numOf(c['change_percent']) != null) ...[
                    const SizedBox(width: 8),
                    Pill(
                      '${numOf(c['change_percent'])! > 0 ? '+' : ''}${fmtNum(numOf(c['change_percent']))}%',
                      fg: Pal.mut,
                      bg: Pal.line,
                    ),
                  ],
                ],
              ),
            ),
        ],
        if (goals.isNotEmpty) ...[
          const SectionTitle('Goals'),
          for (final g in goals)
            SCard(
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${g['title']}',
                          style: const TextStyle(
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        Text(
                          'Latest ${fmtNum(numOf((g['progress'] as Map?)?['latest_value']))} ${g['unit'] ?? ''}',
                          style: const TextStyle(fontSize: 11, color: Pal.mut),
                        ),
                      ],
                    ),
                  ),
                  Pill(
                    '${(g['progress'] as Map?)?['status'] ?? ''}'.replaceAll(
                      '_',
                      ' ',
                    ),
                  ),
                ],
              ),
            ),
        ],
        SectionTitle(
          'Follow-ups',
          trailing: TextButton(
            onPressed: () => _addFollowUp(context, ref),
            child: const Text('Add'),
          ),
        ),
        if (reminders.isEmpty)
          const Padding(
            padding: EdgeInsets.all(8),
            child: Text(
              'No open follow-ups.',
              style: TextStyle(fontSize: 11, color: Pal.mut),
            ),
          ),
        for (final f in reminders)
          SCard(
            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
            child: CheckboxListTile(
              dense: true,
              contentPadding: const EdgeInsets.symmetric(horizontal: 6),
              controlAffinity: ListTileControlAffinity.leading,
              value: f['status'] == 'done',
              title: Text(
                '${f['title']}',
                style: const TextStyle(fontSize: 12),
              ),
              subtitle: f['due_on'] == null
                  ? null
                  : Text(
                      'Due ${f['due_on']}',
                      style: const TextStyle(fontSize: 10),
                    ),
              onChanged: (_) async {
                await ref.read(apiProvider).patch('/follow-ups/${f['id']}', {
                  'status': 'done',
                });
                ref.invalidate(profileMapProvider('/overview'));
              },
            ),
          ),
      ],
    );
  }

  Future<void> _addFollowUp(BuildContext context, WidgetRef ref) async {
    final c = TextEditingController();
    final title = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('New follow-up', style: TextStyle(fontSize: 15)),
        content: TextField(
          controller: c,
          autofocus: true,
          decoration: const InputDecoration(
            hintText: 'e.g. Repeat lipid panel',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, c.text.trim()),
            child: const Text('Add'),
          ),
        ],
      ),
    );
    if (title == null || title.isEmpty) return;
    final id = ref.read(profileIdProvider);
    try {
      await ref.read(apiProvider).post('/profiles/$id/follow-ups', {
        'kind': 'other',
        'title': title,
      });
      ref.invalidate(profileMapProvider('/overview'));
    } catch (e) {
      if (context.mounted) toast(context, friendly(e));
    }
  }
}

class _Stat extends StatelessWidget {
  const _Stat(this.label, this.value);
  final String label;
  final Object? value;
  @override
  Widget build(BuildContext context) => Expanded(
    child: Container(
      margin: const EdgeInsets.only(right: 6),
      padding: const EdgeInsets.symmetric(vertical: 8),
      decoration: BoxDecoration(
        color: Pal.card,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        children: [
          Text(
            '${value ?? 0}',
            style: const TextStyle(
              fontSize: 16,
              fontWeight: FontWeight.w700,
              color: Pal.pri,
            ),
          ),
          Text(label, style: const TextStyle(fontSize: 9.5, color: Pal.mut)),
        ],
      ),
    ),
  );
}
