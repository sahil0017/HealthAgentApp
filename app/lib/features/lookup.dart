import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';

const _note = TextStyle(fontSize: 10.5, color: Pal.mut);

/// Looks up a medicine by name: RxNorm matches and the FDA prescribing text.
class MedicineScreen extends ConsumerStatefulWidget {
  const MedicineScreen({super.key, this.initialQuery});
  final String? initialQuery;
  @override
  ConsumerState<MedicineScreen> createState() => _MedicineState();
}

class _MedicineState extends ConsumerState<MedicineScreen> {
  final _c = TextEditingController();
  Json? _search;
  Json? _label;
  Object? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    final q = widget.initialQuery;
    if (q != null && q.isNotEmpty) {
      _c.text = q;
      WidgetsBinding.instance.addPostFrameCallback((_) => _go());
    }
  }

  Future<void> _go() async {
    final q = _c.text.trim();
    if (q.length < 2) return;
    setState(() {
      _busy = true;
      _error = null;
      _label = null;
    });
    final api = ref.read(apiProvider);
    final enc = Uri.encodeQueryComponent(q);
    try {
      final s = await api.map('/medications/search?q=$enc');
      Json? l;
      try {
        l = await api.map('/medications/label?name=$enc');
      } on ApiError catch (e) {
        if (e.status != 404) rethrow;
      }
      if (mounted) {
        setState(() {
          _search = s;
          _label = l;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final meds = ((_search?['medicines'] as List?) ?? []).cast<Json>();
    final sections = ((_label?['sections'] as List?) ?? [])
        .cast<Json>()
        .where((s) => '${s['text']}'.isNotEmpty)
        .toList();
    return Scaffold(
      appBar: AppBar(title: const Text('Drug Information')),
      body: ListView(
        padding: const EdgeInsets.all(14),
        children: [
          TextField(
            controller: _c,
            textInputAction: TextInputAction.search,
            onSubmitted: (_) => _go(),
            decoration: InputDecoration(
              hintText: 'Medicine name, e.g. metformin',
              suffixIcon: IconButton(
                icon: const Icon(Icons.search),
                onPressed: _busy ? null : _go,
              ),
            ),
          ),
          if (_busy)
            const Padding(
              padding: EdgeInsets.all(20),
              child: Skeleton(rows: 2),
            ),
          if (_error != null)
            SizedBox(height: 200, child: ErrorView(_error!, onRetry: _go)),
          if (_search?['did_you_mean'] != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                'Showing results for “${_search!['did_you_mean']}”.',
                style: _note,
              ),
            ),
          if (_search != null && !_busy && meds.isEmpty && _error == null)
            const Empty(Icons.medication_outlined, 'No match found'),
          for (final s in sections) ...[
            SectionTitle('${s['title']}'),
            SCard(
              color: s['title'] == 'Boxed warning' ? Pal.warnBg : null,
              child: Text(
                '${s['text']}',
                style: const TextStyle(fontSize: 11.5),
              ),
            ),
          ],
          if (meds.isNotEmpty) ...[
            const SectionTitle('Matching products'),
            for (final m in meds.take(12))
              SCard(
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        '${m['name']}',
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                    Text('${m['type']}', style: _note),
                  ],
                ),
              ),
          ],
          if (_search != null)
            Padding(
              padding: const EdgeInsets.all(8),
              child: Text('${_search!['coverage']}', style: _note),
            ),
        ],
      ),
    );
  }
}

/// First sentence of the official "What it is used for" text for a medicine, or null when no
/// label is published for it.
final medicineUseProvider = FutureProvider.family<String?, String>((
  ref,
  name,
) async {
  try {
    final l = await ref
        .read(apiProvider)
        .map('/medications/label?name=${Uri.encodeQueryComponent(name)}');
    for (final s in (l['sections'] as List? ?? []).cast<Json>()) {
      if (s['title'] == 'What it is used for') {
        var t = '${s['text']}'
            .replaceAll(
              RegExp(
                r'^\s*(INDICATIONS?( AND |\s*&\s*)USAGE:?)\s*',
                caseSensitive: false,
              ),
              '',
            )
            .replaceAll(RegExp(r'^\d+(\.\d+)?\s+'), '')
            .trim();
        final dot = t.indexOf(RegExp(r'\.\s'));
        if (dot > 20 && dot < 220) t = t.substring(0, dot + 1);
        if (t.length > 220) t = '${t.substring(0, 217)}…';
        return t.isEmpty ? null : t;
      }
    }
  } catch (_) {}
  return null;
});
