import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart' show kIsWeb;

import 'package:crypto/crypto.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:google_sign_in/google_sign_in.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sign_in_with_apple/sign_in_with_apple.dart';

import 'api.dart';
import 'hardware.dart';

typedef Json = Map<String, dynamic>;

final apiProvider = Provider<ApiClient>(
  (ref) => throw UnimplementedError('override apiProvider'),
);
final locationProvider = Provider<LocationSource>((_) => PhoneLocation());
final healthProvider = Provider<HealthSource>((_) => PhoneHealth());
final scannerProvider = Provider<Scanner>((_) => CameraScanner());
final biometricProvider = Provider<Biometric>((_) => PhoneBiometric());
final identityProvider = Provider<IdentityService>(
  (ref) => IdentityService(ref.read(apiProvider)),
);
final prefsProvider = Provider<SharedPreferences?>((_) => null);

/// Obtains an ID token from the platform's sign-in UI, bound to a fresh nonce.
class IdentityService {
  IdentityService(this._api);
  final ApiClient _api;
  bool _googleReady = false;

  /// The name the sign-in provider gave us, used to create the profile without asking again.
  String? name;

  String _nonce() {
    final r = Random.secure();
    return base64UrlEncode(List.generate(24, (_) => r.nextInt(256)));
  }

  Future<void> google() async {
    final nonce = _nonce();
    final cfg = await _api.map('/auth/config');
    final providers = (cfg['providers'] as List? ?? []).cast<Json>();
    final id =
        providers.firstWhere(
              (p) => p['name'] == 'google',
              orElse: () => {},
            )['client_id']
            as String?;
    if (id == null)
      throw ApiError(
        400,
        'not_configured',
        'Google sign-in is not configured on the server.',
      );
    await GoogleSignIn.instance.initialize(
      clientId: kIsWeb ? id : null,
      serverClientId: kIsWeb ? null : id,
      nonce: nonce,
    );
    _googleReady = true;
    _nonceUsed = nonce;
    if (kIsWeb) return;
    final acct = await GoogleSignIn.instance.authenticate();
    name = acct.displayName;
    final token = acct.authentication.idToken;
    if (token == null)
      throw ApiError(401, 'no_token', 'Google did not return an ID token.');
    await _api.signIn('google', token, nonce);
  }

  String? _nonceUsed;

  /// Web only: prepares Google's rendered button, which hands back the account through [GoogleSignIn.authenticationEvents].
  Future<void> prepareWebGoogle() => google();

  Future<void> completeGoogle(GoogleSignInAccount acct) async {
    name = acct.displayName;
    final token = acct.authentication.idToken;
    if (token == null) {
      throw ApiError(401, 'no_token', 'Google did not return an ID token.');
    }
    await _api.signIn('google', token, _nonceUsed ?? '');
  }

  // Test-only: compiled in when built with --dart-define=SEHAT_TEST_LOGIN=true; the dev proxy mints a token for the test IdP.
  Future<void> testLogin() async {
    final r = await http.get(Uri.parse('${_api.baseUrl}/__testtoken'));
    final j = jsonDecode(r.body) as Map<String, dynamic>;
    await _api.signIn('oidc', j['id_token'] as String, j['nonce'] as String);
  }

  Future<void> apple() async {
    final nonce = _nonce();
    final hashed = sha256.convert(utf8.encode(nonce)).toString();
    final cred = await SignInWithApple.getAppleIDCredential(
      scopes: [
        AppleIDAuthorizationScopes.email,
        AppleIDAuthorizationScopes.fullName,
      ],
      nonce: hashed,
    );
    final token = cred.identityToken;
    if (token == null)
      throw ApiError(
        401,
        'no_token',
        'Apple did not return an identity token.',
      );
    name = [
      cred.givenName,
      cred.familyName,
    ].whereType<String>().where((p) => p.isNotEmpty).join(' ');
    await _api.signIn('apple', token, nonce);
  }

  Future<void> signOut() async {
    if (_googleReady) {
      try {
        await GoogleSignIn.instance.signOut();
      } catch (_) {}
    }
    await _api.signOut();
  }
}

