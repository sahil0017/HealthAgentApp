import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'lookup.dart';

const _slots = ['morning', 'afternoon', 'evening', 'night'];
const _slotLabel = {
  'morning': 'Morning',
  'afternoon': 'Afternoon',
  'evening': 'Evening',
  'night': 'Night',
};
const _slotIcon = {
  'morning': Icons.wb_twilight,
  'afternoon': Icons.wb_sunny_outlined,
  'evening': Icons.wb_cloudy_outlined,
  'night': Icons.nightlight_outlined,
};
const _foods = {
  'any': 'Any time',
  'before': 'Before food',
  'after': 'After food',
  'with': 'With food',
  'empty': 'Empty stomach',
};

String _mimeOf(XFile f) {
  final m = f.mimeType;
  if (m == 'image/jpeg' || m == 'image/png' || m == 'image/webp') return m!;
  final n = f.name.toLowerCase();
  if (n.endsWith('.png')) return 'image/png';
  if (n.endsWith('.webp')) return 'image/webp';
  return 'image/jpeg';
}

String _localDay() {
  final n = DateTime.now();
  return '${n.year}-${n.month.toString().padLeft(2, '0')}-${n.day.toString().padLeft(2, '0')}';
}

/// Today's schedule with the doses already ticked off, for the person's local day.
final doseScheduleProvider = FutureProvider.autoDispose<Json>((ref) {
  final id = ref.watch(profileIdProvider);
  if (id == null) return <String, dynamic>{};
  return ref
      .read(apiProvider)
      .map('/profiles/$id/prescriptions/schedule?day=${_localDay()}');
});

Future<void> _setDose(
  WidgetRef ref,
  BuildContext context,
  Map m,
  String slot,
  bool taken,
) async {
  try {
    await ref.read(apiProvider).post(
      '/profiles/${ref.read(profileIdProvider)}/prescriptions/doses',
      {'item_id': m['id'], 'slot': slot, 'day': _localDay(), 'taken': taken},
    );
    ref.invalidate(doseScheduleProvider);
  } catch (e) {
    if (context.mounted) toast(context, friendly(e));
  }
}

bool _isDue(String slotTime) {
  final p = slotTime.split(':');
  if (p.length != 2) return false;
  final now = DateTime.now();
  final at = DateTime(
    now.year,
    now.month,
    now.day,
    int.tryParse(p[0]) ?? 0,
    int.tryParse(p[1]) ?? 0,
  );
  return !now.isBefore(at);
}

/// Summary-tab card: today's medicines by time of day, with tick-offs and a "due" nudge.
class MedicinesTodayCard extends ConsumerWidget {
  const MedicinesTodayCard({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final d = ref.watch(doseScheduleProvider).asData?.value;
    if (d == null) return const SizedBox.shrink();
    final slots = [
      for (final s in (d['slots'] as List? ?? []).cast<Map>())
        if ((s['items'] as List).isNotEmpty) s,
    ];
    if (slots.isEmpty) return const SizedBox.shrink();
    var total = 0, taken = 0;
    for (final s in slots) {
      for (final m in (s['items'] as List).cast<Map>()) {
        total++;
        if (m['taken'] == true) taken++;
      }
    }
    return SCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.medication_outlined, color: Pal.pri, size: 18),
              const SizedBox(width: 8),
              const Expanded(
                child: Text(
                  'Medicines today',
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                ),
              ),
              Pill('$taken of $total taken'),
            ],
          ),
          for (final s in slots)
            for (final m in (s['items'] as List).cast<Map>())
              _DoseRow(m, '${s['slot']}', '${s['label']}', '${s['time']}'),
        ],
      ),
    );
  }
}

