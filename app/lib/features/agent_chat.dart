import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:speech_to_text/speech_to_text.dart';

import '../core/api.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'agent_screens.dart';
import 'health.dart';
import 'places.dart';
import 'plan.dart';

const _note = TextStyle(fontSize: 10.5, color: Pal.mut);

final _permsProvider = FutureProvider.autoDispose<List<Json>>((ref) {
  final id = ref.watch(profileIdProvider);
  if (id == null) return <Json>[];
  return ref
      .read(apiProvider)
      .list('/profiles/$id/agent/permissions', key: 'permissions');
});

final _promptsProvider = FutureProvider.autoDispose<List<String>>((ref) async {
  final id = ref.watch(profileIdProvider);
  if (id == null) return [];
  final r = await ref.read(apiProvider).map('/profiles/$id/agent/suggestions');
  final l = r['suggestions'];
  return l is List ? [for (final s in l) '$s'] : [];
});

const _modes = [
  ('explain', 'Explain', Icons.lightbulb_outline),
  ('plan', 'Plan', Icons.checklist_rtl),
  ('visit', 'Visit prep', Icons.event_note_outlined),
];

class _Msg {
  _Msg.user(this.text) : user = true;
  _Msg.agent([this.mode = 'explain']) : user = false;
  final bool user;
  String mode = 'explain';
  String text = '';
  List<Json> steps = [];
  List<Json> actions = [];
  int stepsShown = 0;
  bool thinking = false;
  bool done = false;
  bool urgent = false;
  String source = 'records';
  String model = '';
  String? error;
  bool needsPermission = false;
}

/// A chat with the agent: it reads the record with tools, shows each step, and offers actions to tap.
class AgentChatScreen extends ConsumerStatefulWidget {
  const AgentChatScreen({super.key, this.embedded = false, this.report});

  /// A report (`id`, `title`) the chat is limited to, set by "Ask Sehat about this report".
  final Map<String, String>? report;

  /// True when shown as the Agent tab: no app bar, tools sit in a slim toolbar.
  final bool embedded;
  @override
  ConsumerState<AgentChatScreen> createState() => _ChatState();
}

class _ChatState extends ConsumerState<AgentChatScreen> {
  @override
  void initState() {
    super.initState();
    _report = widget.report;
  }

  final _text = TextEditingController();
  final _scroll = ScrollController();
  final _speech = SpeechToText();
  final _msgs = <_Msg>[];
  String _mode = 'explain';
  Map<String, String>? _report;
  bool _busy = false;
  bool _listening = false;

  @override
  void dispose() {
    _speech.cancel();
    _text.dispose();
    _scroll.dispose();
    super.dispose();
  }

  void _toBottom() => WidgetsBinding.instance.addPostFrameCallback((_) {
    if (_scroll.hasClients) {
      _scroll.animateTo(
        _scroll.position.maxScrollExtent,
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOut,
      );
    }
  });

