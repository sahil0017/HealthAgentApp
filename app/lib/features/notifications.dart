import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'health.dart';
import 'more.dart';
import 'plan.dart';
import 'prescriptions.dart';

/// Something the person should look at. Built from the overview, today's doses and the care plan,
/// so the Summary can stay calm and everything that needs action lives here.
class Notice {
  const Notice({
    required this.id,
    required this.urgent,
    required this.icon,
    required this.title,
    required this.detail,
    required this.open,
    this.actionLabel,
  });
  final String id;
  final bool urgent;
  final IconData icon;
  final String title, detail;
  final String? actionLabel;
  final void Function(BuildContext context, WidgetRef ref) open;
}

class _Dismissed extends Notifier<Set<String>> {
  @override
  Set<String> build() => {};
  void add(String id) => state = {...state, id};
  void clear() => state = {};
}

final _dismissedProvider = NotifierProvider<_Dismissed, Set<String>>(
  _Dismissed.new,
);

void _push(BuildContext c, Widget w) =>
    Navigator.push(c, MaterialPageRoute<void>(builder: (_) => w));

/// Closes the notification centre and shows a main tab, so the person lands on it directly.
void _tab(BuildContext c, WidgetRef r, int i) {
  Navigator.of(c).popUntil((route) => route.isFirst);
  r.read(shellTabProvider.notifier).go(i);
}

List<Json> _maps(Object? v) => (v as List? ?? [])
    .whereType<Map>()
    .map((e) => e.cast<String, dynamic>())
    .toList();

Notice _fromAttention(Json a) {
  final kind = '${a['kind']}';
  final urgent = a['severity'] == 'action';
  void Function(BuildContext, WidgetRef) open = switch (kind) {
    'consent' => (c, _) => _push(c, const PrivacyScreen()),
    'review' || 'unverified' || 'failed_document' => (c, r) => _tab(c, r, 1),
    'overdue_follow_up' => (c, _) => _push(c, const CarePlanScreen()),
    'goal_without_data' ||
    'goal_off_target' => (c, _) => _push(c, const GoalsScreen()),
    _ when a['ref_type'] == 'biomarker' && a['ref_id'] != null =>
      (c, _) => _push(
        c,
        BiomarkerScreen(
          '${a['ref_id']}',
          '${a['title']}'.split(RegExp(r' is | has ')).first,
        ),
      ),
    _ => (c, r) => _tab(c, r, 2),
  };
  return Notice(
    id: '$kind|${a['ref_id']}|${a['title']}',
    urgent: urgent,
    icon: switch (kind) {
      'overdue_follow_up' => Icons.event_busy_outlined,
      'out_of_range' => Icons.monitor_heart_outlined,
      'consent' => Icons.lock_outline,
      'review' || 'unverified' || 'failed_document' => Icons.folder_open,
      'investigation' || 'measurement_gap' => Icons.biotech_outlined,
      _ => Icons.flag_outlined,
    },
    title: '${a['title']}',
    detail: '${a['detail'] ?? ''}',
    open: open,
  );
}

/// All current notices, urgent first, minus the ones the person dismissed this session.
final noticesProvider = Provider<List<Notice>>((ref) {
  final ov = ref.watch(profileMapProvider('/overview')).asData?.value;
  final sched = ref.watch(doseScheduleProvider).asData?.value;
  final gone = ref.watch(_dismissedProvider);
  final out = <Notice>[];

  var due = 0;
  for (final s in _maps(sched?['slots'])) {
    final time = '${s['time']}'.split(':');
    final h = int.tryParse(time.first) ?? 0;
    final m = time.length > 1 ? int.tryParse(time[1]) ?? 0 : 0;
    final now = DateTime.now();
    if (now.isBefore(DateTime(now.year, now.month, now.day, h, m))) continue;
    due += _maps(s['items']).where((i) => i['taken'] != true).length;
  }
  if (due > 0) {
    out.add(
      Notice(
        id: 'doses|${DateTime.now().toIso8601String().substring(0, 10)}|$due',
        urgent: true,
        icon: Icons.medication_outlined,
        title: due == 1 ? '1 dose is due' : '$due doses are due',
        detail: 'Tick them off once taken.',
        actionLabel: 'Open medicines',
        open: (c, _) => _push(c, const PrescriptionsScreen()),
      ),
    );
  }

  for (final a in _maps(ov?['attention'])) {
    out.add(_fromAttention(a));
  }

  for (final s in _maps(ov?['suggested_follow_ups']).take(3)) {
    out.add(
      Notice(
        id: 'suggest|${s['title']}',
        urgent: false,
        icon: Icons.lightbulb_outline,
        title: '${s['title']}',
        detail: '${s['reason'] ?? ''}',
        actionLabel: 'Add to care plan',
        open: (c, r) async {
          try {
            await r.read(apiProvider).post(
              '/profiles/${r.read(profileIdProvider)}/follow-ups',
              {
                'kind':
                    const [
                      'investigation',
                      'appointment',
                      'medication',
                    ].contains(s['kind'])
                    ? s['kind']
                    : 'other',
                'title': s['title'],
              },
            );
            refreshCarePlan(r);
            if (c.mounted) toast(c, 'Added to your care plan');
          } catch (e) {
            if (c.mounted) toast(c, friendly(e));
          }
        },
      ),
    );
  }

  final seen = <String>{};
  final list = out.where((n) => !gone.contains(n.id) && seen.add(n.id)).toList()
    ..sort((a, b) => (b.urgent ? 1 : 0) - (a.urgent ? 1 : 0));
  return list;
});

