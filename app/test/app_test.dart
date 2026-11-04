import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sehat/app.dart';
import 'package:sehat/core/api.dart';
import 'package:sehat/core/hardware.dart';
import 'package:sehat/core/state.dart';
import 'package:sehat/features/agent_screens.dart';
import 'package:sehat/features/more.dart';
import 'package:sehat/features/places.dart';
import 'package:sehat/features/plan.dart';

class _FakeLocation implements LocationSource {
  @override
  Future<({double lat, double lng})?> preciseNow() async =>
      (lat: 28.6123, lng: 77.2091);

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
    profileIdProvider.overrideWithValue('p1'),
  ],
  child: home == null ? const SehatApp() : MaterialApp(home: home),
);

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

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
    expect(
      find.textContaining('not verified against a medical registry'),
      findsOneWidget,
    );
  });

  testWidgets('approving a suggestion calls the approve endpoint', (t) async {
    var approved = false;
    final api = _api((r) {
      if (r.url.path.endsWith('/agent/actions') && r.method == 'GET') {
        return _json({
          'actions': approved
              ? []
              : [
                  {
                    'id': 'x1',
                    'kind': 'follow_up',
                    'title': 'Discuss Glucose with your clinician',
                    'reason': 'Glucose is above range',
                    'status': 'proposed',
                  },
                ],
        });
      }
      if (r.url.path.endsWith('/agent/actions/x1/approve')) {
        approved = true;
        return _json({'action': {}, 'follow_up_id': 'f1'});
      }
      return _json({});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const ApprovalsScreen()));
    await t.pumpAndSettle();
    expect(find.text('Discuss Glucose with your clinician'), findsOneWidget);
    await t.tap(find.text('Approve'));
    await t.pumpAndSettle();
    expect(approved, isTrue);
    expect(find.text('Nothing to approve'), findsOneWidget);
  });

  testWidgets('a goal is created for a chosen biomarker', (t) async {
    Map? body;
    final api = _api((r) {
      if (r.url.path.endsWith('/biomarkers/summary')) {
        return _json([
          {'biomarker_code': 'HBA1C', 'display_name': 'HbA1c', 'unit': '%'},
        ]);
      }
      if (r.url.path.endsWith('/goals') && r.method == 'POST') {
        body = jsonDecode(r.body) as Map;
        return _json({'id': 'g1'}, 201);
      }
      return _json([]);
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const GoalForm()));
    await t.pumpAndSettle();
    await t.tap(find.byType(DropdownButtonFormField<Json>));
    await t.pumpAndSettle();
    await t.tap(find.text('HbA1c (%)').last);
    await t.pumpAndSettle();
    await t.enterText(find.widgetWithText(TextField, 'At most'), '5.7');
    await t.tap(find.text('Save goal'));
    await t.pumpAndSettle();
    expect(body?['biomarker_code'], 'HBA1C');
    expect(body?['target_max'], 5.7);
    expect(body?.containsKey('target_min'), isFalse);
  });

  testWidgets('emergency shows the local number and medical id', (t) async {
    final api = _api((r) {
      if (r.url.path.endsWith('/history')) {
        return _json([
          {'kind': 'allergy', 'title': 'Penicillin', 'status': 'active'},
        ]);
      }
      if (r.url.path.endsWith('/nearby/hospitals')) {
        return _json({
          'hospitals': [
            {
              'name': 'City ER',
              'distance_km': 2.4,
              'emergency': true,
              'lat': 1,
              'lng': 1,
            },
          ],
        });
      }
      return _json({'blood_group': 'O+'});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const EmergencyScreen()));
    await t.pumpAndSettle();
    expect(find.textContaining('Call ambulance (112)'), findsOneWidget);
    expect(find.textContaining('Penicillin'), findsOneWidget);
    expect(find.textContaining('O+'), findsOneWidget);
  });

  testWidgets('care near you lists places for the detected area', (t) async {
    String? kind;
    final api = _api((r) {
      if (r.url.path.endsWith('/nearby/places')) {
        kind = r.url.queryParameters['kind'];
        return _json({
          'places': [
            {
              'id': 'n/1',
              'kind': 'doctor',
              'name': 'Dr. Rao Clinic',
              'speciality': 'Endocrinology',
              'distance_km': 3.2,
              'lat': 28.6,
              'lng': 77.2,
              'phone': '+91 20 1234',
            },
          ],
          'note': 'Not verified against a medical registry.',
        });
      }
      return _json({});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const DoctorsScreen()));
    await t.tap(find.text('Allow approximate location'));
    await t.pumpAndSettle();
    expect(kind, 'doctor');
    expect(find.text('Dr. Rao Clinic'), findsOneWidget);
    await t.tap(find.text('Dr. Rao Clinic'));
    await t.pumpAndSettle();
    expect(find.text('Call'), findsOneWidget);
    expect(find.text('Directions'), findsOneWidget);
  });
}