class _DoseRow extends ConsumerWidget {
  const _DoseRow(this.m, this.slot, this.label, this.time);
  final Map m;
  final String slot, label, time;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final taken = m['taken'] == true;
    final due = !taken && _isDue(time);
    final name = [
      m['name'],
      m['strength'],
    ].where((e) => '${e ?? ''}'.isNotEmpty).join(' ');
    return InkWell(
      onTap: () => _setDose(ref, context, m, slot, !taken),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Row(
          children: [
            Icon(
              taken ? Icons.check_circle : Icons.radio_button_unchecked,
              size: 20,
              color: taken ? Pal.pri : (due ? Pal.warn : Pal.mut),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                name,
                style: TextStyle(
                  fontSize: 12.5,
                  decoration: taken ? TextDecoration.lineThrough : null,
                  color: taken ? Pal.mut : null,
                ),
              ),
            ),
            Text(
              due ? '$label · due' : label,
              style: TextStyle(fontSize: 11, color: due ? Pal.warn : Pal.mut),
            ),
          ],
        ),
      ),
    );
  }
}

class PrescriptionsScreen extends ConsumerWidget {
  const PrescriptionsScreen({super.key});

  Future<void> _refresh(WidgetRef ref) async {
    ref.invalidate(doseScheduleProvider);
    ref.invalidate(profileListProvider('/prescriptions'));
  }

