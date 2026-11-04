import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:http/http.dart' as http;
import 'package:http_parser/http_parser.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';

const _status = {
  'queued': 'Reading…',
  'processing': 'Reading…',
  'extracting': 'Reading…',
  'review_required': 'Ready to review',
  'manual_review_required': 'Needs a careful look',
  'approved': 'Confirmed',
  'failed': "Couldn't read",
};

class VaultTab extends ConsumerStatefulWidget {
  const VaultTab({super.key});
  @override
  ConsumerState<VaultTab> createState() => _VaultState();
}

class _VaultState extends ConsumerState<VaultTab> {
  final _q = TextEditingController();
  List<Json>? _hits;
  bool _busy = false;

  @override
  void dispose() {
    _q.dispose();
    super.dispose();
  }

  Future<void> _upload(List<int> bytes, String name, String mime) async {
    final id = ref.read(profileIdProvider);
    setState(() => _busy = true);
    try {
      final parts = mime.split('/');
      await ref
          .read(apiProvider)
          .request(
            'POST',
            '/profiles/$id/documents',
            files: [
              http.MultipartFile.fromBytes(
                'file',
                bytes,
                filename: name,
                contentType: MediaType(parts[0], parts[1]),
              ),
            ],
          );
      ref.invalidate(profileListProvider('/documents'));
      if (mounted) toast(context, 'Uploaded. Reading your document…');
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _pick() async {
    final r = await FilePicker.pickFiles(
      type: FileType.custom,
      allowedExtensions: ['pdf', 'png', 'jpg', 'jpeg'],
    );
    if (r.isEmpty) return;
    final f = r.first;
    final ext = f.extension?.toLowerCase();
    await _upload(
      await f.readAsBytes(),
      f.name,
      ext == 'pdf'
          ? 'application/pdf'
          : (ext == 'png' ? 'image/png' : 'image/jpeg'),
    );
  }

  Future<void> _scan() async {
    final s = await ref.read(scannerProvider).scan();
    if (s != null) await _upload(s.bytes, s.name, 'application/pdf');
  }

  Future<void> _search(String q) async {
    if (q.trim().length < 2) {
      setState(() => _hits = null);
      return;
    }
    final id = ref.read(profileIdProvider);
    try {
      final h = await ref
          .read(apiProvider)
          .list(
            '/profiles/$id/documents/search?q=${Uri.encodeQueryComponent(q.trim())}',
          );
      if (mounted) setState(() => _hits = h);
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    final docs = ref.watch(profileListProvider('/documents'));
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(14, 8, 14, 4),
          child: TextField(
            controller: _q,
            textInputAction: TextInputAction.search,
            onSubmitted: _search,
            decoration: InputDecoration(
              isDense: true,
              prefixIcon: const Icon(Icons.search, size: 18),
              hintText: 'Search inside your documents',
              suffixIcon: _q.text.isEmpty
                  ? null
                  : IconButton(
                      icon: const Icon(Icons.close, size: 16),
                      onPressed: () => setState(() {
                        _q.clear();
                        _hits = null;
                      }),
                    ),
            ),
          ),
        ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 4),
          child: Row(
            children: [
              Expanded(
                child: FilledButton.icon(
                  onPressed: _busy ? null : _scan,
                  icon: const Icon(Icons.document_scanner_outlined, size: 18),
                  label: const Text('Scan'),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: _busy ? null : _pick,
                  icon: const Icon(Icons.upload_file, size: 18),
                  label: const Text('Upload'),
                ),
              ),
            ],
          ),
        ),
        if (_busy) const LinearProgressIndicator(minHeight: 2),
        Expanded(
          child: _hits != null
              ? (_hits!.isEmpty
                    ? const Empty(Icons.search_off, 'No matches')
                    : ListView(
                        padding: const EdgeInsets.all(14),
                        children: [
                          for (final h in _hits!)
                            SCard(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    '${h['file_name']}  ·  p.${h['page']}',
                                    style: const TextStyle(
                                      fontSize: 12,
                                      fontWeight: FontWeight.w600,
                                    ),
                                  ),
                                  const SizedBox(height: 3),
                                  Text(
                                    '${h['snippet']}',
                                    style: const TextStyle(
                                      fontSize: 11,
                                      color: Pal.mut,
                                    ),
                                  ),
                                ],
                              ),
                            ),
                        ],
                      ))
              : RefreshIndicator(
                  onRefresh: () async =>
                      ref.refresh(profileListProvider('/documents').future),
                  child: AsyncBody<List<Json>>(
                    docs,
                    onRetry: () =>
                        ref.invalidate(profileListProvider('/documents')),
                    builder: (list) {
                      if (list.isEmpty) {
                        return const Empty(
                          Icons.folder_open,
                          'Your vault is empty',
                          body: 'Scan or upload a lab report to get started.',
                        );
                      }
                      return ListView(
                        padding: const EdgeInsets.fromLTRB(14, 4, 14, 16),
                        children: [for (final d in list) _DocTile(d)],
                      );
                    },
                  ),
                ),
        ),
      ],
    );
  }
}

