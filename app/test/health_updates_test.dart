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