  Future<void> _start(
    BuildContext context,
    WidgetRef ref,
    ImageSource? src,
  ) async {
    if (src == null) {
      await Navigator.push(
        context,
        MaterialPageRoute(
          builder: (_) => const PrescriptionReview(draft: null),
        ),
      );
      _refresh(ref);
      return;
    }
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Send this photo to GitHub Copilot?'),
        content: const Text(
          'To read handwriting, the photo is sent to GitHub Copilot, an outside AI service. It is not saved by Sehat. '
          'The reading can be wrong, so you will check every line against the photo before anything is saved.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(c, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Choose photo'),
          ),
        ],
      ),
    );
    if (ok != true || !context.mounted) return;
    final XFile? file;
    try {
      file = await ImagePicker().pickImage(
        source: src,
        maxWidth: 2400,
        imageQuality: 88,
      );
    } catch (_) {
      if (context.mounted)
        toast(context, 'Could not open the camera or photos.');
      return;
    }
    if (file == null || !context.mounted) return;
    final bytes = await file.readAsBytes();
    if (!context.mounted) return;
    await Navigator.push(
      context,
      MaterialPageRoute(
        builder: (_) =>
            PrescriptionReview(draft: _Photo(bytes, _mimeOf(file!))),
      ),
    );
    _refresh(ref);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sched = ref.watch(doseScheduleProvider);
    final saved = ref.watch(profileListProvider('/prescriptions'));
    return Scaffold(
      appBar: AppBar(
        title: const Text('Medications'),
        actions: [
          IconButton(
            tooltip: 'Drug Information',
            icon: const Icon(Icons.search),
            onPressed: () => Navigator.push(
              context,
              MaterialPageRoute<void>(builder: (_) => const MedicineScreen()),
            ),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () => _refresh(ref),
        child: ListView(
          padding: const EdgeInsets.fromLTRB(14, 8, 14, 24),
          children: [
            SCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    'Add medicines',
                    style: TextStyle(fontWeight: FontWeight.w700),
                  ),
                  const SizedBox(height: 4),
                  const Text(
                    'Scan a prescription and each medicine, dose and timing is filled in for you. You confirm every line before it is saved.',
                    style: TextStyle(fontSize: 11, color: Pal.mut),
                  ),
                  const SizedBox(height: 12),
                  Row(
                    children: [
                      Expanded(
                        child: _AddTile(
                          Icons.document_scanner_outlined,
                          'Scan',
                          'Use the camera',
                          () => _start(context, ref, ImageSource.camera),
                          primary: true,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: _AddTile(
                          Icons.photo_library_outlined,
                          'Upload',
                          'From your photos',
                          () => _start(context, ref, ImageSource.gallery),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: _AddTile(
                          Icons.edit_note,
                          'Enter',
                          'Type a medicine',
                          () => _start(context, ref, null),
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            const SectionTitle('Daily schedule'),
            AsyncBody<Json>(
              sched,
              onRetry: () => _refresh(ref),
              builder: (d) => _Schedule(d),
            ),
            const SectionTitle('Saved prescriptions'),
            AsyncBody<List<Json>>(
              saved,
              onRetry: () => _refresh(ref),
              builder: (list) => list.isEmpty
                  ? const Padding(
                      padding: EdgeInsets.all(12),
                      child: Text(
                        'Nothing saved yet.',
                        style: TextStyle(color: Pal.mut, fontSize: 12),
                      ),
                    )
                  : Column(
                      children: [
                        for (final p in list)
                          _SavedCard(p, onChanged: () => _refresh(ref)),
                      ],
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

class _AddTile extends StatelessWidget {
  const _AddTile(
    this.icon,
    this.title,
    this.hint,
    this.onTap, {
    this.primary = false,
  });
  final IconData icon;
  final String title, hint;
  final VoidCallback onTap;
  final bool primary;
  @override
  Widget build(BuildContext context) => InkWell(
    borderRadius: BorderRadius.circular(12),
    onTap: onTap,
    child: Container(
      padding: const EdgeInsets.symmetric(vertical: 12, horizontal: 6),
      decoration: BoxDecoration(
        color: primary ? Pal.pri : null,
        border: Border.all(color: primary ? Pal.pri : Pal.line),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        children: [
          Icon(icon, size: 22, color: primary ? Colors.white : Pal.pri),
          const SizedBox(height: 6),
          Text(
            title,
            style: TextStyle(
              fontWeight: FontWeight.w700,
              fontSize: 12.5,
              color: primary ? Colors.white : null,
            ),
          ),
          Text(
            hint,
            textAlign: TextAlign.center,
            style: TextStyle(
              fontSize: 10,
              color: primary ? Colors.white70 : Pal.mut,
            ),
          ),
        ],
      ),
    ),
  );
}

class _Schedule extends ConsumerWidget {
  const _Schedule(this.data);
  final Json data;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final slots = (data['slots'] as List? ?? []).cast<Map>();
    final asNeeded = (data['as_needed'] as List? ?? []).cast<Map>();
    final unscheduled = (data['unscheduled'] as List? ?? []).cast<Map>();
    final any =
        slots.any((s) => (s['items'] as List).isNotEmpty) ||
        asNeeded.isNotEmpty ||
        unscheduled.isNotEmpty;
    if (!any) {
      return const Padding(
        padding: EdgeInsets.all(12),
        child: Text(
          'Save a prescription to see your day laid out.',
          style: TextStyle(color: Pal.mut, fontSize: 12),
        ),
      );
    }
    Widget med(Map m, [String? slot]) => Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (slot == null)
            const Icon(Icons.medication_outlined, size: 16, color: Pal.pri)
          else
            InkWell(
              onTap: () => _setDose(ref, context, m, slot, m['taken'] != true),
              child: Icon(
                m['taken'] == true
                    ? Icons.check_circle
                    : Icons.radio_button_unchecked,
                size: 20,
                color: m['taken'] == true ? Pal.pri : Pal.mut,
              ),
            ),
          const SizedBox(width: 6),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  [
                    m['name'],
                    m['strength'],
                    m['dose'],
                  ].where((e) => '${e ?? ''}'.isNotEmpty).join(' · '),
                  style: const TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                Text(
                  _foods[m['with_food']] ?? '',
                  style: const TextStyle(fontSize: 11, color: Pal.mut),
                ),
              ],
            ),
          ),
        ],
      ),
    );
    return Column(
      children: [
        for (final s in slots)
          if ((s['items'] as List).isNotEmpty)
            SCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Icon(_slotIcon[s['slot']], size: 18, color: Pal.warn),
                      const SizedBox(width: 6),
                      Text(
                        '${s['label']}',
                        style: const TextStyle(fontWeight: FontWeight.w700),
                      ),
                      const Spacer(),
                      Text(
                        'around ${s['time']}',
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                    ],
                  ),
                  for (final m in (s['items'] as List).cast<Map>())
                    med(m, '${s['slot']}'),
                ],
              ),
            ),
        if (asNeeded.isNotEmpty)
          SCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'Only when needed',
                  style: TextStyle(fontWeight: FontWeight.w700),
                ),
                for (final m in asNeeded) med(m),
              ],
            ),
          ),
        if (unscheduled.isNotEmpty)
          SCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'Timing not set',
                  style: TextStyle(fontWeight: FontWeight.w700),
                ),
                for (final m in unscheduled) med(m),
              ],
            ),
          ),
        Padding(
          padding: const EdgeInsets.all(6),
          child: Text(
            '${data['note'] ?? ''}',
            style: const TextStyle(fontSize: 10.5, color: Pal.mut),
          ),
        ),
      ],
    );
  }
}

