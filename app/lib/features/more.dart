import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/hardware.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'nearby.dart';
import 'records.dart';

class MoreTab extends ConsumerWidget {
  const MoreTab({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    void open(Widget w) =>
        Navigator.push(context, MaterialPageRoute(builder: (_) => w));
    final items = <(IconData, String, String, Widget)>[
      (
        Icons.place_outlined,
        'Nearby care',
        'Your area, doctors, alerts',
        const NearbyScreen(),
      ),
      (
        Icons.self_improvement,
        'Lifestyle & symptoms',
        'Sleep, activity, how you feel',
        const LifestyleScreen(),
      ),
      (
        Icons.timeline,
        'Timeline',
        'Everything in order',
        const TimelineScreen(),
      ),
      (
        Icons.flag_outlined,
        'Goals',
        'Targets for your biomarkers',
        const GoalsScreen(),
      ),
      (
        Icons.travel_explore,
        'Investigations',
        'Research you are tracking',
        const InvestigationsScreen(),
      ),
      (
        Icons.medical_information_outlined,
        'Medical history',
        'Conditions, allergies, medications',
        const HistoryScreen(),
      ),
      (
        Icons.group_outlined,
        'Family sharing',
        'Share read-only access',
        const FamilyScreen(),
      ),
      (
        Icons.hub_outlined,
        'Connect AI tools',
        'MCP access token',
        const McpScreen(),
      ),
      (
        Icons.storage_outlined,
        'Data & retention',
        'Retention limits, export',
        const DataControlScreen(),
      ),
      (
        Icons.shield_outlined,
        'Privacy & data',
        'Consents, devices, delete',
        const PrivacyScreen(),
      ),
    ];
    return ListView(
      padding: const EdgeInsets.fromLTRB(14, 8, 14, 16),
      children: [
        for (final i in items)
          SCard(
            onTap: () => open(i.$4),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            child: Row(
              children: [
                Icon(i.$1, color: Pal.pri, size: 20),
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
                        style: const TextStyle(fontSize: 10.5, color: Pal.mut),
                      ),
                    ],
                  ),
                ),
                const Icon(Icons.chevron_right, size: 18, color: Pal.mut),
              ],
            ),
          ),
        const SizedBox(height: 6),
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

// ---------------------------------------------------------------- Nearby

class NearbyScreen extends ConsumerStatefulWidget {
  const NearbyScreen({super.key});
  @override
  ConsumerState<NearbyScreen> createState() => _NearbyState();
}

class _NearbyState extends ConsumerState<NearbyScreen> {
  bool _busy = false;
  String? _msg;

