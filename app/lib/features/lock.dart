import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../core/state.dart';
import '../core/theme.dart';

const _lockKey = 'app_lock';

/// Whether the user asked for Face ID / fingerprint to open the app. Stored on this phone only.
final appLockEnabledProvider = FutureProvider<bool>((ref) async {
  final p = await SharedPreferences.getInstance();
  return p.getBool(_lockKey) ?? false;
});

Future<void> setAppLock(WidgetRef ref, bool on) async {
  final p = await SharedPreferences.getInstance();
  await p.setBool(_lockKey, on);
  ref.invalidate(appLockEnabledProvider);
}

/// Covers the app with an unlock screen on start and whenever it returns from the background.
class LockGate extends ConsumerStatefulWidget {
  const LockGate({super.key, required this.child});
  final Widget child;
  @override
  ConsumerState<LockGate> createState() => _LockGateState();
}

class _LockGateState extends ConsumerState<LockGate>
    with WidgetsBindingObserver {
  bool _locked = true;
  bool _authing = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState s) {
    if (s == AppLifecycleState.paused && !_authing) {
      setState(() => _locked = true);
    }
  }

  Future<void> _unlock() async {
    if (_authing) return;
    _authing = true;
    try {
      final ok = await ref.read(biometricProvider).confirm('Unlock Sehat');
      if (ok && mounted) setState(() => _locked = false);
    } catch (_) {
      // stay locked; the user can try again
    } finally {
      _authing = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final enabled = ref.watch(appLockEnabledProvider).asData?.value ?? false;
    if (!enabled || !_locked) return widget.child;
    WidgetsBinding.instance.addPostFrameCallback((_) => _unlock());
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.lock_outline, size: 40, color: Pal.pri),
            const SizedBox(height: 12),
            const Text('Sehat is locked', style: TextStyle(fontSize: 15)),
            const SizedBox(height: 12),
            FilledButton(onPressed: _unlock, child: const Text('Unlock')),
          ],
        ),
      ),
    );
  }
}
