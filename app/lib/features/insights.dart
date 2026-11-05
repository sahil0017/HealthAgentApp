import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'agent_screens.dart' show PermissionsScreen;

Color insightColor(String tone) => switch (tone) {
  'good' => Pal.ok,
  'watch' => Pal.warn,
  'act' => Pal.bad,
  _ => Pal.pri,
};

IconData insightIcon(String tone) => switch (tone) {
  'good' => Icons.check_circle_outline,
  'watch' => Icons.visibility_outlined,
  'act' => Icons.priority_high,
  _ => Icons.info_outline,
};

/// A written reading of the person's own data (lab history, lifestyle, research questions).
/// The result is cached by the provider, so it opens instantly after the first load. When the AI
/// is switched off the same panel shows a summary built straight from the records.
class InsightPanel extends ConsumerWidget {
  const InsightPanel({
    super.key,
    required this.path,
    required this.title,
    this.icon = Icons.auto_awesome,
    this.onItem,
    this.itemLabel,
    this.maxItems = 8,
  });

  /// Path below /profiles/{id}, for example /agent/insights/lifestyle.
  final String path, title;
  final IconData icon;
  final int maxItems;

  /// Optional one-tap action on each item, such as "Follow" or "Add".
  final void Function(Json item)? onItem;
  final String? itemLabel;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final async = ref.watch(profileMapProvider(path));
    return async.when(
      loading: () => SCard(
        child: Row(
          children: [
            Icon(icon, color: Pal.pri, size: 18),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                '$title · reading your records…',
                style: const TextStyle(fontSize: 12, color: Pal.mut),
              ),
            ),
            const SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          ],
        ),
      ),
      error: (e, _) => SCard(
        onTap: () => ref.invalidate(profileMapProvider(path)),
        child: Row(
          children: [
            const Icon(Icons.refresh, size: 18, color: Pal.mut),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                '$title could not load. Tap to retry.',
                style: const TextStyle(fontSize: 12, color: Pal.mut),
              ),
            ),
          ],
        ),
      ),
      data: (d) {
        final items = (d['items'] as List? ?? [])
            .whereType<Map>()
            .map((e) => e.cast<String, dynamic>())
            .take(maxItems)
            .toList();
        final headline = '${d['headline'] ?? ''}';
        if (items.isEmpty && headline.isEmpty) return const SizedBox.shrink();
        return SCard(
          padding: const EdgeInsets.all(14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(icon, size: 16, color: Pal.pri),
                  const SizedBox(width: 6),
                  Text(
                    title,
                    style: const TextStyle(
                      fontSize: 12,
                      fontWeight: FontWeight.w700,
                      color: Pal.pri,
                    ),
                  ),
                  const Spacer(),
                  if (d['source'] == 'ai') const Pill('Sehat AI'),
                ],
              ),
              if (headline.isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(
                  headline,
                  style: const TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w700,
                    height: 1.3,
                  ),
                ),
              ],
              for (final it in items) _Row(it, onItem, itemLabel),
              if (d['ai_available'] == true) ...[
                const SizedBox(height: 10),
                InkWell(
                  onTap: () => Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => const PermissionsScreen(),
                    ),
                  ),
                  child: const Text(
                    'Turn on AI for a deeper reading → Agent permissions',
                    style: TextStyle(fontSize: 11, color: Pal.pri),
                  ),
                ),
              ],
            ],
          ),
        );
      },
    );
  }
}

class _Row extends StatelessWidget {
  const _Row(this.it, this.onItem, this.label);
  final Json it;
  final void Function(Json)? onItem;
  final String? label;
  @override
  Widget build(BuildContext context) {
    final c = insightColor('${it['tone']}');
    return Padding(
      padding: const EdgeInsets.only(top: 12),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 26,
            height: 26,
            decoration: BoxDecoration(
              color: c.withValues(alpha: .14),
              shape: BoxShape.circle,
            ),
            child: Icon(insightIcon('${it['tone']}'), size: 14, color: c),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${it['title']}',
                  style: const TextStyle(
                    fontSize: 12.5,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                if ('${it['detail'] ?? ''}'.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 2),
                    child: Text(
                      '${it['detail']}',
                      style: const TextStyle(
                        fontSize: 11.5,
                        color: Pal.mut,
                        height: 1.35,
                      ),
                    ),
                  ),
                if (onItem != null)
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(
                      style: TextButton.styleFrom(
                        padding: EdgeInsets.zero,
                        minimumSize: const Size(0, 28),
                        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      ),
                      onPressed: () => onItem!(it),
                      child: Text(
                        label ?? 'Use',
                        style: const TextStyle(fontSize: 11),
                      ),
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