enum AuthStatus { unknown, signedOut, signedIn }

class AuthNotifier extends Notifier<AuthStatus> {
  @override
  AuthStatus build() {
    final api = ref.read(apiProvider);
    api.onUnauthorized = () => state = AuthStatus.signedOut;
    return AuthStatus.unknown;
  }

  Future<void> restore() async {
    final has = await ref.read(apiProvider).restore();
    state = has ? AuthStatus.signedIn : AuthStatus.signedOut;
  }

  void signedIn() => state = AuthStatus.signedIn;

  Future<void> signOut() async {
    await ref.read(identityProvider).signOut();
    ref.read(activeProfileProvider.notifier).clear();
    state = AuthStatus.signedOut;
  }
}

final authProvider = NotifierProvider<AuthNotifier, AuthStatus>(
  AuthNotifier.new,
);

final profilesProvider = FutureProvider<List<Json>>((ref) {
  ref.watch(authProvider);
  if (ref.read(authProvider) != AuthStatus.signedIn) return [];
  return ref.read(apiProvider).list('/profiles');
});

class ActiveProfile extends Notifier<String?> {
  @override
  String? build() => null;
  void set(String id) => state = id;
  void clear() => state = null;
}

final activeProfileProvider = NotifierProvider<ActiveProfile, String?>(
  ActiveProfile.new,
);

/// The selected profile id, falling back to the first profile.
final profileIdProvider = Provider<String?>((ref) {
  final sel = ref.watch(activeProfileProvider);
  final list = ref.watch(profilesProvider).value;
  if (list == null || list.isEmpty) return null;
  if (sel != null && list.any((p) => p['id'] == sel)) return sel;
  return list.first['id'] as String;
});

/// Per-profile GET returning a JSON object. Family key is the path below /profiles/{id}.
final profileMapProvider = FutureProvider.family<Json, String>((ref, path) {
  final id = ref.watch(profileIdProvider);
  if (id == null) return <String, dynamic>{};
  return ref.read(apiProvider).map('/profiles/$id$path');
});

final profileListProvider = FutureProvider.family<List<Json>, String>((
  ref,
  path,
) {
  final id = ref.watch(profileIdProvider);
  if (id == null) return <Json>[];
  return ref.read(apiProvider).list('/profiles/$id$path');
});

/// Remembered coarse area, set after the user grants location.
class AreaNotifier extends Notifier<Area?> {
  static const _key = 'last_area';

  @override
  Area? build() {
    _restore();
    return null;
  }

  Future<void> _restore() async {
    try {
      final raw = (await SharedPreferences.getInstance()).getString(_key);
      if (raw == null || state != null) return;
      final m = jsonDecode(raw) as Map<String, dynamic>;
      state = Area(
        lat: (m['lat'] as num).toDouble(),
        lng: (m['lng'] as num).toDouble(),
        city: m['city'] as String?,
        region: m['region'] as String?,
        countryCode: m['cc'] as String?,
        country: m['country'] as String?,
      );
    } catch (_) {}
  }

  Future<void> _save(Area a) async {
    final p = await SharedPreferences.getInstance();
    await p.setString(
      _key,
      jsonEncode({
        'lat': a.lat,
        'lng': a.lng,
        'city': a.city,
        'region': a.region,
        'cc': a.countryCode,
        'country': a.country,
      }),
    );
  }

  Future<bool> detect() async {
    final a = await ref.read(locationProvider).currentArea();
    if (a != null) {
      state = a;
      _save(a);
    }
    return a != null;
  }

  void set(Area a) {
    state = a;
    _save(a);
  }
}

final areaProvider = NotifierProvider<AreaNotifier, Area?>(AreaNotifier.new);

String friendly(Object e) => e is ApiError
    ? (e.status == 0 ? 'You appear to be offline.' : e.message)
    : 'Something went wrong.';

/// The selected bottom tab, so a notification can jump to Records or Results.
class TabNotifier extends Notifier<int> {
  @override
  int build() => 0;
  void go(int i) => state = i;
}

final shellTabProvider = NotifierProvider<TabNotifier, int>(TabNotifier.new);