class _SavedCard extends ConsumerWidget {
  const _SavedCard(this.rx, {required this.onChanged});
  final Json rx;
  final VoidCallback onChanged;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final items = (rx['items'] as List? ?? []).cast<Map>();
    final title = [
      if ('${rx['doctor'] ?? ''}'.isNotEmpty) '${rx['doctor']}',
      if (rx['prescribed_on'] != null) '${rx['prescribed_on']}',
    ].join(' · ');
    return SCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  title.isEmpty ? 'Prescription' : title,
                  style: const TextStyle(fontWeight: FontWeight.w700),
                ),
              ),
              IconButton(
                tooltip: 'Delete',
                icon: const Icon(Icons.delete_outline, size: 20),
                onPressed: () async {
                  final id = ref.read(profileIdProvider);
                  final api = ref.read(apiProvider);
                  final sure = await showDialog<bool>(
                    context: context,
                    builder: (c) => AlertDialog(
                      title: const Text('Delete this prescription?'),
                      actions: [
                        TextButton(
                          onPressed: () => Navigator.pop(c, false),
                          child: const Text('Keep'),
                        ),
                        FilledButton(
                          onPressed: () => Navigator.pop(c, true),
                          child: const Text('Delete'),
                        ),
                      ],
                    ),
                  );
                  if (sure != true || id == null) return;
                  try {
                    await api.delete('/profiles/$id/prescriptions/${rx['id']}');
                    onChanged();
                  } catch (e) {
                    if (context.mounted) toast(context, friendly(e));
                  }
                },
              ),
            ],
          ),
          for (final m in items) _SavedItem(m),
        ],
      ),
    );
  }
}

class _SavedItem extends ConsumerWidget {
  const _SavedItem(this.m);
  final Map m;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final name = '${m['name']}';
    final use = ref.watch(medicineUseProvider(name)).value;
    return InkWell(
      onTap: () => Navigator.push(
        context,
        MaterialPageRoute<void>(
          builder: (_) => MedicineScreen(initialQuery: name),
        ),
      ),
      child: Padding(
        padding: const EdgeInsets.only(top: 10),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Icon(Icons.medication_outlined, size: 18, color: Pal.pri),
            const SizedBox(width: 8),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '$name ${m['strength'] ?? ''}'.trim(),
                    style: const TextStyle(
                      fontWeight: FontWeight.w600,
                      fontSize: 12.5,
                    ),
                  ),
                  if ('${m['summary'] ?? ''}'.isNotEmpty)
                    Text(
                      '${m['summary']}',
                      style: const TextStyle(fontSize: 11.5, color: Pal.mut),
                    ),
                  if (use != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 3),
                      child: Text(
                        use,
                        style: const TextStyle(fontSize: 11, color: Pal.mut),
                      ),
                    ),
                ],
              ),
            ),
            const Icon(Icons.chevron_right, size: 18, color: Pal.mut),
          ],
        ),
      ),
    );
  }
}

class _Photo {
  _Photo(this.bytes, this.mime);
  final Uint8List bytes;
  final String mime;
}

/// Shows the original photo beside editable lines. Nothing is saved until the person confirms.
class PrescriptionReview extends ConsumerStatefulWidget {
  const PrescriptionReview({super.key, this.draft, this.documentId});
  final _Photo? draft;
  final String? documentId;
  @override
  ConsumerState<PrescriptionReview> createState() => _ReviewState();
}

