import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import '../core/hardware.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'agent.dart';
import 'places.dart';

final alertsProvider = FutureProvider.autoDispose.family<List<Json>, String>(
  (ref, country) => ref
      .read(apiProvider)
      .list(
        '/nearby/alerts?country=${Uri.encodeQueryComponent(country)}',
        key: 'alerts',
      ),
);

/// Words from the user's own record that point to a specialty: active conditions and the
/// categories of results that are out of range.
final careNeedsProvider = FutureProvider.autoDispose<List<String>>((ref) async {
  final terms = <String>{};
  try {
    final history = await ref.watch(profileListProvider('/history').future);
    for (final e in history) {
      if (e['kind'] == 'condition' && e['status'] == 'active') {
        terms.add('${e['title']}'.toLowerCase());
      }
    }
    final labs = await ref.watch(
      profileListProvider('/biomarkers/summary').future,
    );
    for (final b in labs) {
      final flag = '${(b['latest'] as Map?)?['flag'] ?? ''}';
      if (flag.isNotEmpty && outOfRange(flag) && b['category'] != null) {
        terms.add('${b['category']}'.toLowerCase());
      }
    }
  } catch (_) {}
  return terms.toList();
});

// Words that appear in hospital specialty tags and names for each need.
const _specialtyHints = <String, List<String>>{
  'lipid': ['cardio', 'heart'],
  'cardi': ['cardio', 'heart'],
  'hypertension': ['cardio', 'heart'],
  'blood pressure': ['cardio', 'heart'],
  'glucose': ['diabet', 'endocrin'],
  'diabet': ['diabet', 'endocrin'],
  'thyroid': ['endocrin', 'diabet'],
  'kidney': ['nephro', 'renal', 'kidney'],
  'renal': ['nephro', 'renal', 'kidney'],
  'liver': ['gastro', 'hepat', 'liver'],
  'asthma': ['pulmon', 'chest', 'respir'],
  'blood count': ['haemat', 'hemat', 'oncolog'],
  'anaemia': ['haemat', 'hemat'],
  'cancer': ['oncolog', 'cancer'],
  'bone': ['ortho'],
  'pregnan': ['maternity', 'gynae', 'gynec', 'obstet'],
};

/// Orders hospitals by fit with the user's records, emergency cover and distance, and says why.
List<Json> rankHospitals(List<Json> all, List<String> needs) {
  final scored = <(double, Json)>[];
  for (final h in all) {
    final text = '${h['speciality'] ?? ''} ${h['name']}'.toLowerCase();
    var score = -(numOf(h['distance_km']) ?? 0).toDouble() * 0.4;
    String? reason;
    for (final n in needs) {
      final hints = <String>{
        for (final e in _specialtyHints.entries)
          if (n.contains(e.key)) ...e.value,
        ...n.split(RegExp(r'\W+')).where((w) => w.length > 4),
      };
      if (hints.any(text.contains)) {
        score += 4;
        reason ??= 'Matches your ${n.length > 24 ? 'records' : n}';
      }
    }
    if (h['emergency'] == true) score += 2;
    if (text.contains('multi') || text.contains('super')) score += 1;
    scored.add((score, {...h, 'reason': ?reason}));
  }
  scored.sort((a, b) => b.$1.compareTo(a.$1));
  return [for (final s in scored) s.$2];
}

final hospitalsProvider = FutureProvider.autoDispose.family<List<Json>, Area>((
  ref,
  a,
) async {
  final all = await ref
      .read(apiProvider)
      .list(
        '/nearby/hospitals?lat=${a.lat}&lng=${a.lng}&radius_km=15',
        key: 'hospitals',
      );
  return rankHospitals(all, await ref.watch(careNeedsProvider.future));
});

const _small = TextStyle(fontSize: 10.5, color: Pal.mut);
const _bold = TextStyle(fontSize: 12, fontWeight: FontWeight.w600);

