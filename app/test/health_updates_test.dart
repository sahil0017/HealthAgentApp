import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:sehat/core/api.dart';
import 'package:sehat/core/state.dart';
import 'package:sehat/features/dashboard.dart';
import 'package:sehat/features/health.dart';
import 'package:sehat/features/investigations.dart';
import 'package:sehat/features/records.dart';
import 'package:sehat/features/summary.dart';
import 'package:sehat/features/today.dart';

http.Response response(Object data) => http.Response(
  jsonEncode({'data': data, 'meta': {}}),
  200,
  headers: {'content-type': 'application/json'},
);

Widget app(Widget child, http.Response Function(http.Request) route) {
  final api = ApiClient(
    baseUrl: 'http://test',
    tokens: MemoryTokenStore(),
    client: MockClient((r) async => route(r)),
    clientLabel: 'android',
  );
  return ProviderScope(
    overrides: [
      apiProvider.overrideWithValue(api),
      profileIdProvider.overrideWithValue('p1'),
    ],
    child: MaterialApp(home: child),
  );
}

void main() {
  testWidgets('wellbeing explains its components and missing data', (t) async {
    await t.pumpWidget(
      app(const Scaffold(body: SingleChildScrollView(child: WellbeingCard())), (
        r,
      ) {
        return response({
          'score': 75,
          'parts': [
            {'key': 'labs', 'score': 75, 'weight': 40},
          ],
          'labs': {'in_range': 3, 'out_of_range': 1, 'unrated': 2},
        });
      }),
    );
    await t.pumpAndSettle();
    expect(find.text('75'), findsOneWidget);
    expect(find.textContaining('Based on 1 of 3 areas'), findsOneWidget);
    expect(
      find.text(
        '3 of 4 results with ranges are in range; 2 without a range not scored.',
      ),
      findsOneWidget,
    );
    expect(find.textContaining('Not included: habit history'), findsOneWidget);
    expect(find.text('Log habits'), findsOneWidget);
  });

  testWidgets('wellbeing shows a retry when its data is unavailable', (
    t,
  ) async {
    var unavailable = true;
    await t.pumpWidget(
      app(const Scaffold(body: WellbeingCard()), (r) {
        if (unavailable) return http.Response('unavailable', 503);
        return response({'score': 0, 'parts': []});
      }),
    );
    await t.pump();
    await t.pump(const Duration(milliseconds: 50));
    expect(find.text('Your wellbeing score could not load.'), findsOneWidget);
    unavailable = false;
    await t.tap(find.text('Retry'));
    await t.pumpAndSettle();
    expect(find.textContaining('No score yet.'), findsOneWidget);
    expect(find.text('0'), findsNothing);
  });

  testWidgets('summary shows out of range and unrated results', (t) async {
    await t.pumpWidget(
      app(const Scaffold(body: TodayTab()), (r) {
        if (r.url.path.endsWith('/overview')) {
          return response({
            'profile': {'display_name': 'Test'},
            'summary': {},
            'goals': [],
            'completeness': {'score': 30, 'items': []},
          });
        }
        if (r.url.path.endsWith('/biomarkers/summary')) {
          return response([
            {
              'display_name': 'Glucose',
              'latest': {'flag': 'above_range'},
            },
            {
              'display_name': 'Other',
              'latest': {'flag': 'unknown'},
            },
          ]);
        }
        if (r.url.path.endsWith('/agent/wellbeing')) {
          return response({'parts': []});
        }
        return response([]);
      }),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(find.textContaining('Review Glucose'), 250);
    expect(
      find.textContaining(
        '1 result outside the reference range. Review Glucose.',
      ),
      findsOneWidget,
    );
    expect(find.text('1 without a reference range.'), findsOneWidget);
    expect(find.text('Record complete'), findsNothing);
  });

  testWidgets('lab results retains report and test views without add result', (
    t,
  ) async {
    await t.pumpWidget(
      app(const Scaffold(body: HealthTab()), (r) => response([])),
    );
    await t.pumpAndSettle();
    expect(find.text('Add a result'), findsNothing);
    expect(find.text('No reports with results yet'), findsOneWidget);
    await t.tap(find.text('By test'));
    await t.pumpAndSettle();
    expect(find.text('Add a result'), findsNothing);
    expect(find.text('By report'), findsOneWidget);
  });

  testWidgets('changes use only the newest report and display both dates', (
    t,
  ) async {
    await t.pumpWidget(
      app(const Scaffold(body: WhatChanged()), (r) {
        return response([
          {
            'results': [
              {
                'display_name': 'Glucose',
                'biomarker_code': 'GLU',
                'previous_value': 100,
                'value': 120,
                'unit': 'mg/dL',
                'flag': 'above_range',
                'previous_observed_at': '2024-01-01',
                'observed_at': '2025-01-01',
              },
            ],
          },
          {
            'results': [
              {'display_name': 'Old result', 'previous_value': 1, 'value': 2},
            ],
          },
        ]);
      }),
    );
    await t.pumpAndSettle();
    expect(find.text('Glucose'), findsOneWidget);
    expect(find.text('2024-01-01 → 2025-01-01'), findsOneWidget);
    expect(find.text('Old result'), findsNothing);
  });

  testWidgets('history fetches older pages of verified reports', (t) async {
    await t.pumpWidget(
      app(const HistoryScreen(), (r) {
        if (r.url.path.endsWith('/history')) return response([]);
        final offset = r.url.queryParameters['offset'];
        if (offset == '0') {
          return response(
            List.generate(
              100,
              (i) => {
                'id': '$i',
                'file_name': 'Report $i',
                'status': 'processing',
              },
            ),
          );
        }
        return response([
          {
            'id': 'old',
            'file_name': 'Old verified report',
            'status': 'approved',
          },
        ]);
      }),
    );
    await t.pumpAndSettle();
    expect(find.text('Old verified report'), findsOneWidget);
  });

  testWidgets('zero-source Copilot response is labelled uncited', (t) async {
    var requests = 0;
    await t.pumpWidget(
      app(const InvestigationScreen('i1'), (r) {
        if (r.url.path.endsWith('/general-answer')) {
          requests++;
          return response({
            'answer': 'General information.',
            'source': 'copilot',
          });
        }
        return response({
          'investigation': {'question': 'What is vitamin D?'},
          'items': [],
        });
      }),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('Ask Copilot'));
    await t.pumpAndSettle();
    expect(requests, 1);
    expect(find.text('Copilot · uncited general information'), findsOneWidget);
    expect(find.text('General information.'), findsOneWidget);
  });
}
