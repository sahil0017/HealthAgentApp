import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:sehat/app.dart';
import 'package:sehat/core/api.dart';
import 'package:sehat/core/hardware.dart';
import 'package:sehat/core/state.dart';
import 'package:sehat/features/more.dart';

class _FakeLocation implements LocationSource {
  @override
  Future<Area?> currentArea() async => const Area(
    lat: 28.6,
    lng: 77.2,
    city: 'New Delhi',
    region: 'Delhi',
    countryCode: 'IN',
    country: 'India',
  );
}

http.Response _json(Object data, [int status = 200]) => http.Response(
  jsonEncode({'data': data, 'meta': {}}),
  status,
  headers: {'content-type': 'application/json'},
);

ApiClient _api(http.Response Function(http.Request) route, {String? token}) {
  final store = MemoryTokenStore();
  if (token != null) store.write(token);
  return ApiClient(
    baseUrl: 'http://test',
    tokens: store,
    client: MockClient((r) async => route(r)),
    clientLabel: 'android',
  );
}

Widget _app(ApiClient api, {Widget? home}) => ProviderScope(
  overrides: [
    apiProvider.overrideWithValue(api),
    locationProvider.overrideWithValue(_FakeLocation()),
  ],
  child: home == null ? const SehatApp() : MaterialApp(home: home),
);

void main() {
  testWidgets('signed out shows sign-in', (t) async {
    await t.pumpWidget(_app(_api((_) => _json({}))));
    await t.pumpAndSettle();
    expect(find.text('Continue with Google'), findsOneWidget);
  });

  testWidgets('signed in without a profile asks to create one', (t) async {
    final api = _api((r) => _json([]), token: 'tok');
    await t.pumpWidget(_app(api));
    await t.pumpAndSettle();
    expect(find.text('Create your profile'), findsOneWidget);
  });

  testWidgets('nearby shows alerts and hospitals for the detected area', (
    t,
  ) async {
    String? country;
    final api = _api((r) {
      if (r.url.path.endsWith('/nearby/alerts')) {
        country = r.url.queryParameters['country'];
        return _json({
          'alerts': [
            {
              'id': 'a',
              'title': 'Measles - India',
              'summary': 'Cases rising',
              'published_at': '2026-10-01T00:00:00Z',
              'url': 'https://example.org/a',
              'source': 'WHO',
              'in_country': true,
            },
          ],
        });
      }
      if (r.url.path.endsWith('/nearby/hospitals')) {
        return _json({
          'hospitals': [
            {
              'id': 'n/1',
              'name': 'City Hospital',
              'distance_km': 1.5,
              'emergency': true,
            },
          ],
        });
      }
      return _json({});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const NearbyScreen()));
    await t.tap(find.text('Use my location'));
    await t.pumpAndSettle();
    expect(country, 'India');
    expect(find.text('Measles - India'), findsOneWidget);
    expect(find.text('Your country'), findsOneWidget);
    expect(find.text('City Hospital'), findsOneWidget);
    expect(find.textContaining('none is connected'), findsOneWidget);
  });
}