/// Outbreak alerts and hospitals for the user's coarse area, from the `nearby` API.
class NearbyResults extends ConsumerWidget {
  const NearbyResults(this.area, {super.key});
  final Area area;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final country = area.country ?? '';
    final alerts = ref.watch(alertsProvider(country));
    final hospitals = ref.watch(hospitalsProvider(area));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SectionTitle('Outbreak alerts'),
        _Section<List<Json>>(
          alerts,
          () => ref.invalidate(alertsProvider(country)),
          (l) => l.isEmpty
              ? const Text('No alerts right now.', style: _small)
              : Column(
                  children: [
                    for (final a in l.take(6))
                      SCard(
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(builder: (_) => AlertScreen(a)),
                        ),
                        child: Row(
                          children: [
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text('${a['title']}', style: _bold),
                                  Text(
                                    '${a['source']} · ${_day(a['published_at'])}',
                                    style: _small,
                                  ),
                                ],
                              ),
                            ),
                            if (a['in_country'] == true)
                              const Pill(
                                'Your country',
                                fg: Pal.warn,
                                bg: Pal.warnBg,
                              ),
                          ],
                        ),
                      ),
                    const Text(
                      'Source: WHO Disease Outbreak News (global).',
                      style: _small,
                    ),
                  ],
                ),
        ),
        const SectionTitle('Hospitals near you'),
        _Section<List<Json>>(
          hospitals,
          () => ref.invalidate(hospitalsProvider(area)),
          (l) => l.isEmpty
              ? const Text(
                  'No hospitals found within 15 km in OpenStreetMap.',
                  style: _small,
                )
              : Column(
                  children: [
                    for (final h in l.take(10))
                      SCard(
                        child: Row(
                          children: [
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text('${h['name']}', style: _bold),
                                  Text(
                                    [
                                      '${h['distance_km']} km',
                                      if ('${h['address'] ?? ''}'.isNotEmpty)
                                        '${h['address']}',
                                      if ('${h['phone'] ?? ''}'.isNotEmpty)
                                        '${h['phone']}',
                                    ].join(' · '),
                                    style: _small,
                                  ),
                                ],
                              ),
                            ),
                            if (h['emergency'] == true)
                              const Pill(
                                'Emergency',
                                fg: Pal.bad,
                                bg: Pal.badBg,
                              ),
                          ],
                        ),
                      ),
                    const Text(
                      'Source: OpenStreetMap contributors. Call ahead to confirm services.',
                      style: _small,
                    ),
                  ],
                ),
        ),
        SCard(
          onTap: () => Navigator.push(
            context,
            MaterialPageRoute<void>(builder: (_) => const AreaMapScreen()),
          ),
          child: const Row(
            children: [
              Icon(Icons.map_outlined, color: Pal.pri, size: 20),
              SizedBox(width: 12),
              Expanded(
                child: Text('Area map: hospitals and alerts around you'),
              ),
              Icon(Icons.chevron_right, color: Pal.mut),
            ],
          ),
        ),
        const SectionTitle('Doctors, clinics, pharmacies & labs'),
        SCard(
          onTap: () => Navigator.push(
            context,
            MaterialPageRoute<void>(builder: (_) => const DoctorsScreen()),
          ),
          child: const Row(
            children: [
              Expanded(
                child: Text(
                  'Find care near you. Listings come from OpenStreetMap and are not verified against a medical registry.',
                  style: TextStyle(fontSize: 11.5, color: Pal.mut, height: 1.4),
                ),
              ),
              Icon(Icons.chevron_right, color: Pal.mut),
            ],
          ),
        ),
      ],
    );
  }
}

String _day(Object? iso) {
  final s = '$iso';
  return s.length >= 10 ? s.substring(0, 10) : s;
}

class _Section<T> extends StatelessWidget {
  const _Section(this.value, this.retry, this.builder);
  final AsyncValue<T> value;
  final VoidCallback retry;
  final Widget Function(T) builder;
  @override
  Widget build(BuildContext context) => value.when(
    data: builder,
    loading: () => const Padding(
      padding: EdgeInsets.all(12),
      child: Center(
        child: SizedBox(
          width: 20,
          height: 20,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      ),
    ),
    error: (e, _) => Row(
      children: [
        Expanded(child: Text(friendly(e), style: _small)),
        TextButton(onPressed: retry, child: const Text('Retry')),
      ],
    ),
  );
}

class AlertScreen extends ConsumerWidget {
  const AlertScreen(this.a, {super.key});
  final Json a;
  @override
  Widget build(BuildContext context, WidgetRef ref) => Scaffold(
    appBar: AppBar(title: const Text('Outbreak alert')),
    body: ListView(
      padding: const EdgeInsets.all(14),
      children: [
        Text(
          '${a['title']}',
          style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
        ),
        Text('${a['source']} · ${_day(a['published_at'])}', style: _small),
        const SizedBox(height: 10),
        Text(
          '${a['summary']}',
          style: const TextStyle(fontSize: 12.5, height: 1.45),
        ),
        const SizedBox(height: 10),
        SCard(
          child: Row(
            children: [
              Expanded(child: SelectableText('${a['url']}', style: _small)),
              TextButton(
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: '${a['url']}'));
                  toast(context, 'Link copied');
                },
                child: const Text('Copy'),
              ),
            ],
          ),
        ),
        FilledButton.tonalIcon(
          onPressed: () => launchUrl(
            Uri.parse('${a['url']}'),
            mode: LaunchMode.externalApplication,
          ),
          icon: const Icon(Icons.open_in_new, size: 16),
          label: const Text('Read the official guidance'),
        ),
        OutlinedButton.icon(
          onPressed: () => Navigator.push(
            context,
            MaterialPageRoute<void>(
              builder: (_) => AskAboutScreen(
                'What does this outbreak notice mean for me: ${a['title']}?',
              ),
            ),
          ),
          icon: const Icon(Icons.auto_awesome, size: 16),
          label: const Text('Ask the agent about this'),
        ),
        const Padding(
          padding: EdgeInsets.only(top: 8),
          child: Text(
            'Public-health notices are general and not a diagnosis. Talk to a clinician if you have symptoms.',
            style: _small,
          ),
        ),
      ],
    ),
  );
}
