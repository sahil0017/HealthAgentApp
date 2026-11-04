import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';

class SignInScreen extends ConsumerStatefulWidget {
  const SignInScreen({super.key});
  @override
  ConsumerState<SignInScreen> createState() => _SignInState();
}

class _SignInState extends ConsumerState<SignInScreen> {
  bool _busy = false;
  String? _error;

  Future<void> _run(Future<void> Function() f) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await f();
      ref.read(authProvider.notifier).signedIn();
    } catch (e) {
      if (mounted) setState(() => _error = friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final id = ref.read(identityProvider);
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Spacer(),
              const Icon(Icons.favorite_rounded, size: 40, color: Pal.pri),
              const SizedBox(height: 14),
              const Text(
                'Sehat',
                style: TextStyle(
                  fontSize: 26,
                  fontWeight: FontWeight.w700,
                  color: Pal.ink,
                ),
              ),
              const SizedBox(height: 4),
              const Text(
                'Your health records, understood. Private by design.',
                style: TextStyle(fontSize: 12, color: Pal.mut),
              ),
              const Spacer(),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: Text(
                    _error!,
                    style: const TextStyle(color: Pal.bad, fontSize: 12),
                  ),
                ),
              FilledButton.icon(
                onPressed: _busy ? null : () => _run(id.google),
                icon: const Icon(Icons.g_mobiledata, size: 26),
                label: const Text('Continue with Google'),
              ),
              const SizedBox(height: 8),
              OutlinedButton.icon(
                onPressed: _busy ? null : () => _run(id.apple),
                icon: const Icon(Icons.apple),
                label: const Text('Continue with Apple'),
              ),
              if (const bool.fromEnvironment('SEHAT_TEST_LOGIN')) ...[
                const SizedBox(height: 8),
                TextButton(
                  onPressed: _busy ? null : () => _run(id.testLogin),
                  child: const Text('Test sign-in (dev builds only)'),
                ),
              ],
              const SizedBox(height: 14),
              const Text(
                'We never see your password. Health data stays encrypted and under your control.',
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 10, color: Pal.mut),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class CreateProfileScreen extends ConsumerStatefulWidget {
  const CreateProfileScreen({super.key});
  @override
  ConsumerState<CreateProfileScreen> createState() => _CreateProfileState();
}

class _CreateProfileState extends ConsumerState<CreateProfileScreen> {
  final _name = TextEditingController();
  DateTime? _dob;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  Future<void> _create() async {
    final n = _name.text.trim();
    if (n.isEmpty) {
      setState(() => _error = 'Enter a name.');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final p = await ref.read(apiProvider).post('/profiles', {
        'display_name': n,
      });
      if (_dob != null && p is Map && p['id'] != null) {
        await ref.read(apiProvider).patch('/profiles/${p['id']}', {
          'date_of_birth': _dob!.toIso8601String().substring(0, 10),
        });
      }
      ref.invalidate(profilesProvider);
    } catch (e) {
      if (mounted) setState(() => _error = friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    body: SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Spacer(),
            const Text(
              'Create your profile',
              style: TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 4),
            const Text(
              'You can add family members later.',
              style: TextStyle(fontSize: 12, color: Pal.mut),
            ),
            const SizedBox(height: 18),
            TextField(
              controller: _name,
              textCapitalization: TextCapitalization.words,
              decoration: InputDecoration(labelText: 'Name', errorText: _error),
            ),
            const SizedBox(height: 10),
            OutlinedButton(
              onPressed: () async {
                final d = await showDatePicker(
                  context: context,
                  initialDate: DateTime(1990),
                  firstDate: DateTime(1900),
                  lastDate: DateTime.now(),
                );
                if (d != null) setState(() => _dob = d);
              },
              child: Text(
                _dob == null
                    ? 'Date of birth (optional)'
                    : _dob!.toIso8601String().substring(0, 10),
              ),
            ),
            const SizedBox(height: 14),
            FilledButton(
              onPressed: _busy ? null : _create,
              child: const Text('Continue'),
            ),
            const Spacer(flex: 2),
            TextButton(
              onPressed: () => ref.read(authProvider.notifier).signOut(),
              child: const Text('Sign out'),
            ),
          ],
        ),
      ),
    ),
  );
}

const consentPurposes = [
  ('health_data', 'Store my health data', 'Required to use Sehat.', true),
  (
    'medical_record_storage',
    'Keep my uploaded records',
    'Store files securely in your vault.',
    false,
  ),
  (
    'document_processing',
    'Read my documents',
    'Extract lab values from reports.',
    false,
  ),
  (
    'ai_processing',
    'AI answers',
    'Answer questions using your records.',
    false,
  ),
  ('notifications', 'Reminders', 'Follow-up and checkup reminders.', false),
];

class ConsentScreen extends ConsumerStatefulWidget {
  const ConsentScreen({super.key});
  @override
  ConsumerState<ConsentScreen> createState() => _ConsentState();
}

class _ConsentState extends ConsumerState<ConsentScreen> {
  final _on = {for (final c in consentPurposes) c.$1: true};
  bool _busy = false;
  String? _error;

  Future<void> _save() async {
    final id = ref.read(profileIdProvider)!;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      for (final c in consentPurposes) {
        await ref.read(apiProvider).put('/profiles/$id/consents/${c.$1}', {
          'granted': _on[c.$1],
          'policy_version': '1.0',
        });
      }
      ref.invalidate(profileListProvider('/consents'));
    } catch (e) {
      if (mounted) setState(() => _error = friendly(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Your consent')),
    body: ListView(
      padding: const EdgeInsets.all(16),
      children: [
        const Text(
          'Choose what Sehat may do. Change this any time in Privacy.',
          style: TextStyle(fontSize: 12, color: Pal.mut),
        ),
        const SizedBox(height: 10),
        for (final c in consentPurposes)
          SCard(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
            child: SwitchListTile(
              contentPadding: EdgeInsets.zero,
              dense: true,
              title: Text(c.$2, style: const TextStyle(fontSize: 13)),
              subtitle: Text(c.$3, style: const TextStyle(fontSize: 11)),
              value: _on[c.$1]!,
              onChanged: c.$4 || _busy
                  ? null
                  : (v) => setState(() => _on[c.$1] = v),
            ),
          ),
        if (_error != null)
          Text(_error!, style: const TextStyle(color: Pal.bad)),
        const SizedBox(height: 8),
        FilledButton(
          onPressed: _busy ? null : _save,
          child: const Text('Agree and continue'),
        ),
      ],
    ),
  );
}