class _DocTile extends ConsumerWidget {
  const _DocTile(this.d);
  final Json d;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = '${d['status']}';
    final ready = s == 'review_required' || s == 'manual_review_required';
    return SCard(
      onTap: ready || s == 'approved'
          ? () => Navigator.push(
              context,
              MaterialPageRoute(builder: (_) => DocumentScreen('${d['id']}')),
            )
          : null,
      child: Row(
        children: [
          const Icon(Icons.description_outlined, size: 20, color: Pal.pri),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${d['file_name']}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                Text(
                  '${d['document_type'] ?? ''}'.replaceAll('_', ' '),
                  style: const TextStyle(fontSize: 10, color: Pal.mut),
                ),
              ],
            ),
          ),
          if (s == 'failed')
            TextButton(
              onPressed: () async {
                await ref
                    .read(apiProvider)
                    .post(
                      '/profiles/${ref.read(profileIdProvider)}/documents/${d['id']}/retry',
                    );
                ref.invalidate(profileListProvider('/documents'));
              },
              child: const Text('Retry'),
            )
          else
            Pill(
              _status[s] ?? s,
              fg: ready ? Pal.warn : (s == 'approved' ? Pal.ok : Pal.mut),
              bg: ready ? Pal.warnBg : (s == 'approved' ? Pal.okBg : Pal.line),
            ),
        ],
      ),
    );
  }
}

class DocumentScreen extends ConsumerStatefulWidget {
  const DocumentScreen(this.id, {super.key});
  final String id;
  @override
  ConsumerState<DocumentScreen> createState() => _DocumentState();
}

class _DocumentState extends ConsumerState<DocumentScreen> {
  Json? _doc;
  List<Json> _rows = [];
  Object? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final d = await ref
          .read(apiProvider)
          .map(
            '/profiles/${ref.read(profileIdProvider)}/documents/${widget.id}',
          );
      final rows = ((d['extraction'] as Map?)?['biomarkers'] as List? ?? [])
          .whereType<Map>()
          .map((e) => e.cast<String, dynamic>())
          .toList();
      if (mounted) {
        setState(() {
          _doc = d;
          _rows = rows;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _approve() async {
    setState(() => _busy = true);
    try {
      await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/documents/${widget.id}/approve',
        {'biomarkers': _rows},
      );
      ref.invalidate(profileListProvider('/documents'));
      ref.invalidate(profileListProvider('/biomarkers/summary'));
      ref.invalidate(profileMapProvider('/overview'));
      if (mounted) Navigator.pop(context);
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _edit(int i) async {
    final c = TextEditingController(text: fmtNum(numOf(_rows[i]['value'])));
    final v = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(
          '${_rows[i]['name']}',
          style: const TextStyle(fontSize: 14),
        ),
        content: TextField(
          controller: c,
          autofocus: true,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          decoration: InputDecoration(suffixText: '${_rows[i]['unit']}'),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, c.text),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    final n = v == null ? null : num.tryParse(v);
    if (n != null) setState(() => _rows[i] = {..._rows[i], 'value': n});
  }

  @override
  Widget build(BuildContext context) {
    final approved = _doc?['status'] == 'approved';
    return Scaffold(
      appBar: AppBar(
        title: Text(
          '${_doc?['file_name'] ?? 'Document'}',
          style: const TextStyle(fontSize: 14),
        ),
      ),
      body: _error != null
          ? ErrorView(_error!, onRetry: _load)
          : _doc == null
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.all(14),
              children: [
                if (!approved)
                  const Text(
                    'Check each value against your report. Tap a value to correct it.',
                    style: TextStyle(fontSize: 11, color: Pal.mut),
                  ),
                const SizedBox(height: 8),
                if (_rows.isEmpty)
                  const Empty(
                    Icons.search_off,
                    'No values found',
                    body: 'Nothing was extracted from this document.',
                  ),
                for (var i = 0; i < _rows.length; i++)
                  SCard(
                    onTap: approved ? null : () => _edit(i),
                    child: Row(
                      children: [
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                '${_rows[i]['name']}',
                                style: const TextStyle(
                                  fontSize: 12,
                                  fontWeight: FontWeight.w600,
                                ),
                              ),
                              if ('${_rows[i]['reference_range'] ?? ''}'
                                  .isNotEmpty)
                                Text(
                                  'Range ${_rows[i]['reference_range']}',
                                  style: const TextStyle(
                                    fontSize: 10,
                                    color: Pal.mut,
                                  ),
                                ),
                            ],
                          ),
                        ),
                        Text(
                          '${fmtNum(numOf(_rows[i]['value']))} ${_rows[i]['unit']}',
                          style: const TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        if (!approved)
                          IconButton(
                            visualDensity: VisualDensity.compact,
                            icon: const Icon(
                              Icons.close,
                              size: 16,
                              color: Pal.mut,
                            ),
                            onPressed: () => setState(() => _rows.removeAt(i)),
                          ),
                      ],
                    ),
                  ),
                if (!approved && _rows.isNotEmpty)
                  FilledButton(
                    onPressed: _busy ? null : _approve,
                    child: const Text('Confirm and save'),
                  ),
              ],
            ),
    );
  }
}
