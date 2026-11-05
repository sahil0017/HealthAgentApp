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
import 'package:sehat/features/environment.dart';
import 'package:sehat/features/find_care.dart';
import 'package:sehat/features/health.dart';
import 'package:sehat/features/lookup.dart';
import 'package:sehat/features/nearby.dart';
import 'package:sehat/features/more.dart';
import 'package:sehat/features/places.dart';
import 'package:sehat/features/notifications.dart';
import 'package:sehat/features/plan.dart';
import 'package:sehat/features/summary.dart';
import 'package:sehat/features/today.dart';
import 'package:sehat/features/prescriptions.dart';
import 'package:sehat/features/vault.dart';

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
  test(
    'hospitals matching the user records rank above a nearer general one',
    () {
      final ranked = rankHospitals(
        [
          {'name': 'Corner Nursing Home', 'distance_km': 1.0},
          {'name': 'City Heart Institute', 'distance_km': 4.0},
          {'name': 'Far Hospital', 'distance_km': 9.0, 'emergency': true},
        ],
        ['lipid profile'],
      );
      expect(ranked.first['name'], 'City Heart Institute');
      expect(ranked.first['reason'], 'Matches your lipid profile');
      expect(ranked.last['name'], 'Far Hospital');
    },
  );

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

  testWidgets('find care shows emergency, reach and hospital details', (
    t,
  ) async {
    final api = _api((r) {
      if (r.url.path.endsWith('/nearby/hospitals')) {
        return _json({
          'hospitals': [
            {
              'id': 'n/1',
              'name': 'City Hospital',
              'distance_km': 1.5,
              'emergency': false,
              'phone': '+91 11 5550',
            },
          ],
        });
      }
      if (r.url.path.endsWith('/nearby/places')) {
        return _json({'places': []});
      }
      return _json({});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const FindCareScreen()));
    await t.tap(find.text('Approximate only'));
    await t.pumpAndSettle();
    expect(find.text('Emergency'), findsOneWidget);
    expect(find.text('Call 112'), findsOneWidget);
    expect(find.text('Nearest hospital'), findsOneWidget);
    expect(find.text('Care within reach'), findsOneWidget);
    await t.tap(find.text('City Hospital').first);
    await t.pumpAndSettle();
    expect(find.text('Directions'), findsOneWidget);
    expect(find.text('Call'), findsOneWidget);
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

  testWidgets('confirming a report sends only the fields the server accepts', (
    t,
  ) async {
    Map<String, dynamic>? sent;
    final api = _api((r) {
      if (r.url.path.endsWith('/approve')) {
        sent = jsonDecode(r.body) as Map<String, dynamic>;
        return _json({'status': 'approved'});
      }
      if (r.url.path.contains('/documents/d1')) {
        return _json({
          'id': 'd1',
          'file_name': 'labs.pdf',
          'status': 'review_required',
          'extraction': {
            'biomarkers': [
              {
                'id': 'c1',
                'name': 'Hemoglobin',
                'value': 15,
                'unit': 'g/dL',
                'page': 2,
                'biomarker_code': 'HEMOGLOBIN',
                'reference_range': '13 - 17',
              },
            ],
          },
        });
      }
      return _json({});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const DocumentScreen('d1')));
    await t.pumpAndSettle();
    await t.tap(find.text('Confirm and save'));
    await t.pumpAndSettle();
    final row = (sent!['biomarkers'] as List).single as Map;
    expect(row.containsKey('id'), isFalse);
    expect(row['candidate_id'], 'c1');
    expect(row['biomarker_code'], 'HEMOGLOBIN');
  });

  testWidgets('a goal can be set for any catalog test, even with no results', (
    t,
  ) async {
    Map? body;
    final api = _api((r) {
      if (r.url.path.endsWith('/biomarkers')) {
        return _json([
          {
            'code': 'HBA1C',
            'display_name': 'HbA1c',
            'synonyms': ['glycated haemoglobin'],
            'supported_units': ['%', 'mmol/mol'],
          },
          {
            'code': 'LDL',
            'display_name': 'LDL cholesterol',
            'synonyms': [],
            'supported_units': ['mg/dL'],
          },
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
    await t.enterText(
      find.widgetWithText(TextField, 'Search for a test'),
      'hba',
    );
    await t.pumpAndSettle();
    expect(find.text('LDL cholesterol'), findsNothing);
    await t.tap(find.text('HbA1c'));
    await t.pumpAndSettle();
    await t.tap(find.text('mmol/mol'));
    await t.enterText(find.widgetWithText(TextField, 'Upper limit'), '39');
    await t.tap(find.text('Save goal'));
    await t.pumpAndSettle();
    expect(body?['biomarker_code'], 'HBA1C');
    expect(body?['unit'], 'mmol/mol');
    expect(body?['target_max'], 39);
    expect(body?.containsKey('target_min'), isFalse);
  });

  Json overview({List attention = const []}) => {
    'profile': {'display_name': 'Asha Rao'},
    'summary': {'open_follow_ups': 1, 'documents': 2, 'biomarkers_tracked': 5},
    'attention': attention,
    'goals': [],
    'recent_changes': [],
    'suggested_follow_ups': [],
    'reminders': [],
    'completeness': {'score': 50, 'items': []},
  };

  testWidgets('summary stays calm and puts urgent items in a strip', (t) async {
    final api = _api((r) {
      if (r.url.path.endsWith('/overview')) {
        return _json(
          overview(
            attention: [
              {
                'kind': 'overdue_follow_up',
                'severity': 'action',
                'title': 'Repeat HbA1c is overdue',
                'detail': 'Due 2026-10-01',
                'ref_id': 'f1',
              },
              {
                'kind': 'out_of_range',
                'severity': 'info',
                'title': 'LDL is above its reference range',
                'detail': '',
                'ref_id': 'l1',
              },
            ],
          ),
        );
      }
      if (r.url.path.endsWith('/follow-ups')) {
        return _json([
          {'id': 'f1', 'title': 'Repeat HbA1c', 'status': 'open'},
        ]);
      }
      return _json(<String, dynamic>{});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const Scaffold(body: TodayTab())));
    await t.pumpAndSettle();
    expect(find.text('Hello, Asha'), findsOneWidget);
    expect(find.text('Repeat HbA1c is overdue'), findsOneWidget);
    expect(find.text('+1 more'), findsOneWidget);
    expect(find.text('LDL is above its reference range'), findsNothing);
    expect(find.textContaining('1 follow-ups pending'), findsOneWidget);
  });

  testWidgets('notifications list everything and can be dismissed', (t) async {
    final api = _api((r) {
      if (r.url.path.endsWith('/overview')) {
        return _json(
          overview(
            attention: [
              {
                'kind': 'out_of_range',
                'severity': 'info',
                'title': 'LDL is above its reference range',
                'detail': 'Latest 150 mg/dL',
                'ref_id': 'l1',
              },
            ],
          ),
        );
      }
      return _json(<String, dynamic>{});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const NotificationsScreen()));
    await t.pumpAndSettle();
    expect(find.text('LDL is above its reference range'), findsOneWidget);
    await t.tap(find.byTooltip('Dismiss'));
    await t.pumpAndSettle();
    expect(find.text('You are all caught up'), findsOneWidget);
  });

  testWidgets('care plan lists an open follow-up that has no due date', (
    t,
  ) async {
    final api = _api((r) {
      if (r.url.path.endsWith('/follow-ups')) {
        return _json([
          {'id': 'f1', 'title': 'Book dental checkup', 'status': 'open'},
        ]);
      }
      return _json([]);
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const CarePlanScreen()));
    await t.pumpAndSettle();
    expect(find.text('Book dental checkup'), findsOneWidget);
    expect(find.text('No due date'), findsOneWidget);
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
    await t.tap(find.text('Approximate only'));
    await t.pumpAndSettle();
    expect(kind, 'doctor');
    expect(find.text('Dr. Rao Clinic'), findsOneWidget);
    await t.tap(find.text('Dr. Rao Clinic'));
    await t.pumpAndSettle();
    expect(find.text('Call'), findsOneWidget);
    expect(find.text('Directions'), findsOneWidget);
  });

  testWidgets('review shows evidence and puts unmatched rows first', (t) async {
    final api = _api((r) {
      if (r.url.path.contains('/documents/d1')) {
        return _json({
          'id': 'd1',
          'file_name': 'lab.pdf',
          'status': 'review_required',
          'extraction': {
            'biomarkers': [
              {
                'name': 'Glucose',
                'biomarker_code': 'GLUCOSE',
                'value': 5,
                'unit': 'mmol/L',
                'evidence': 'Glucose 5.0',
                'page': 2,
              },
              {'name': 'Mystery test', 'value': 1, 'unit': 'x'},
            ],
          },
        });
      }
      return _json({});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const DocumentScreen('d1')));
    await t.pumpAndSettle();
    expect(find.textContaining('page 2'), findsOneWidget);
    expect(find.textContaining('Not matched'), findsOneWidget);
    expect(
      t.getTopLeft(find.text('Mystery test')).dy <
          t.getTopLeft(find.text('Glucose')).dy,
      isTrue,
    );
  });

  testWidgets('lifestyle patterns are labelled correlation not cause', (
    t,
  ) async {
    final api = _api((r) {
      if (r.url.path.endsWith('/lifestyle/correlations')) {
        return _json({
          'correlations': [
            {
              'status': 'ok',
              'description': 'Sleep tracks with headache',
              'strength': 'moderate',
            },
          ],
        });
      }
      return _json(r.url.path.contains('categories') ? [] : {});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const LifestyleScreen()));
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.textContaining('Correlation, not cause'),
      200,
    );
    expect(find.text('Sleep tracks with headache'), findsOneWidget);
  });

  testWidgets('vitals tab lists readings with a typical range', (t) async {
    final api = _api((r) {
      if (r.url.path.endsWith('/device-readings/summary')) {
        return _json({
          'summaries': [
            {
              'name': 'Resting heart rate',
              'unit': 'bpm',
              'group': 'vitals',
              'latest_value': 64,
              'mean_7_days': 66,
              'typical_adult_range': {'low': 60, 'high': 100},
            },
            {
              'name': 'Steps',
              'unit': 'count',
              'group': 'activity',
              'latest_value': 5000,
            },
          ],
        });
      }
      return _json([]);
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const Scaffold(body: HealthTab())));
    await t.pumpAndSettle();
    await t.tap(find.text('Vitals'));
    await t.pumpAndSettle();
    expect(find.text('Resting heart rate'), findsOneWidget);
    expect(find.text('Steps'), findsNothing);
    expect(find.textContaining('typical 60'), findsOneWidget);
  });

  testWidgets('air and weather show the category and advice', (t) async {
    final api = _api((r) {
      if (r.url.path.endsWith('/environment/now')) {
        return _json({
          'air_category': 'Unhealthy',
          'air_advice': 'Reduce prolonged outdoor exertion.',
          'air': {
            'current': {'us_aqi': 160, 'pm2_5': 40, 'uv_index': 7},
            'hourly': {
              'time': [
                for (var h = 0; h < 24; h++)
                  '2026-11-04T${h.toString().padLeft(2, '0')}:00',
              ],
              'us_aqi': [for (var h = 0; h < 24; h++) 100 + h * 3],
            },
          },
          'uv_category': 'High',
          'weather': {
            'current': {'temperature_2m': 31, 'weather_code': 3, 'is_day': 1},
            'daily': {
              'temperature_2m_max': [33],
              'temperature_2m_min': [24],
              'sunrise': ['2026-11-04T06:30'],
              'sunset': ['2026-11-04T17:40'],
            },
            'hourly': {
              'time': [
                for (var h = 0; h < 24; h++)
                  '2026-11-04T${h.toString().padLeft(2, '0')}:00',
              ],
              'temperature_2m': [for (var h = 0; h < 24; h++) 24 + h % 9],
              'precipitation_probability': [for (var h = 0; h < 24; h++) h * 4],
            },
          },
        });
      }
      return _json({});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const EnvironmentScreen()));
    await t.tap(find.text('Approximate only'));
    await t.pumpAndSettle();
    expect(find.text('Unhealthy'), findsOneWidget);
    expect(find.text('160'), findsOneWidget);
    expect(find.text('Next 24 hours · air quality'), findsOneWidget);
    expect(find.text('Pollutants (µg/m³)'), findsOneWidget);
    expect(find.textContaining('Reduce prolonged'), findsOneWidget);
  });

  testWidgets('medicine lookup shows label text and the coverage note', (
    t,
  ) async {
    final api = _api((r) {
      if (r.url.path.endsWith('/medications/search')) {
        return _json({
          'medicines': [
            {'name': 'metformin 500 MG Oral Tablet', 'type': 'Generic product'},
          ],
          'coverage': 'US catalogue, not an Indian brand list.',
        });
      }
      if (r.url.path.endsWith('/medications/label')) {
        return _json({
          'sections': [
            {'title': 'Boxed warning', 'text': 'Lactic acidosis.'},
            {'title': 'Interactions', 'text': ''},
          ],
        });
      }
      return _json({});
    }, token: 'tok');
    await t.pumpWidget(_app(api, home: const MedicineScreen()));
    await t.enterText(find.byType(TextField), 'metformin');
    await t.testTextInput.receiveAction(TextInputAction.search);
    await t.pumpAndSettle();
    expect(find.text('Lactic acidosis.'), findsOneWidget);
    expect(find.text('Interactions'), findsNothing);
    expect(find.text('metformin 500 MG Oral Tablet'), findsOneWidget);
    expect(find.textContaining('not an Indian brand list'), findsOneWidget);
  });

  testWidgets('medicines today lets you tick a dose off', (t) async {
    var taken = false;
    Map<String, dynamic>? posted;
    final api = _api((r) {
      if (r.url.path.endsWith('/prescriptions/doses')) {
        posted = jsonDecode(r.body) as Map<String, dynamic>;
        taken = posted!['taken'] == true;
        return _json({'taken': taken});
      }
      if (r.url.path.endsWith('/prescriptions/schedule')) {
        return _json({
          'slots': [
            {
              'slot': 'morning',
              'label': 'Morning',
              'time': '23:59',
              'items': [
                {
                  'id': 'i1',
                  'name': 'Metformin',
                  'strength': '500 mg',
                  'taken': taken,
                },
              ],
            },
          ],
        });
      }
      return _json({});
    }, token: 'tok');
    await t.pumpWidget(
      _app(api, home: const Scaffold(body: MedicinesTodayCard())),
    );
    await t.pumpAndSettle();
    expect(find.text('0 of 1 taken'), findsOneWidget);
    await t.tap(find.text('Metformin 500 mg'));
    await t.pumpAndSettle();
    expect(posted?['item_id'], 'i1');
    expect(posted?['slot'], 'morning');
    expect(find.text('1 of 1 taken'), findsOneWidget);
  });

  testWidgets(
    'summary shows the wellbeing score and a plan that can be ticked',
    (t) async {
      final api = _api((r) {
        if (r.url.path.endsWith('/agent/wellbeing')) {
          return _json({
            'score': 72,
            'parts': [
              {'key': 'labs', 'score': 85, 'weight': 40},
              {'key': 'habits', 'score': 60, 'weight': 40},
            ],
            'habits': [
              {
                'code': 'sleep',
                'name': 'Sleep',
                'unit': 'h',
                'avg_7': 5.5,
                'baseline': 7.5,
                'direction': 'slipping',
                'projected_7d': 5.1,
                'daily': [
                  {'d': '2026-11-04', 'v': 5.0},
                  {'d': '2026-11-05', 'v': 6.0},
                ],
              },
            ],
            'tasks': [
              {
                'id': 'habit-sleep',
                'kind': 'habit',
                'title': 'Bring your sleep back toward 7.5 h',
                'why':
                    'Your 7-day average is 5.5 h, down from your usual 7.5 h.',
                'action': {'type': 'log', 'ref': 'sleep'},
              },
            ],
            'basis': 'Built from your own labs, habit logs and care plan.',
          });
        }
        return _json({});
      }, token: 'tok');
      await t.pumpWidget(
        _app(
          api,
          home: const Scaffold(
            body: SingleChildScrollView(
              child: Column(
                children: [WellbeingCard(), TodaysPlan(), HabitTrends()],
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('Wellbeing score'), findsOneWidget);
      expect(find.text('72'), findsOneWidget);
      expect(find.text('Lab results'), findsOneWidget);
      expect(find.text('0 of 1 done'), findsOneWidget);
      expect(find.text('Slipping'), findsOneWidget);
      expect(find.textContaining('Next wk'), findsOneWidget);
      await t.tap(find.byIcon(Icons.radio_button_unchecked));
      await t.pumpAndSettle();
      expect(find.text('1 of 1 done'), findsOneWidget);
    },
  );
}