class _ReviewState extends ConsumerState<PrescriptionReview> {
  final List<Json> _items = [];
  final _doctor = TextEditingController();
  String? _date;
  String _note = '';
  List<String> _unreadable = [];
  bool _loading = false, _saving = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    if (widget.draft == null && widget.documentId == null) {
      _items.add(_blank());
    } else {
      _loading = true;
      WidgetsBinding.instance.addPostFrameCallback((_) => _read());
    }
  }

  Json _blank() => {
    'name': '',
    'strength': '',
    'form': '',
    'dose': '',
    'frequency_text': '',
    'timing_text': '',
    'duration_text': '',
    'instructions': '',
    'slots': <String>[],
    'with_food': 'any',
    'as_needed': false,
    'confidence': null,
    'understood': false,
  };

  Future<void> _read() async {
    final id = ref.read(profileIdProvider);
    if (id == null) return;
    try {
      final d = widget.documentId != null
          ? await ref
                .read(apiProvider)
                .get(
                  '/profiles/$id/prescriptions/from-document/${widget.documentId}',
                )
          : await ref
                .read(apiProvider)
                .postBytes(
                  '/profiles/$id/prescriptions/read?cloud=1',
                  widget.draft!.bytes,
                  widget.draft!.mime,
                );
      final m = Map<String, dynamic>.from(d as Map);
      setState(() {
        _loading = false;
        _doctor.text = '${m['doctor'] ?? ''}';
        _date = _isoDate('${m['date'] ?? ''}');
        _note = '${m['note'] ?? ''}';
        _unreadable = (m['unreadable'] as List? ?? [])
            .map((e) => '$e')
            .toList();
        for (final i in (m['items'] as List? ?? [])) {
          final it = Map<String, dynamic>.from(i as Map);
          it['slots'] = (it['slots'] as List? ?? []).map((e) => '$e').toList();
          _items.add(it);
        }
        if (m['is_prescription'] == false) {
          _error = 'This does not look like a prescription. Try a clearer photo, or add the medicines by hand.';
        } else if (_items.isEmpty) {
          _error = 'No medicines could be read. Try a clearer photo, or add them by hand.';
        }
      });
    } catch (e) {
      setState(() {
        _loading = false;
        _error = friendly(e);
      });
    }
  }

  String? _isoDate(String s) {
    final m = RegExp(r'^(\d{4})-(\d{2})-(\d{2})$').firstMatch(s);
    return m == null ? null : s;
  }

  Future<void> _save() async {
    final id = ref.read(profileIdProvider);
    final items = _items
        .where((i) => '${i['name']}'.trim().isNotEmpty)
        .toList();
    if (id == null || items.isEmpty) {
      toast(context, 'Add at least one medicine with a name.');
      return;
    }
    setState(() => _saving = true);
    try {
      await ref.read(apiProvider).post('/profiles/$id/prescriptions', {
        'doctor': _doctor.text.trim(),
        'prescribed_on': _date,
        'source': widget.draft == null && widget.documentId == null
            ? 'manual'
            : 'photo',
        'items': items,
      });
      if (mounted) Navigator.pop(context);
    } catch (e) {
      if (mounted) {
        setState(() => _saving = false);
        toast(context, friendly(e));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(
          widget.draft == null && widget.documentId == null
              ? 'Add medicines'
              : 'Check the reading',
        ),
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(14, 8, 14, 24),
        children: [
          if (widget.draft != null)
            ClipRRect(
              borderRadius: BorderRadius.circular(12),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 260),
                child: InteractiveViewer(
                  child: Image.memory(widget.draft!.bytes, fit: BoxFit.contain),
                ),
              ),
            ),
          if (_loading)
            const Padding(
              padding: EdgeInsets.all(24),
              child: Column(
                children: [
                  CircularProgressIndicator(),
                  SizedBox(height: 10),
                  Text(
                    'Reading your prescription…',
                    style: TextStyle(color: Pal.mut),
                  ),
                ],
              ),
            )
          else ...[
            if (_note.isNotEmpty)
              SCard(
                color: Pal.warnBg,
                child: Text(_note, style: const TextStyle(fontSize: 11.5)),
              ),
            if (_unreadable.isNotEmpty)
              SCard(
                color: Pal.warnBg,
                child: Text(
                  'Could not read: ${_unreadable.join('; ')}',
                  style: const TextStyle(fontSize: 11.5),
                ),
              ),
            if (_error != null) SCard(color: Pal.badBg, child: Text(_error!)),
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 6),
              child: TextField(
                controller: _doctor,
                decoration: const InputDecoration(
                  labelText: 'Doctor (optional)',
                ),
              ),
            ),
            for (var i = 0; i < _items.length; i++)
              _ItemEditor(
                key: ObjectKey(_items[i]),
                item: _items[i],
                onRemove: () => setState(() => _items.removeAt(i)),
              ),
            TextButton.icon(
              onPressed: () => setState(() => _items.add(_blank())),
              icon: const Icon(Icons.add),
              label: const Text('Add a medicine'),
            ),
            const SizedBox(height: 8),
            FilledButton(
              onPressed: _saving ? null : _save,
              child: Text(_saving ? 'Saving…' : 'These are correct, save'),
            ),
            const SizedBox(height: 6),
            const Text(
              'Timings follow your prescription shorthand, and you can adjust them any time.',
              style: TextStyle(fontSize: 10.5, color: Pal.mut),
            ),
          ],
        ],
      ),
    );
  }
}