  Future<void> _send(String q) async {
    q = q.trim();
    if (q.isEmpty || _busy) return;
    FocusScope.of(context).unfocus();
    final history = [
      for (final m
          in _msgs
              .where((m) => m.text.isNotEmpty)
              .toList()
              .reversed
              .take(6)
              .toList()
              .reversed)
        {'role': m.user ? 'user' : 'assistant', 'content': m.text},
    ];
    final reply = _Msg.agent(_mode)..thinking = true;
    setState(() {
      _msgs
        ..add(_Msg.user(q))
        ..add(reply);
      _busy = true;
      _text.clear();
    });
    _toBottom();
    try {
      final r = await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/agent/chat',
        {
          'message': q,
          'mode': _mode,
          'history': history,
          if (_report != null) 'report_id': _report!['id'],
        },
      );
      final d = r is Map ? r.cast<String, dynamic>() : <String, dynamic>{};
      reply
        ..steps = [
          for (final s in (d['steps'] as List? ?? []))
            (s as Map).cast<String, dynamic>(),
        ]
        ..actions = [
          for (final s in (d['actions'] as List? ?? []))
            (s as Map).cast<String, dynamic>(),
        ]
        ..source = '${d['source'] ?? 'records'}'
        ..model = '${d['model'] ?? ''}'
        ..urgent = d['urgent'] == true
        ..thinking = false;
      // Reveal each tool step, then type the answer out, so the user can follow the work.
      for (var i = 1; i <= reply.steps.length; i++) {
        if (!mounted) return;
        setState(() => reply.stepsShown = i);
        _toBottom();
        await Future<void>.delayed(const Duration(milliseconds: 450));
      }
      final full = '${d['answer'] ?? ''}';
      for (var i = 0; i < full.length; i += 3) {
        if (!mounted) return;
        setState(
          () => reply.text = full.substring(0, (i + 3).clamp(0, full.length)),
        );
        _toBottom();
        await Future<void>.delayed(const Duration(milliseconds: 12));
      }
      if (mounted) setState(() => reply.done = true);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        reply
          ..thinking = false
          ..done = true
          ..error = friendly(e)
          ..needsPermission = e is ApiError && e.code == 'PERMISSION_REQUIRED';
      });
    } finally {
      if (mounted) setState(() => _busy = false);
      _toBottom();
    }
  }

  Future<void> _mic() async {
    if (_listening) {
      await _speech.stop();
      if (mounted) setState(() => _listening = false);
      return;
    }
    final ok = await _speech.initialize(
      onStatus: (s) {
        if ((s == 'done' || s == 'notListening') && mounted) {
          setState(() => _listening = false);
        }
      },
      onError: (_) {
        if (mounted) setState(() => _listening = false);
      },
    );
    if (!ok) {
      if (mounted)
        toast(
          context,
          'Voice input is not available here. Type your question instead.',
        );
      return;
    }
    setState(() => _listening = true);
    await _speech.listen(
      onResult: (r) {
        _text.text = r.recognizedWords;
        _text.selection = TextSelection.collapsed(offset: _text.text.length);
        if (r.finalResult && mounted) setState(() => _listening = false);
      },
    );
  }

  Future<void> _addStep(String step) async {
    try {
      await ref.read(apiProvider).post(
        '/profiles/${ref.read(profileIdProvider)}/follow-ups',
        {'kind': 'other', 'title': step},
      );
      ref.invalidate(profileMapProvider('/overview'));
      if (mounted) toast(context, 'Added to your care plan');
    } catch (e) {
      if (mounted) toast(context, friendly(e));
    }
  }

  Future<void> _act(Json a) async {
    final id = ref.read(profileIdProvider);
    final code = '${a['biomarker_code'] ?? ''}', name = '${a['name'] ?? ''}';
    void open(Widget w) =>
        Navigator.push(context, MaterialPageRoute(builder: (_) => w));
    switch (a['kind']) {
      case 'open_trend':
        open(BiomarkerScreen(code, name));
      case 'set_goal':
        open(GoalForm(initialCode: code));
      case 'visit_prep':
        open(const VisitPrepScreen());
      case 'emergency':
        open(const EmergencyScreen());
      case 'follow_up':
        try {
          await ref.read(apiProvider).post('/profiles/$id/follow-ups', {
            'kind': 'other',
            'title': 'Discuss $name with my doctor',
          });
          ref.invalidate(profileMapProvider('/overview'));
          if (mounted) toast(context, 'Added to your follow-ups');
        } catch (e) {
          if (mounted) toast(context, friendly(e));
        }
    }
  }

  void _cloudSheet(bool on) {
    showModalBottomSheet<void>(
      context: context,
      backgroundColor: Pal.card,
      showDragHandle: true,
      builder: (ctx) => Padding(
        padding: const EdgeInsets.fromLTRB(18, 0, 18, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Cloud AI model',
              style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 8),
            const Text(
              'Off: answers are built only from your record on our server.\n\n'
              'On: the agent may send the lab names, values and dates it reads for your question to a Hugging Face–hosted model to write a clearer answer. Your name, profile and documents are not sent. Every answer is checked: numbers must come from your record and diagnosis or treatment wording is rejected.',
              style: TextStyle(fontSize: 12, color: Pal.mut, height: 1.4),
            ),
            const SizedBox(height: 10),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Use cloud AI model'),
              value: on,
              onChanged: (v) async {
                Navigator.pop(ctx);
                try {
                  await ref.read(apiProvider).put(
                    '/profiles/${ref.read(profileIdProvider)}/agent/permissions/cloud_model',
                    {'granted': v},
                  );
                  ref.invalidate(_permsProvider);
                } catch (e) {
                  if (mounted) toast(context, friendly(e));
                }
              },
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _whatIf() async {
    final q = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Pal.card,
      showDragHandle: true,
      builder: (_) => const _WhatIfSheet(),
    );
    if (q != null) {
      setState(() => _mode = 'explain');
      _send(q);
    }
  }

  @override
  Widget build(BuildContext context) {
    final perms = ref.watch(_permsProvider).asData?.value ?? [];
    final cloud = perms.any(
      (p) => p['permission'] == 'cloud_model' && p['granted'] == true,
    );
    final tools = <Widget>[
      IconButton(
        tooltip: 'What-if',
        icon: const Icon(Icons.tune),
        onPressed: _busy ? null : _whatIf,
      ),
      IconButton(
        tooltip: 'New chat',
        icon: const Icon(Icons.refresh),
        onPressed: _busy ? null : () => setState(_msgs.clear),
      ),
      PopupMenuButton<Widget>(
        tooltip: 'Agent tools',
        icon: const Icon(Icons.more_vert),
        onSelected: (w) =>
            Navigator.push(context, MaterialPageRoute<void>(builder: (_) => w)),
        itemBuilder: (_) => const [
          PopupMenuItem(value: BriefingScreen(), child: Text('Daily briefing')),
          PopupMenuItem(value: RunAgentScreen(), child: Text('Run a check-up')),
          PopupMenuItem(value: ApprovalsScreen(), child: Text('Approvals')),
          PopupMenuItem(value: VisitPrepScreen(), child: Text('Visit prep')),
          PopupMenuItem(value: ActivityScreen(), child: Text('Activity')),
          PopupMenuItem(value: PermissionsScreen(), child: Text('Permissions')),
        ],
      ),
    ];
    return Scaffold(
      appBar: widget.embedded
          ? null
          : AppBar(title: const Text('Sehat'), actions: tools),
      body: Column(
        children: [
          if (_report != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 8, 14, 0),
              child: Align(
                alignment: Alignment.centerLeft,
                child: InputChip(
                  avatar: const Icon(Icons.description_outlined, size: 16),
                  label: Text('Report: ${_report!['title']}'),
                  onDeleted: () => setState(() => _report = null),
                ),
              ),
            ),
          if (widget.embedded)
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 4, 4, 0),
              child: Row(
                children: [
                  const Icon(Icons.auto_awesome, color: Pal.pri, size: 18),
                  const SizedBox(width: 8),
                  const Expanded(
                    child: Text(
                      'Sehat',
                      style: TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                  ...tools,
                ],
              ),
            ),
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 4, 14, 0),
            child: Row(
              children: [
                Expanded(
                  child: SingleChildScrollView(
                    scrollDirection: Axis.horizontal,
                    child: Row(
                      children: [
                        for (final m in _modes)
                          Padding(
                            padding: const EdgeInsets.only(right: 6),
                            child: ChoiceChip(
                              avatar: Icon(m.$3, size: 14),
                              label: Text(
                                m.$2,
                                style: const TextStyle(fontSize: 11),
                              ),
                              selected: _mode == m.$1,
                              onSelected: _busy
                                  ? null
                                  : (_) => setState(() => _mode = m.$1),
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
                ActionChip(
                  avatar: Icon(
                    cloud ? Icons.cloud_done_outlined : Icons.shield_outlined,
                    size: 14,
                  ),
                  label: Text(
                    cloud ? 'Cloud AI' : 'Records only',
                    style: const TextStyle(fontSize: 11),
                  ),
                  onPressed: () => _cloudSheet(cloud),
                ),
              ],
            ),
          ),
          Expanded(
            child: _msgs.isEmpty
                ? _Intro(mode: _mode, onPick: _send)
                : ListView.builder(
                    controller: _scroll,
                    padding: const EdgeInsets.fromLTRB(14, 10, 14, 10),
                    itemCount: _msgs.length,
                    itemBuilder: (_, i) => _Bubble(
                      _msgs[i],
                      onAction: _act,
                      onAddStep: _addStep,
                      onPermissions: () {
                        Navigator.push(
                          context,
                          MaterialPageRoute(
                            builder: (_) => const PermissionsScreen(),
                          ),
                        );
                      },
                    ),
                  ),
          ),
          SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(14, 4, 14, 8),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _text,
                      minLines: 1,
                      maxLines: 3,
                      maxLength: 1000,
                      buildCounter: (
                        _, {
                        required currentLength,
                        required isFocused,
                        maxLength,
                      }) => null,
                      textInputAction: TextInputAction.send,
                      onSubmitted: _busy ? null : _send,
                      decoration: InputDecoration(
                        isDense: true,
                        hintText: _listening
                            ? 'Listening…'
                            : switch (_mode) {
                                'plan' => 'What do you want a plan for?',
                                'visit' => 'Which visit are you preparing for?',
                                _ => 'Ask what a result or term means',
                              },
                        suffixIcon: IconButton(
                          tooltip: 'Speak',
                          icon: Icon(
                            _listening
                                ? Icons.stop_circle_outlined
                                : Icons.mic_none,
                            color: _listening ? Pal.bad : Pal.mut,
                          ),
                          onPressed: _busy ? null : _mic,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton.filled(
                    onPressed: _busy ? null : () => _send(_text.text),
                    icon: const Icon(Icons.arrow_upward_rounded),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _Intro extends ConsumerWidget {
  const _Intro({required this.onPick, required this.mode});
  final void Function(String) onPick;
  final String mode;

  static const _planCards = [
    (
      Icons.monitor_heart_outlined,
      'Bring a result back in range',
      'Steps tied to your latest numbers',
      'Make me a plan to bring my out-of-range results back to normal',
    ),
    (
      Icons.restaurant_outlined,
      'Eat and sleep better',
      'Small daily habits that fit your results',
      'Plan the next 2 weeks of food, sleep and activity for my results',
    ),
    (
      Icons.event_repeat,
      'Retest schedule',
      'What to repeat and when',
      'Which tests should I repeat, and when?',
    ),
  ];

  static const _visitCards = [
    (
      Icons.event_note_outlined,
      'Prepare for my next visit',
      'What changed, what to raise, what to ask',
      'Prepare me for my next doctor visit',
    ),
    (
      Icons.compare_arrows,
      'Since my last report',
      'Only what moved since the previous one',
      'What should I tell my doctor changed since my previous report?',
    ),
  ];

  static const _cards = [
    (
      Icons.biotech_outlined,
      'Understand my results',
      'What is outside my range, and why it matters',
      'What is outside my range?',
    ),
    (
      Icons.trending_up,
      'What changed',
      'Trends since your last report',
      'What changed since last time?',
    ),
    (
      Icons.event_note_outlined,
      'Prepare for my doctor',
      'Questions and a summary to carry along',
      'Prepare me for my next doctor visit',
    ),
    (
      Icons.restaurant_outlined,
      'Improve with lifestyle',
      'Food, sleep and activity tied to your numbers',
      'What lifestyle changes would help my results most?',
    ),
  ];

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final extra = ref.watch(_promptsProvider).asData?.value ?? [];
    return ListView(
      padding: const EdgeInsets.all(14),
      children: [
        const SizedBox(height: 12),
        Row(
          children: [
            const CircleAvatar(
              radius: 22,
              backgroundColor: Pal.priBg,
              child: Icon(Icons.auto_awesome, color: Pal.pri),
            ),
            const SizedBox(width: 12),
            const Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Hi, I’m Sehat',
                    style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
                  ),
                  SizedBox(height: 2),
                  Text(
                    'I read your own reports, medicines and check-ins, then answer with the numbers behind it.',
                    style: TextStyle(
                      fontSize: 12,
                      color: Pal.mut,
                      height: 1.35,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
        const SizedBox(height: 16),
        for (final c in switch (mode) {
          'plan' => _planCards,
          'visit' => _visitCards,
          _ => _cards,
        })
          SCard(
            onTap: () => onPick(c.$4),
            child: Row(
              children: [
                CircleAvatar(
                  radius: 18,
                  backgroundColor: Pal.priBg,
                  child: Icon(c.$1, size: 18, color: Pal.pri),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        c.$2,
                        style: const TextStyle(
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      Text(c.$3, style: _note),
                    ],
                  ),
                ),
                const Icon(Icons.chevron_right, color: Pal.mut),
              ],
            ),
          ),
        if (extra.isNotEmpty) ...[
          const SectionTitle('Asked about your record'),
          Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              for (final s in extra.take(4))
                ActionChip(
                  label: Text(s, style: const TextStyle(fontSize: 11.5)),
                  onPressed: () => onPick(s),
                ),
            ],
          ),
        ],
        const SizedBox(height: 10),
        const Text(
          'Tip: the tune icon lets you try “what if” values, and the mic lets you speak.',
          textAlign: TextAlign.center,
          style: _note,
        ),
      ],
    );
  }
}

class _Bubble extends StatelessWidget {
  const _Bubble(
    this.m, {
    required this.onAction,
    required this.onAddStep,
    required this.onPermissions,
  });
  final _Msg m;
  final void Function(String) onAddStep;
  final void Function(Json) onAction;
  final VoidCallback onPermissions;
  @override
  Widget build(BuildContext context) {
    if (m.user) {
      return Align(
        alignment: Alignment.centerRight,
        child: Container(
          margin: const EdgeInsets.only(bottom: 10, left: 40),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
          decoration: BoxDecoration(
            color: Pal.priBg,
            borderRadius: BorderRadius.circular(14),
          ),
          child: Text(m.text, style: const TextStyle(fontSize: 13)),
        ),
      );
    }
    return Align(
      alignment: Alignment.centerLeft,
      child: Container(
        margin: const EdgeInsets.only(bottom: 12, right: 24),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: m.urgent ? Pal.badBg : Pal.card,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(color: m.urgent ? Pal.bad : Pal.line),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (m.thinking)
              const Row(
                children: [
                  SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  SizedBox(width: 8),
                  Text('Reading your record…', style: _note),
                ],
              ),
            if (m.done && m.stepsShown > 0)
              Theme(
                data: Theme.of(context)
                    .copyWith(dividerColor: Colors.transparent),
                child: ExpansionTile(
                  tilePadding: EdgeInsets.zero,
                  childrenPadding: EdgeInsets.zero,
                  dense: true,
                  title: Text(
                    'Looked at ${m.stepsShown} part${m.stepsShown == 1 ? '' : 's'} of your record',
                    style: _note,
                  ),
                  children: [
                    for (var i = 0; i < m.stepsShown; i++)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 5),
                        child: Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            const Icon(
                              Icons.check_circle,
                              size: 14,
                              color: Pal.pri,
                            ),
                            const SizedBox(width: 6),
                            Expanded(
                              child: Text.rich(
                                TextSpan(
                                  text: '${m.steps[i]['label']}',
                                  style: const TextStyle(fontSize: 11.5),
                                  children: [
                                    TextSpan(
                                      text: '  ${m.steps[i]['summary']}',
                                      style: _note,
                                    ),
                                  ],
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                  ],
                ),
              ),
            for (var i = 0; i < (m.done ? 0 : m.stepsShown); i++)
              Padding(
                padding: const EdgeInsets.only(bottom: 5),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Icon(Icons.check_circle, size: 14, color: Pal.pri),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text.rich(
                        TextSpan(
                          text: '${m.steps[i]['label']}',
                          style: const TextStyle(fontSize: 11.5),
                          children: [
                            TextSpan(
                              text: '  ${m.steps[i]['summary']}',
                              style: _note,
                            ),
                          ],
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            if (m.text.isNotEmpty) ...[
              if (m.stepsShown > 0) const SizedBox(height: 6),
              _body(),
            ],
            if (m.error != null) ...[
              Text(
                m.error!,
                style: const TextStyle(color: Pal.bad, fontSize: 12),
              ),
              if (m.needsPermission)
                TextButton(
                  onPressed: onPermissions,
                  child: const Text('Open permissions'),
                ),
            ],
            if (m.done && m.error == null && !m.urgent) ...[
              const SizedBox(height: 8),
              Row(
                children: [
                  Pill(
                    m.source == 'copilot' || m.source == 'model'
                        ? 'Sehat AI'
                        : 'From your records',
                  ),
                  const SizedBox(width: 6),
                  const Expanded(
                    child: Text('Based on your records', style: _note),
                  ),
                ],
              ),
            ],
            if (m.done && m.actions.isNotEmpty) ...[
              const SizedBox(height: 8),
              Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [
                  for (final a in m.actions)
                    ActionChip(
                      avatar: Icon(_icon('${a['kind']}'), size: 14),
                      label: Text(
                        '${a['label']}',
                        style: const TextStyle(fontSize: 11),
                      ),
                      onPressed: () => onAction(a),
                    ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }

  IconData _icon(String kind) => switch (kind) {
    'open_trend' => Icons.show_chart,
    'set_goal' => Icons.flag_outlined,
    'follow_up' => Icons.add_task,
    'visit_prep' => Icons.event_note_outlined,
    'emergency' => Icons.emergency_outlined,
    _ => Icons.touch_app_outlined,
  };
}

/// Drag a value for one of your biomarkers and see where it would sit in its reference range.
class _WhatIfSheet extends ConsumerStatefulWidget {
  const _WhatIfSheet();
  @override
  ConsumerState<_WhatIfSheet> createState() => _WhatIfState();
}

class _WhatIfState extends ConsumerState<_WhatIfSheet> {
  Json? _b;
  double? _v;

  @override
  Widget build(BuildContext context) {
    final list =
        (ref.watch(profileListProvider('/biomarkers/summary')).asData?.value ??
                [])
            .where((b) => numOf((b['latest'] as Map?)?['value']) != null)
            .toList();
    final latest = (_b?['latest'] as Map?)?.cast<String, dynamic>();
    final range = (latest?['range'] as Map?)?.cast<String, dynamic>();
    final cur = numOf(latest?['value'])?.toDouble();
    final lo = numOf(range?['low'])?.toDouble(),
        hi = numOf(range?['high'])?.toDouble();
    double min = 0, max = 1;
    if (cur != null) {
      final span = (lo != null && hi != null && hi > lo)
          ? hi - lo
          : cur.abs() * .5 + 1;
      min = ((lo ?? cur) - span).clamp(0, double.infinity).toDouble();
      max = (hi ?? cur) + span;
      if (cur > max) max = cur * 1.2;
      if (cur < min) min = cur * .8;
    }
    final v = (_v ?? cur ?? 0).clamp(min, max).toDouble();
    final unit = '${latest?['unit'] ?? _b?['unit'] ?? ''}';
    final status = lo != null && v < lo
        ? ('Below range', Pal.warn)
        : hi != null && v > hi
        ? ('Above range', Pal.bad)
        : (lo != null || hi != null
              ? ('In range', Pal.ok)
              : ('No range on file', Pal.mut));
    return Padding(
      padding: EdgeInsets.fromLTRB(
        18,
        0,
        18,
        18 + MediaQuery.of(context).viewInsets.bottom,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'What if…',
            style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
          ),
          const Text(
            'Move the slider to try a value. Nothing is saved.',
            style: _note,
          ),
          const SizedBox(height: 10),
          if (list.isEmpty)
            const Text(
              'Add a lab result first to try what-if values.',
              style: TextStyle(fontSize: 12, color: Pal.mut),
            )
          else ...[
            DropdownButtonFormField<Json>(
              initialValue: _b,
              decoration: const InputDecoration(labelText: 'Biomarker'),
              items: [
                for (final b in list)
                  DropdownMenuItem(
                    value: b,
                    child: Text('${b['display_name']}'),
                  ),
              ],
              onChanged: (b) => setState(() {
                _b = b;
                _v = null;
              }),
            ),
            if (cur != null) ...[
              const SizedBox(height: 16),
              Row(
                children: [
                  Text(
                    v.toStringAsFixed(v.abs() < 10 ? 1 : 0),
                    style: const TextStyle(
                      fontSize: 26,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(width: 6),
                  Text(unit, style: _note),
                  const Spacer(),
                  Pill(status.$1, fg: status.$2, bg: Pal.line),
                ],
              ),
              Slider(
                value: v,
                min: min,
                max: max,
                onChanged: (x) => setState(() => _v = x),
              ),
              RangeBar(value: v, low: lo, high: hi),
              const SizedBox(height: 4),
              Text(
                'Your latest recorded value: ${cur.toStringAsFixed(cur.abs() < 10 ? 1 : 0)} $unit'
                '${lo != null && hi != null ? ' · reference ${lo.toStringAsFixed(1)}–${hi.toStringAsFixed(1)}' : ''}',
                style: _note,
              ),
              const SizedBox(height: 14),
              FilledButton.icon(
                icon: const Icon(Icons.auto_awesome, size: 16),
                label: const Text('Ask the agent about this'),
                onPressed: () => Navigator.pop(
                  context,
                  'What would it mean if my ${_b!['display_name']} were ${v.toStringAsFixed(v.abs() < 10 ? 1 : 0)} $unit?',
                ),
              ),
            ],
          ],
        ],
      ),
    );
  }
}

extension on _Bubble {
  static final _num = RegExp(r'^\s*(?:\d+[.)]|[-*•])\s+');

  Widget _plain(String t) =>
      Text(t, style: const TextStyle(fontSize: 13, height: 1.4));

  Widget _body() {
    if (m.mode == 'plan' && m.done) {
      final steps = [
        for (final l in m.text.split('\n'))
          if (_num.hasMatch(l)) l.replaceFirst(_num, '').trim(),
      ];
      if (steps.length >= 2) return _planBody(steps);
    }
    if (m.mode == 'visit' && m.done) {
      final parts = _sections(m.text);
      if (parts.isNotEmpty) return _visitBody(parts);
    }
    return _plain(m.text);
  }

  Widget _planBody(List<String> steps) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      for (var i = 0; i < steps.length; i++)
        Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              CircleAvatar(
                radius: 11,
                backgroundColor: Pal.priBg,
                child: Text(
                  '${i + 1}',
                  style: const TextStyle(
                    fontSize: 11,
                    color: Pal.pri,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  steps[i],
                  style: const TextStyle(fontSize: 13, height: 1.35),
                ),
              ),
              IconButton(
                tooltip: 'Add to care plan',
                visualDensity: VisualDensity.compact,
                icon: const Icon(Icons.add_task, size: 18, color: Pal.pri),
                onPressed: () => onAddStep(steps[i]),
              ),
            ],
          ),
        ),
    ],
  );

  List<(String, List<String>)> _sections(String t) {
    final out = <(String, List<String>)>[];
    for (final raw in t.split('\n')) {
      final l = raw.trim();
      if (l.isEmpty) continue;
      if (l.endsWith(':') && !_num.hasMatch(l)) {
        out.add((l.substring(0, l.length - 1), []));
      } else if (out.isNotEmpty) {
        out.last.$2.add(l.replaceFirst(_num, ''));
      }
    }
    return [
      for (final s in out)
        if (s.$2.isNotEmpty) s,
    ];
  }

  Widget _visitBody(List<(String, List<String>)> parts) {
    IconData icon(String h) => switch (h.toLowerCase()) {
      final x when x.contains('changed') => Icons.trending_up,
      final x when x.contains('raising') => Icons.flag_outlined,
      _ => Icons.help_outline,
    };
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final (h, items) in parts) ...[
          Row(
            children: [
              Icon(icon(h), size: 16, color: Pal.pri),
              const SizedBox(width: 6),
              Text(
                h,
                style: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          for (final i in items)
            Padding(
              padding: const EdgeInsets.only(left: 22, bottom: 4),
              child: Text(
                '• $i',
                style: const TextStyle(fontSize: 12.5, height: 1.35),
              ),
            ),
          const SizedBox(height: 6),
        ],
      ],
    );
  }
}
