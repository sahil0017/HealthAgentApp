import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'agent_chat.dart';
import 'insights.dart';
import 'vault.dart';

/// Every verified result grouped under the report it came from.
final labReportsProvider = profileListProvider('/biomarkers/by-report');

/// Lab Results, one card per uploaded report: which lab, which date, what was outside range and what moved
/// since the same test in the previous report.
class LabReportsView extends ConsumerWidget {
  const LabReportsView({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final reports = ref.watch(labReportsProvider);
    return RefreshIndicator(
      onRefresh: () async => ref.refresh(labReportsProvider.future),
      child: AsyncBody<List<Json>>(
        reports,
        onRetry: () => ref.invalidate(labReportsProvider),
        builder: (list) {
          if (list.isEmpty) {
            return const Empty(
              Icons.description_outlined,
              'No reports with results yet',
              body: 'Upload a lab report in the Reports tab and confirm it. Each report then shows here with its results.',
            );
          }
          return ListView(
            padding: const EdgeInsets.fromLTRB(14, 6, 14, 16),
            children: [
              if (list.length > 1)
                const InsightPanel(
                  path: '/agent/insights/uploads',
                  title: 'Across your reports',
                  icon: Icons.compare_arrows,
                ),
              for (var i = 0; i < list.length; i++)
                _ReportCard(list[i], latest: i == 0),
            ],
          );
        },
      ),
    );
  }
}

/// Opens the assistant limited to one report.
void openReportChat(BuildContext context, String id, String title) {
  Navigator.of(context).push(
    MaterialPageRoute<void>(
      builder: (_) => AgentChatScreen(report: {'id': id, 'title': title}),
    ),
  );
}

class _ReportCard extends StatelessWidget {
  const _ReportCard(this.g, {required this.latest});
  final Json g;
  final bool latest;

  @override
  Widget build(BuildContext context) {
    final total = (g['total'] as num?)?.toInt() ?? 0;
    final out = (g['outside_range'] as num?)?.toInt() ?? 0;
    final inr = (g['in_range'] as num?)?.toInt() ?? 0;
    final none = (g['no_range'] as num?)?.toInt() ?? 0;
    final changed = (g['changed_since_previous'] as num?)?.toInt() ?? 0;
    final results = (g['results'] as List? ?? []).cast<Json>();
    final file = '${g['file_name'] ?? ''}';
    return SCard(
      padding: EdgeInsets.zero,
      child: Theme(
        data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
        child: ExpansionTile(
          initiallyExpanded: latest,
          tilePadding: const EdgeInsets.fromLTRB(14, 6, 10, 6),
          childrenPadding: const EdgeInsets.fromLTRB(14, 0, 14, 12),
          title: Row(
            children: [
              Expanded(
                child: Text(
                  '${g['title']}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
              if (latest) const Pill('Latest', fg: Pal.pri, bg: Pal.priBg),
            ],
          ),
          subtitle: Padding(
            padding: const EdgeInsets.only(top: 3),
            child: Text(
              [
                shortDate('${g['report_date']}'),
                '$total results',
                if (out > 0) '$out outside range',
              ].where((s) => s.isNotEmpty).join(' · '),
              style: TextStyle(
                fontSize: 11,
                color: out > 0 ? Pal.warn : Pal.mut,
              ),
            ),
          ),
          children: [
            ClipRRect(
              borderRadius: BorderRadius.circular(4),
              child: SizedBox(
                height: 7,
                child: Row(
                  children: [
                    if (inr > 0)
                      Expanded(
                        flex: inr,
                        child: Container(color: Pal.ok),
                      ),
                    if (out > 0) ...[
                      const SizedBox(width: 2),
                      Expanded(
                        flex: out,
                        child: Container(color: Pal.warn),
                      ),
                    ],
                    if (none > 0) ...[
                      const SizedBox(width: 2),
                      Expanded(
                        flex: none,
                        child: Container(color: Pal.line),
                      ),
                    ],
                  ],
                ),
              ),
            ),
            const SizedBox(height: 6),
            Text(
              [
                '$inr in range',
                '$out outside range',
                if (none > 0) '$none without a range',
                if (changed > 0) '$changed compared with your earlier report',
              ].join(' · '),
              style: const TextStyle(fontSize: 10.5, color: Pal.mut),
            ),
            const SizedBox(height: 8),
            for (final r in results) _ResultRow(r),
            const SizedBox(height: 6),
            Row(
              children: [
                if (file.isNotEmpty)
                  Expanded(
                    child: Text(
                      file,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 10, color: Pal.mut),
                    ),
                  )
                else
                  const Spacer(),
                TextButton.icon(
                  onPressed: () => openReportChat(
                    context,
                    '${g['document_id']}',
                    '${g['title']} · ${shortDate('${g['report_date']}')}',
                  ),
                  icon: const Icon(Icons.chat_bubble_outline, size: 16),
                  label: const Text('Ask Sehat'),
                ),
                TextButton.icon(
                  onPressed: () => Navigator.of(context).push(
                    MaterialPageRoute(
                      builder: (_) => DocumentScreen('${g['document_id']}'),
                    ),
                  ),
                  icon: const Icon(Icons.auto_awesome, size: 16),
                  label: const Text('Summary & report'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _ResultRow extends StatelessWidget {
  const _ResultRow(this.r);
  final Json r;

  @override
  Widget build(BuildContext context) {
    final change = numOf(r['change']);
    final out = outOfRange('${r['flag']}');
    final unit = '${r['unit'] ?? ''}';
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${r['display_name']}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontSize: 12.5),
                ),
                if (change != null && change != 0)
                  Text(
                    '${change > 0 ? '▲' : '▼'} ${fmtNum(change.abs())} since ${shortDate('${r['previous_observed_at']}')}',
                    style: const TextStyle(fontSize: 10, color: Pal.mut),
                  ),
              ],
            ),
          ),
          Text(
            '${fmtNum(numOf(r['value']))} $unit'.trim(),
            style: TextStyle(
              fontSize: 12.5,
              fontWeight: FontWeight.w700,
              color: out ? Pal.warn : Pal.ink,
            ),
          ),
          const SizedBox(width: 8),
          SizedBox(
            width: 86,
            child: Align(
              alignment: Alignment.centerRight,
              child: Pill.flag('${r['flag']}'),
            ),
          ),
        ],
      ),
    );
  }
}
