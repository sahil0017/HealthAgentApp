import 'package:flutter/foundation.dart';

import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

class ApiError implements Exception {
  ApiError(this.status, this.code, this.message, [this.fields]);
  final int status;
  final String code;
  final String message;
  final Map<String, dynamic>? fields;
  bool get offline => status == 0;
  @override
  String toString() => message;
}

abstract class TokenStore {
  Future<String?> read();
  Future<void> write(String? token);
}

class SecureTokenStore implements TokenStore {
  static const _key = 'sehat.access_token';
  final _s = const FlutterSecureStorage();
  @override
  Future<String?> read() => _s.read(key: _key);
  @override
  Future<void> write(String? token) =>
      token == null ? _s.delete(key: _key) : _s.write(key: _key, value: token);
}

/// Web builds: flutter_secure_storage needs WebCrypto, which browsers only allow on HTTPS or localhost.
class WebTokenStore implements TokenStore {
  static const _key = 'sehat.access_token';
  @override
  Future<String?> read() async =>
      (await SharedPreferences.getInstance()).getString(_key);
  @override
  Future<void> write(String? token) async {
    final p = await SharedPreferences.getInstance();
    token == null ? await p.remove(_key) : await p.setString(_key, token);
  }
}

class MemoryTokenStore implements TokenStore {
  String? _t;
  @override
  Future<String?> read() async => _t;
  @override
  Future<void> write(String? token) async => _t = token;
}

typedef MultipartFile = http.MultipartFile;

/// Thin client over the Sehat API. Success is `{data, meta}`; errors are `{error:{code,message,fields}}`
/// or `{error:"snake_case"}`. Some endpoints return the bare object, so both shapes are accepted.
class ApiClient {
  ApiClient({
    required this.baseUrl,
    required this.tokens,
    http.Client? client,
    this.clientLabel = 'android',
    this.onUnauthorized,
  }) : _http = client ?? http.Client();

  final String baseUrl;
  final TokenStore tokens;
  final String clientLabel;
  final http.Client _http;
  void Function()? onUnauthorized;
  String? _token;

  /// True while the last request failed to reach the server.
  final ValueNotifier<bool> offline = ValueNotifier(false);

  Future<bool> restore() async {
    _token = await tokens.read();
    return _token != null;
  }

  bool get hasToken => _token != null;

  Future<void> setToken(String? t) async {
    _token = t;
    await tokens.write(t);
  }

  Uri _uri(String path) => Uri.parse('$baseUrl/api/v1$path');

  Map<String, String> get _headers => {
    'X-Cortex-Client': clientLabel,
    'Accept': 'application/json',
    if (_token != null) 'Authorization': 'Bearer $_token',
  };

  Future<dynamic> request(
    String method,
    String path, {
    Object? body,
    List<http.MultipartFile>? files,
    bool expectSession = true,
  }) async {
    http.Response res;
    try {
      if (files != null) {
        final req = http.MultipartRequest(method, _uri(path))
          ..headers.addAll(_headers)
          ..files.addAll(files);
        res = await http.Response.fromStream(
          await _http.send(req).timeout(const Duration(seconds: 90)),
        );
      } else {
        final h = {
          ..._headers,
          if (body != null) 'Content-Type': 'application/json',
        };
        final req = http.Request(method, _uri(path))..headers.addAll(h);
        if (body != null) req.body = jsonEncode(body);
        res = await http.Response.fromStream(
          await _http.send(req).timeout(const Duration(seconds: 30)),
        );
      }
    } on ApiError {
      rethrow;
    } catch (_) {
      offline.value = true;
      throw ApiError(
        0,
        'offline',
        'No connection. Check your network and try again.',
      );
    }
    offline.value = false;
    if (res.statusCode == 204 || res.bodyBytes.isEmpty) return null;
    dynamic payload;
    try {
      payload = jsonDecode(utf8.decode(res.bodyBytes));
    } catch (_) {}
    if (res.statusCode >= 400) {
      if (res.statusCode == 401 && expectSession) {
        await setToken(null);
        onUnauthorized?.call();
      }
      final e = payload is Map ? payload['error'] : null;
      if (e is String)
        throw ApiError(res.statusCode, e, e.replaceAll('_', ' '));
      if (e is Map)
        throw ApiError(
          res.statusCode,
          '${e['code'] ?? 'error'}',
          '${e['message'] ?? res.reasonPhrase}',
          (e['fields'] as Map?)?.cast<String, dynamic>(),
        );
      throw ApiError(
        res.statusCode,
        'error',
        res.reasonPhrase ?? 'Request failed',
      );
    }
    if (payload is Map && payload.containsKey('data')) return payload['data'];
    return payload;
  }

  Future<dynamic> get(String p) => request('GET', p);
  Future<dynamic> post(String p, [Object? b]) =>
      request('POST', p, body: b ?? {});
  Future<dynamic> put(String p, Object b) => request('PUT', p, body: b);
  Future<dynamic> patch(String p, Object b) => request('PATCH', p, body: b);
  Future<dynamic> delete(String p, [Object? b]) =>
      request('DELETE', p, body: b);

  Future<List<Map<String, dynamic>>> list(String p, {String? key}) async {
    var d = await get(p);
    if (d is Map && key != null) d = d[key];
    if (d is! List) return [];
    return d.whereType<Map>().map((e) => e.cast<String, dynamic>()).toList();
  }

  Future<Map<String, dynamic>> map(String p) async {
    final d = await get(p);
    return d is Map ? d.cast<String, dynamic>() : {};
  }

  Future<void> signIn(String provider, String idToken, String nonce) async {
    final d = await request(
      'POST',
      '/auth/session',
      body: {'provider': provider, 'id_token': idToken, 'nonce': nonce},
      expectSession: false,
    );
    final t = d is Map ? d['access_token'] : null;
    if (t is! String)
      throw ApiError(500, 'no_token', 'Sign-in failed. Please try again.');
    await setToken(t);
  }

  Future<void> signOut() async {
    try {
      await post('/auth/logout');
    } finally {
      await setToken(null);
    }
  }

  Future<void> signOutEverywhere() async {
    try {
      await post('/auth/logout-all');
    } finally {
      await setToken(null);
    }
  }

  Future<void> refresh() async {
    final d = await request('POST', '/auth/refresh', body: {});
    final t = d is Map ? d['access_token'] : null;
    if (t is String) await setToken(t);
  }
}