/// Bell for the app bar with a count of what needs attention.
class NotificationBell extends ConsumerWidget {
  const NotificationBell({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final n = ref.watch(noticesProvider);
    final urgent = n.where((e) => e.urgent).length;
    return IconButton(
      tooltip: 'Notifications',
      onPressed: () => _push(context, const NotificationsScreen()),
      icon: Badge(
        isLabelVisible: n.isNotEmpty,
        label: Text('${n.length}'),
        backgroundColor: urgent > 0 ? Pal.warn : Pal.pri,
        child: const Icon(Icons.notifications_none),
      ),
    );
  }
}

/// A slim strip at the top of the Summary, only when something is urgent.
class UrgentStrip extends ConsumerWidget {
  const UrgentStrip({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final all = ref.watch(noticesProvider);
    final urgent = all.where((n) => n.urgent).toList();
    if (urgent.isEmpty) return const SizedBox.shrink();
    final first = urgent.first;
    return SCard(
      color: Pal.warnBg,
      padding: const EdgeInsets.fromLTRB(12, 8, 6, 8),
      onTap: () => _push(context, const NotificationsScreen()),
      child: Row(
        children: [
          Icon(first.icon, size: 18, color: Pal.warn),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              first.title,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(
                fontSize: 12.5,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
          if (all.length > 1)
            Text(
              '+${all.length - 1} more',
              style: const TextStyle(fontSize: 11, color: Pal.mut),
            ),
          const Icon(Icons.chevron_right, color: Pal.mut),
        ],
      ),
    );
  }
}

class NotificationsScreen extends ConsumerWidget {
  const NotificationsScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(noticesProvider);
    final urgent = list.where((n) => n.urgent).toList();
    final other = list.where((n) => !n.urgent).toList();
    Widget tile(Notice n) => SCard(
      color: n.urgent ? Pal.warnBg : null,
      onTap: () {
        if (n.actionLabel == null) Navigator.pop(context);
        n.open(context, ref);
      },
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(n.icon, size: 18, color: n.urgent ? Pal.warn : Pal.pri),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  n.title,
                  style: const TextStyle(
                    fontSize: 12.5,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                if (n.detail.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 2),
                    child: Text(
                      n.detail,
                      style: const TextStyle(
                        fontSize: 11,
                        color: Pal.mut,
                        height: 1.3,
                      ),
                    ),
                  ),
                if (n.actionLabel != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: Text(
                      n.actionLabel!,
                      style: const TextStyle(
                        fontSize: 11.5,
                        color: Pal.pri,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
              ],
            ),
          ),
          IconButton(
            tooltip: 'Dismiss',
            visualDensity: VisualDensity.compact,
            icon: const Icon(Icons.close, size: 16, color: Pal.mut),
            onPressed: () => ref.read(_dismissedProvider.notifier).add(n.id),
          ),
        ],
      ),
    );

    return Scaffold(
      appBar: AppBar(
        title: const Text('Notifications'),
        actions: [
          if (list.isNotEmpty)
            TextButton(
              onPressed: () {
                for (final n in list) {
                  ref.read(_dismissedProvider.notifier).add(n.id);
                }
              },
              child: const Text('Clear all'),
            ),
        ],
      ),
      body: list.isEmpty
          ? const Empty(
              Icons.notifications_none,
              'You are all caught up',
              body: 'Reminders and things to review will appear here.',
            )
          : ListView(
              padding: const EdgeInsets.all(14),
              children: [
                if (urgent.isNotEmpty) ...[
                  const SectionTitle('Needs attention'),
                  for (final n in urgent) tile(n),
                ],
                if (other.isNotEmpty) ...[
                  const SectionTitle('Suggestions'),
                  for (final n in other) tile(n),
                ],
              ],
            ),
    );
  }
}