class _ItemEditor extends StatefulWidget {
  const _ItemEditor({super.key, required this.item, required this.onRemove});
  final Json item;
  final VoidCallback onRemove;
  @override
  State<_ItemEditor> createState() => _ItemEditorState();
}

class _ItemEditorState extends State<_ItemEditor> {
  Json get it => widget.item;

  Widget _field(String label, String key, {int flex = 1}) => Expanded(
    flex: flex,
    child: Padding(
      padding: const EdgeInsets.only(right: 6, top: 6),
      child: TextFormField(
        initialValue: '${it[key] ?? ''}',
        decoration: InputDecoration(labelText: label, isDense: true),
        onChanged: (v) => it[key] = v,
      ),
    ),
  );

  @override
  Widget build(BuildContext context) {
    final conf = (it['confidence'] as num?)?.toDouble();
    final low = conf != null && conf < 0.7;
    final slots = (it['slots'] as List).cast<String>();
    return SCard(
      color: low ? Pal.warnBg : null,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              if (conf != null)
                low
                    ? Pill(
                        'Check carefully ${(conf * 100).round()}%',
                        fg: Pal.warn,
                        bg: Pal.warnBg,
                      )
                    : Pill(
                        'Read ${(conf * 100).round()}%',
                        fg: Pal.ok,
                        bg: Pal.okBg,
                      ),
              if (it['understood'] == false &&
                  '${it['frequency_text']}'.isNotEmpty)
                const Padding(
                  padding: EdgeInsets.only(left: 6),
                  child: Pill('Timing unclear', fg: Pal.warn, bg: Pal.warnBg),
                ),
              const Spacer(),
              IconButton(
                tooltip: 'Remove',
                icon: const Icon(Icons.close, size: 18),
                onPressed: widget.onRemove,
              ),
            ],
          ),
          Row(
            children: [
              _field('Medicine', 'name', flex: 3),
              _field('Strength', 'strength', flex: 2),
            ],
          ),
          Row(children: [_field('Form', 'form'), _field('Dose', 'dose')]),
          Row(
            children: [
              _field('Frequency as written', 'frequency_text'),
              _field('Duration', 'duration_text'),
            ],
          ),
          const SizedBox(height: 8),
          const Text(
            'When to take it',
            style: TextStyle(fontSize: 11, color: Pal.mut),
          ),
          Wrap(
            spacing: 6,
            children: [
              for (final s in _slots)
                FilterChip(
                  label: Text(_slotLabel[s]!),
                  selected: slots.contains(s),
                  onSelected: (on) => setState(() {
                    on ? slots.add(s) : slots.remove(s);
                    it['understood'] =
                        slots.isNotEmpty || it['as_needed'] == true;
                  }),
                ),
              FilterChip(
                label: const Text('Only when needed'),
                selected: it['as_needed'] == true,
                onSelected: (on) => setState(() {
                  it['as_needed'] = on;
                  it['understood'] = slots.isNotEmpty || on;
                }),
              ),
            ],
          ),
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: DropdownButtonFormField<String>(
              initialValue: _foods.containsKey(it['with_food'])
                  ? it['with_food'] as String
                  : 'any',
              decoration: const InputDecoration(
                labelText: 'Food',
                isDense: true,
              ),
              items: [
                for (final e in _foods.entries)
                  DropdownMenuItem(value: e.key, child: Text(e.value)),
              ],
              onChanged: (v) => it['with_food'] = v ?? 'any',
            ),
          ),
          Row(children: [_field('Notes', 'instructions')]),
        ],
      ),
    );
  }
}
