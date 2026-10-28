import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qoimeks/api.dart';
import 'package:qoimeks/feedback.dart';
import 'package:qoimeks/settings.dart';
import 'package:qoimeks/settings_screen.dart';
import 'package:shared_preferences/shared_preferences.dart';

const _code = 'sEcr3t-code';

class FakeFeedbackServer {
  bool up = true;
  final List<Map<String, dynamic>> received = [];

  late final http.Client client = MockClient((req) async {
    if (!up) throw http.ClientException('offline');
    if (req.url.path == '/api/feedback') {
      received.add(jsonDecode(req.body) as Map<String, dynamic>);
      return http.Response('{"id":1}', 201);
    }
    return http.Response('{"error":"not found"}', 404);
  });
}

Future<DeviceInfo> _device() async =>
    (appVersion: '0.1.2+3', androidVersion: '14', device: 'Samsung A15');

FeedbackService make(AppSettings settings, FakeFeedbackServer server) => FeedbackService(
      settings: settings,
      api: () => ApiClient('http://server', accessCode: settings.accessCode, client: server.client),
      deviceInfo: _device,
      now: () => DateTime.utc(2026, 10, 28),
    );

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('the error log keeps the last 50 and never the access code', () async {
    final settings = AppSettings(accessCode: _code, operator: 'Айгуль');
    final f = make(settings, FakeFeedbackServer());
    for (var i = 0; i < 60; i++) {
      await f.logError('boom $i with $_code and Bearer $_code', where: 'screen $_code');
    }
    final log = await f.errors();
    expect(log, hasLength(maxLoggedErrors));
    expect(log.first['message'], startsWith('boom 10 '));
    expect(log.last['message'], startsWith('boom 59 '));
    final p = await SharedPreferences.getInstance();
    for (final key in p.getKeys()) {
      expect('${p.get(key)}', isNot(contains(_code)));
    }
    expect(log.first['message'], contains('[redacted]'));
    expect(log.first.keys, containsAll(['time', 'screen', 'message']));
  });

  test('a failed API call is logged by the client hook', () async {
    final settings = AppSettings(accessCode: _code);
    final server = FakeFeedbackServer();
    final f = make(settings, server);
    final api = ApiClient('http://server', client: server.client, onError: f.onApiError);
    server.up = false;
    await expectLater(api.catalog(), throwsA(isA<ApiException>()));
    await pumpEventQueue();
    expect(await f.errors(), hasLength(1));
  });

  test('with a failing API the report is kept and sent exactly once after recovery', () async {
    final settings = AppSettings(accessCode: _code, operator: 'Айгуль');
    final server = FakeFeedbackServer()..up = false;
    final f = make(settings, server);

    expect(await f.submit('не работает', from: 'operation'), FeedbackOutcome.savedForLater);
    expect(server.received, isEmpty);
    expect(await f.pending(), hasLength(1));

    await f.retryPending(); // still down: stays
    expect(await f.pending(), hasLength(1));

    server.up = true;
    await Future.wait([f.retryPending(), f.retryPending()]);
    await f.retryPending();
    expect(server.received, hasLength(1));
    expect(server.received.single['text'], 'не работает');
    expect(await f.pending(), isEmpty);
  });

  testWidgets('settings lead to the form; sending posts all metadata and the errors', (tester) async {
    final settings = AppSettings(serverUrl: 'http://server', accessCode: _code, operator: 'Айгуль');
    final server = FakeFeedbackServer();
    final f = make(settings, server);
    await f.logError('catalog failed', where: 'operation');

    await tester.pumpWidget(MaterialApp(home: SettingsScreen(settings: settings, feedback: f)));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byKey(const Key('report_problem')));
    await tester.tap(find.byKey(const Key('report_problem')));
    await tester.pumpAndSettle();

    expect(tester.widget<FilledButton>(find.byKey(const Key('report_send'))).onPressed, isNull);
    await tester.enterText(find.byKey(const Key('report_text')), 'Кнопка не нажимается');
    await tester.pump();
    await tester.tap(find.byKey(const Key('report_send')));
    await tester.pumpAndSettle();

    expect(server.received, hasLength(1));
    final r = server.received.single;
    expect(r['text'], 'Кнопка не нажимается');
    expect(r['voice_text'], '');
    expect(r['app_version'], '0.1.2+3');
    expect(r['android_version'], '14');
    expect(r['device'], 'Samsung A15');
    expect(r['operator'], 'Айгуль');
    expect(r['screen'], 'settings');
    expect((r['errors'] as List).single['message'], 'catalog failed');
    expect(find.byKey(const Key('report_text')), findsNothing);
  });

  testWidgets('offline: the form says it is saved for later', (tester) async {
    final settings = AppSettings(serverUrl: 'http://server', operator: 'Айгуль', lang: 'kk');
    final server = FakeFeedbackServer()..up = false;
    final f = make(settings, server);
    await tester.pumpWidget(MaterialApp(home: SettingsScreen(settings: settings, feedback: f)));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byKey(const Key('report_problem')));
    await tester.tap(find.byKey(const Key('report_problem')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('report_text')), 'сынды');
    await tester.pump();
    await tester.tap(find.byKey(const Key('report_send')));
    await tester.pumpAndSettle();
    expect(find.text(settings.s('report_saved')), findsOneWidget);
    expect(await f.pending(), hasLength(1));
  });
}