  Future<void> _detect() async {
    setState(() {
      _busy = true;
      _msg = null;
    });
    try {
      final ok = await ref.read(areaProvider.notifier).detect();
      if (!ok) _msg = 'Location is off or permission was denied. You can enable it in Settings.';
    } catch (_) {
      _msg = 'Could not read your location.';
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final Area? area = ref.watch(areaProvider);
    return _Page(
      'Nearby care',
      ListView(
        padding: const EdgeInsets.all(14),
        children: [
          SCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    const Icon(Icons.my_location, color: Pal.pri, size: 18),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        area == null
                            ? 'Location is off'
                            : (area.label.isEmpty
                                  ? 'Area detected'
                                  : area.label),
                        style: const TextStyle(
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    if (area?.countryCode != null) Pill(area!.countryCode!),
                  ],
                ),
                const SizedBox(height: 6),
                const Text(
                  'Only an approximate area (about 5 km) is used. Your exact position never leaves your phone.',
                  style: TextStyle(fontSize: 11, color: Pal.mut),
                ),
                if (_msg != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: Text(
                      _msg!,
                      style: const TextStyle(fontSize: 11, color: Pal.warn),
                    ),
                  ),
                const SizedBox(height: 10),
                FilledButton(
                  onPressed: _busy ? null : _detect,
                  child: Text(
                    _busy
                        ? 'Locating…'
                        : (area == null
                              ? 'Use my location'
                              : 'Refresh location'),
                  ),
                ),
              ],
            ),
          ),
          if (area != null) NearbyResults(area),
          if (area == null)
            const SCard(
              child: Text(
                'Turn on location to see outbreak alerts and hospitals near you.',
                style: TextStyle(fontSize: 11.5, color: Pal.mut),
              ),
            ),
          const SCard(
            color: Pal.badBg,
            child: Text(
              'In an emergency call your local emergency number now.',
              style: TextStyle(
                fontSize: 12,
                color: Pal.bad,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------- Lifestyle

class LifestyleScreen extends ConsumerStatefulWidget {
  const LifestyleScreen({super.key});
  @override
  ConsumerState<LifestyleScreen> createState() => _LifestyleState();
}

class _LifestyleState extends ConsumerState<LifestyleScreen> {
  Future<void> _logLifestyle(List<Json> cats) async {
    var cat = cats.first['code'] as String;
    final v = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, set) => AlertDialog(
          title: const Text('Log', style: TextStyle(fontSize: 15)),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              DropdownButton<String>(
                isExpanded: true,
                value: cat,
                items: [
                  for (final c in cats)
                    DropdownMenuItem(
                      value: c['code'] as String,
                      child: Text('${c['name']} (${c['unit']})'),
                    ),
                ],
                onChanged: (x) => set(() => cat = x!),
              ),
              TextField(
                controller: v,
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                ),
                decoration: const InputDecoration(labelText: 'Value'),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Save'),
            ),
          ],
        ),
      ),
    );
    final n = num.tryParse(v.text);
    if (ok != true || n == null) return;
    try {
      await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/lifestyle-logs',
        {'category': cat, 'value': n},
      );
      ref.invalidate(profileMapProvider('/lifestyle/summary?days=30'));
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  Future<void> _logSymptom() async {
    final name = TextEditingController();
    double sev = 3;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, set) => AlertDialog(
          title: const Text('Log a symptom', style: TextStyle(fontSize: 15)),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: name,
                decoration: const InputDecoration(
                  labelText: 'What are you feeling?',
                ),
              ),
              Slider(
                value: sev,
                min: 1,
                max: 10,
                divisions: 9,
                label: '${sev.round()}',
                onChanged: (x) => set(() => sev = x),
              ),
              Text(
                'Strength ${sev.round()} of 10',
                style: const TextStyle(fontSize: 11),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Save'),
            ),
          ],
        ),
      ),
    );
    if (ok != true || name.text.trim().isEmpty) return;
    try {
      await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/symptoms',
        {'name': name.text.trim(), 'severity': sev.round()},
      );
      ref.invalidate(profileListProvider('/symptoms'));
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final sum = ref.watch(profileMapProvider('/lifestyle/summary?days=30'));
    final symptoms = ref.watch(profileListProvider('/symptoms'));
    final cats = ref.watch(_categories).value ?? [];
    return Scaffold(
      appBar: AppBar(title: const Text('Lifestyle & symptoms')),
      body: ListView(
        padding: const EdgeInsets.all(14),
        children: [
          Row(
            children: [
              Expanded(
                child: FilledButton(
                  onPressed: cats.isEmpty ? null : () => _logLifestyle(cats),
                  child: const Text('Log activity'),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: OutlinedButton(
                  onPressed: _logSymptom,
                  child: const Text('Log symptom'),
                ),
              ),
            ],
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
              if (list.isEmpty)
                return const Padding(
                  padding: EdgeInsets.all(8),
                  child: Text(
                    'Nothing logged yet.',
                    style: TextStyle(fontSize: 11, color: Pal.mut),
                  ),
                );
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
                                  '${c['name']}',
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
                            width: 80,
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
          const SectionTitle('Symptoms'),
          AsyncBody<List<Json>>(
            symptoms,
            onRetry: () => ref.invalidate(profileListProvider('/symptoms')),
            builder: (list) {
              if (list.isEmpty)
                return const Padding(
                  padding: EdgeInsets.all(8),
                  child: Text(
                    'No symptoms logged.',
                    style: TextStyle(fontSize: 11, color: Pal.mut),
                  ),
                );
              return Column(
                children: [
                  for (final s in list)
                    SCard(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 12,
                        vertical: 8,
                      ),
                      child: Row(
                        children: [
                          Expanded(
                            child: Text(
                              '${s['name']}  ·  ${s['severity']}/10',
                              style: const TextStyle(fontSize: 12),
                            ),
                          ),
                          if (s['active'] == true)
                            TextButton(
                              onPressed: () async {
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
                              },
                              child: const Text('Resolved'),
                            )
                          else
                            const Pill('Ended', fg: Pal.mut, bg: Pal.line),
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

final _categories = FutureProvider<List<Json>>(
  (ref) => ref.read(apiProvider).list('/lifestyle-categories'),
);

// ---------------------------------------------------------------- Timeline / Goals / Investigations

class TimelineScreen extends ConsumerWidget {
  const TimelineScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final t = ref.watch(profileListProvider('/timeline'));
    return _Page(
      'Timeline',
      AsyncBody<List<Json>>(
        t,
        onRetry: () => ref.invalidate(profileListProvider('/timeline')),
        builder: (list) {
          if (list.isEmpty)
            return const Empty(Icons.timeline, 'Nothing here yet');
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [
              for (final i in list)
                SCard(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Pill('${i['kind']}'.replaceAll('_', ' ')),
                          const SizedBox(width: 8),
                          Text(
                            '${i['occurred_at']}'.split('T').first,
                            style: const TextStyle(
                              fontSize: 10,
                              color: Pal.mut,
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 4),
                      Text(
                        '${i['title']}',
                        style: const TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      if ('${i['detail'] ?? ''}'.isNotEmpty)
                        Text(
                          '${i['detail']}',
                          style: const TextStyle(fontSize: 11, color: Pal.mut),
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

class GoalsScreen extends ConsumerWidget {
  const GoalsScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final g = ref.watch(profileListProvider('/goals'));
    return _Page(
      'Goals',
      AsyncBody<List<Json>>(
        g,
        onRetry: () => ref.invalidate(profileListProvider('/goals')),
        builder: (list) {
          if (list.isEmpty)
            return const Empty(
              Icons.flag_outlined,
              'No goals yet',
              body: 'Goals set from your records will show here.',
            );
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
                            Text(
                              '${x['title']}',
                              style: const TextStyle(
                                fontSize: 12,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                            Text(
                              'Target ${fmtNum(numOf(x['target_min']))}–${fmtNum(numOf(x['target_max']))} ${x['unit'] ?? ''}',
                              style: const TextStyle(
                                fontSize: 10.5,
                                color: Pal.mut,
                              ),
                            ),
                          ],
                        ),
                      ),
                      Pill('${x['status']}'),
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

class InvestigationsScreen extends ConsumerWidget {
  const InvestigationsScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final g = ref.watch(profileListProvider('/investigations'));
    return _Page(
      'Investigations',
      AsyncBody<List<Json>>(
        g,
        onRetry: () => ref.invalidate(profileListProvider('/investigations')),
        builder: (list) {
          if (list.isEmpty)
            return const Empty(
              Icons.travel_explore,
              'No investigations',
              body: 'Track a health question and its sources over time.',
            );
          return ListView(
            padding: const EdgeInsets.all(14),
            children: [
              for (final x in list)
                SCard(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '${x['title']}',
                        style: const TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      Text(
                        '${x['question']}',
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        '${x['source_count']} sources · ${x['note_count']} notes',
                        style: const TextStyle(fontSize: 10, color: Pal.mut),
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
      'Privacy & data',
      ListView(
        padding: const EdgeInsets.all(14),
        children: [
          const SectionTitle('Consents'),
          AsyncBody<List<Json>>(
            consents,
            onRetry: () => ref.invalidate(profileListProvider('/consents')),
            builder: (list) {
              final on = {
                for (final c in list) '${c['purpose']}': c['granted'] == true,
              };
              return Column(
                children: [
                  for (final e in _consentLabels.entries)
                    SCard(
                      padding: const EdgeInsets.symmetric(horizontal: 12),
                      child: SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        dense: true,
                        title: Text(
                          e.value,
                          style: const TextStyle(fontSize: 12.5),
                        ),
                        value: on[e.key] ?? false,
                        onChanged: (v) => _toggle(e.key, v),
                      ),
                    ),
                ],
              );
            },
          ),
          SectionTitle(
            'Signed-in devices',
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
            builder: (list) => Column(
              children: [
                for (final s in list)
                  SCard(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 8,
                    ),
                    child: Row(
                      children: [
                        Expanded(
                          child: Text(
                            '${s['device']}${s['current'] == true ? ' · this device' : ''}',
                            style: const TextStyle(fontSize: 12),
                          ),
                        ),
                        if (s['current'] != true)
                          TextButton(
                            onPressed: () async {
                              await ref
                                  .read(apiProvider)
                                  .delete('/auth/sessions/${s['id']}');
                              ref.invalidate(_sessions);
                            },
                            child: const Text('Revoke'),
                          ),
                      ],
                    ),
                  ),
              ],
            ),
          ),
          const SectionTitle('Recent access'),
          AsyncBody<List<Json>>(
            access,
            builder: (list) => Column(
              children: [
                for (final a in list.take(10))
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 3),
                    child: Row(
                      children: [
                        Expanded(
                          child: Text(
                            '${a['action']} · ${a['resource_type']}',
                            style: const TextStyle(
                              fontSize: 11,
                              color: Pal.mut,
                            ),
                          ),
                        ),
                        Text(
                          '${a['created_at']}'.split('T').first,
                          style: const TextStyle(fontSize: 10, color: Pal.mut),
                        ),
                      ],
                    ),
                  ),
              ],
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
