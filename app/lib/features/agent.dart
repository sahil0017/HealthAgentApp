import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';

class AnswerCard extends ConsumerStatefulWidget {
  const AnswerCard(this.a, {super.key, this.onSuggest});
  final Json a;
  final void Function(String)? onSuggest;
  @override
  ConsumerState<AnswerCard> createState() => _AnswerCardState();
}

class _AnswerCardState extends ConsumerState<AnswerCard> {
  String? _rating;

  @override
  void didUpdateWidget(AnswerCard old) {
    super.didUpdateWidget(old);
    if (old.a['id'] != widget.a['id']) _rating = null;
  }

  Future<void> _rate(String r) async {
    final a = widget.a;
    try {
      await ref.read(apiProvider).put(
        '/profiles/${ref.read(profileIdProvider)}/questions/${a['id']}/feedback',
        {'rating': r},
      );
      setState(() => _rating = r);
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final a = widget.a;
    final sources = (a['sources'] as List? ?? []).whereType<Map>().toList();
    final limits = (a['limitations'] as List? ?? []).map((e) => '$e').toList();
    final suggested = (a['suggested_questions'] as List? ?? [])
        .map((e) => '$e')
        .toList();
    final rating = _rating ?? a['feedback'] as String?;
    final conf = '${a['confidence'] ?? 'insufficient'}';
    return SCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (a['question'] != null)
            Text(
              '${a['question']}',
              style: const TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w600,
                color: Pal.pri,
              ),
            ),
          if (a['urgent'] == true)
            Container(
              margin: const EdgeInsets.only(top: 8),
              padding: const EdgeInsets.all(8),
              decoration: BoxDecoration(
                color: Pal.badBg,
                borderRadius: BorderRadius.circular(10),
              ),
              child: const Text(
                'This may need urgent medical attention. Contact a clinician or emergency services now.',
                style: TextStyle(fontSize: 12, color: Pal.bad),
              ),
            ),
          const SizedBox(height: 8),
          SelectableText(
            '${a['answer']}',
            style: const TextStyle(fontSize: 13, height: 1.4),
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              Pill(
                '$conf confidence',
                fg: conf == 'high' ? Pal.ok : Pal.warn,
                bg: conf == 'high' ? Pal.okBg : Pal.warnBg,
              ),
              if (a['uncertainty'] != null)
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.only(left: 8),
                    child: Text(
                      '${a['uncertainty']}',
                      style: const TextStyle(fontSize: 10, color: Pal.mut),
                    ),
                  ),
                ),
            ],
          ),
          for (final l in limits)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                '• $l',
                style: const TextStyle(fontSize: 10.5, color: Pal.mut),
              ),
            ),
          if (sources.isNotEmpty) ...[
            const SectionTitle('Sources'),
            for (final s in sources)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Pill('${s['type']}', fg: Pal.mut, bg: Pal.line),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        '${s['description']}${s['verification_status'] != null ? ' · ${s['verification_status']}' : ''}',
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                    ),
                  ],
                ),
              ),
          ],
          if (suggested.isNotEmpty && widget.onSuggest != null) ...[
            const SectionTitle('Ask next'),
            Wrap(
              spacing: 6,
              runSpacing: 6,
              children: [
                for (final s in suggested)
                  ActionChip(
                    label: Text(s, style: const TextStyle(fontSize: 11)),
                    onPressed: () => widget.onSuggest!(s),
                  ),
              ],
            ),
          ],
          if (a['id'] != null)
            Row(
              children: [
                IconButton(
                  visualDensity: VisualDensity.compact,
                  tooltip: 'Helpful',
                  onPressed: () => _rate('helpful'),
                  icon: Icon(
                    Icons.thumb_up_alt_outlined,
                    size: 18,
                    color: rating == 'helpful' ? Pal.pri : Pal.mut,
                  ),
                ),
                IconButton(
                  visualDensity: VisualDensity.compact,
                  tooltip: 'Not helpful',
                  onPressed: () => _rate('not_helpful'),
                  icon: Icon(
                    Icons.thumb_down_alt_outlined,
                    size: 18,
                    color: rating == 'not_helpful' ? Pal.bad : Pal.mut,
                  ),
                ),
              ],
            ),
        ],
      ),
    );
  }
}

/// Sends one question to the agent and shows the cited answer.
class AskAboutScreen extends ConsumerStatefulWidget {
  const AskAboutScreen(this.question, {super.key});
  final String question;
  @override
  ConsumerState<AskAboutScreen> createState() => _AskAboutState();
}

class _AskAboutState extends ConsumerState<AskAboutScreen> {
  Json? _answer;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _run();
  }

  Future<void> _run() async {
    setState(() => _error = null);
    try {
      final a = await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/questions',
        {'question': widget.question},
      );
      if (mounted) setState(() => _answer = (a as Map).cast<String, dynamic>());
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Ask the agent')),
    body: ListView(
      padding: const EdgeInsets.all(14),
      children: [
        Text(
          widget.question,
          style: const TextStyle(fontSize: 12, color: Pal.mut),
        ),
        const SizedBox(height: 10),
        if (_error != null)
          ErrorView(_error!, onRetry: _run)
        else if (_answer == null)
          const Center(child: CircularProgressIndicator())
        else
          AnswerCard(_answer!),
      ],
    ),
  );
}
