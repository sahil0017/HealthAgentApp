import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'core/api.dart';
import 'core/state.dart';
import 'core/theme.dart';
import 'core/widgets.dart';
import 'features/agent.dart';
import 'features/auth.dart';
import 'features/health.dart';
import 'features/more.dart';
import 'features/today.dart';
import 'features/vault.dart';

const baseUrl = String.fromEnvironment(
  'SEHAT_API',
  defaultValue: 'http://localhost:8080',
);

String clientLabel() => kIsWeb
    ? 'web'
    : (defaultTargetPlatform == TargetPlatform.iOS ? 'ios' : 'android');

ApiClient buildApi() => ApiClient(
  baseUrl: baseUrl,
  tokens: SecureTokenStore(),
  clientLabel: clientLabel(),
);

class SehatApp extends StatelessWidget {
  const SehatApp({super.key});
  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'Sehat',
    debugShowCheckedModeBanner: false,
    theme: buildTheme(),
    home: const Root(),
  );
}

class Root extends ConsumerStatefulWidget {
  const Root({super.key});
  @override
  ConsumerState<Root> createState() => _RootState();
}

class _RootState extends ConsumerState<Root> {
  @override
  void initState() {
    super.initState();
    Future.microtask(() => ref.read(authProvider.notifier).restore());
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authProvider);
    return switch (auth) {
      AuthStatus.unknown => const Scaffold(
        body: Center(child: CircularProgressIndicator()),
      ),
      AuthStatus.signedOut => const SignInScreen(),
      AuthStatus.signedIn => const Gate(),
    };
  }
}

/// Routes a signed-in user to profile creation, consent, or the main app.
class Gate extends ConsumerWidget {
  const Gate({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final profiles = ref.watch(profilesProvider);
    return profiles.when(
      loading: () =>
          const Scaffold(body: Center(child: CircularProgressIndicator())),
      error: (e, _) => Scaffold(
        body: ErrorView(e, onRetry: () => ref.invalidate(profilesProvider)),
      ),
      data: (list) {
        if (list.isEmpty) return const CreateProfileScreen();
        final id = ref.watch(profileIdProvider)!;
        final consents = ref.watch(profileListProvider('/consents'));
        return consents.when(
          loading: () =>
              const Scaffold(body: Center(child: CircularProgressIndicator())),
          error: (e, _) => Scaffold(
            body: ErrorView(
              e,
              onRetry: () => ref.invalidate(profileListProvider('/consents')),
            ),
          ),
          data: (cs) =>
              cs.any(
                (c) => c['purpose'] == 'health_data' && c['granted'] == true,
              )
              ? Shell(key: ValueKey(id))
              : const ConsentScreen(),
        );
      },
    );
  }
}

class Shell extends ConsumerStatefulWidget {
  const Shell({super.key});
  @override
  ConsumerState<Shell> createState() => _ShellState();
}

class _ShellState extends ConsumerState<Shell> {
  int _i = 0;
  static const _tabs = [
    (Icons.today_outlined, Icons.today, 'Today'),
    (Icons.folder_outlined, Icons.folder, 'Vault'),
    (Icons.monitor_heart_outlined, Icons.monitor_heart, 'Health'),
    (Icons.auto_awesome_outlined, Icons.auto_awesome, 'Agent'),
    (Icons.grid_view_outlined, Icons.grid_view_rounded, 'More'),
  ];

  @override
  Widget build(BuildContext context) {
    final profiles = ref.watch(profilesProvider).value ?? [];
    final id = ref.watch(profileIdProvider);
    final name =
        profiles.firstWhere(
          (p) => p['id'] == id,
          orElse: () => {},
        )['display_name'] ??
        '';
    return Scaffold(
      appBar: AppBar(
        title: Text(_tabs[_i].$3),
        actions: [
          if (profiles.length > 1)
            PopupMenuButton<String>(
              tooltip: 'Switch profile',
              onSelected: (v) =>
                  ref.read(activeProfileProvider.notifier).set(v),
              itemBuilder: (_) => [
                for (final p in profiles)
                  PopupMenuItem(
                    value: p['id'] as String,
                    child: Text('${p['display_name']}'),
                  ),
              ],
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 12),
                child: Pill('$name  ▾'),
              ),
            )
          else
            Padding(
              padding: const EdgeInsets.only(right: 12),
              child: Pill('$name'),
            ),
        ],
      ),
      body: IndexedStack(
        index: _i,
        children: const [
          TodayTab(),
          VaultTab(),
          HealthTab(),
          AgentTab(),
          MoreTab(),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _i,
        height: 62,
        labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
        onDestinationSelected: (v) => setState(() => _i = v),
        destinations: [
          for (final t in _tabs)
            NavigationDestination(
              icon: Icon(t.$1),
              selectedIcon: Icon(t.$2),
              label: t.$3,
            ),
        ],
      ),
    );
  }
}
