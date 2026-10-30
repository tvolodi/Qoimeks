import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qoimeks/api.dart';
import 'package:qoimeks/l10n.dart';
import 'package:qoimeks/qr_scanner.dart';
import 'package:qoimeks/settings.dart';
import 'package:qoimeks/settings_screen.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _Scanner implements QrScanner {
  _Scanner(this.result);
  final ScanResult result;
  @override
  Future<ScanResult> scan(BuildContext context, Strings s) async => result;
}

http.Response _json(int status, Object body) => http.Response.bytes(
  utf8.encode(jsonEncode(body)),
  status,
  headers: {'content-type': 'application/json'},
);

const _key = 'qdk_FAKEKEY123';

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  late List<http.Request> requests;
  late AppSettings settings;

  Future<void> pump(
    WidgetTester tester, {
    required Future<http.Response> Function(http.Request) handler,
    String lang = 'ru',
    String server = 'https://srv.example',
    ScanResult scan = const ScanCancelled(),
  }) async {
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    requests = [];
    settings = AppSettings(
      serverUrl: server,
      accessCode: 'oldcode',
      operator: 'Айгуль',
      lang: lang,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          settings: settings,
          scanner: _Scanner(scan),
          deviceInfo: () async => (
            appVersion: '0.2.0+3',
            androidVersion: '14',
            device: 'Samsung A15',
          ),
          pairClient: (url) => ApiClient(
            url,
            client: MockClient((req) async {
              requests.add(req);
              return handler(req);
            }),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  Future<void> connect(WidgetTester tester, String typed) async {
    await tester.ensureVisible(find.byKey(const Key('pair_code')));
    await tester.enterText(find.byKey(const Key('pair_code')), typed);
    await tester.ensureVisible(find.byKey(const Key('pair_connect')));
    await tester.tap(find.byKey(const Key('pair_connect')));
    await tester.pumpAndSettle();
  }

  Future<http.Response> ok(http.Request r) async => _json(200, {
    'device_id': 'd-1',
    'device_key': _key,
    'label': 'Pairing name',
    'tenant': {'name': 'Demo', 'language': 'ru'},
  });

  testWidgets('typed code is upper-cased, exchanged, key saved and masked', (
    tester,
  ) async {
    await pump(tester, handler: ok);
    await connect(tester, 'k7m2-9qxa');
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('pair_code')))
          .controller!
          .text,
      '',
    );
    expect(requests, hasLength(1));
    expect(
      requests.single.url.toString(),
      'https://srv.example/pair/v1/exchange',
    );
    final body = jsonDecode(requests.single.body) as Map;
    expect(body['code'], 'K7M2-9QXA');
    expect(body['device_label'], 'Samsung A15');
    expect(body['operator'], 'Айгуль');
    expect(settings.accessCode, _key);
    expect(settings.serverUrl, 'https://srv.example');
    expect(settings.operator, 'Айгуль');
    final field = tester.widget<EditableText>(
      find.descendant(
        of: find.byKey(const Key('access_code')),
        matching: find.byType(EditableText),
      ),
    );
    expect(field.obscureText, isTrue);
    expect(find.text('Подключено как «Pairing name»'), findsOneWidget);
    expect(
      find.byWidgetPredicate((w) => w is Text && (w.data ?? '').contains(_key)),
      findsNothing,
    );
  });

  testWidgets('the code field upper-cases as the user types', (tester) async {
    await pump(tester, handler: ok);
    await tester.enterText(find.byKey(const Key('pair_code')), 'ab12 cd');
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('pair_code')))
          .controller!
          .text,
      'AB12CD',
    );
  });

  for (final lang in ['ru', 'kk']) {
    final errors = {
      (400, 'invalid_pairing_code'): 'pair_err_code',
      (429, 'rate_limited'): 'pair_err_wait',
      (403, 'tenant_unavailable'): 'pair_err_tenant',
    };
    errors.forEach((status, key) {
      testWidgets('server ${status.$1} shows a localized error ($lang)', (
        tester,
      ) async {
        await pump(
          tester,
          lang: lang,
          handler: (r) async => _json(status.$1, {'error': status.$2}),
        );
        await connect(tester, 'AAAA-BBBB');
        expect(find.text(Strings(lang)(key)), findsOneWidget);
        expect(settings.accessCode, 'oldcode');
      });
    });

    testWidgets('no network shows a localized error, nothing changes ($lang)', (
      tester,
    ) async {
      await pump(
        tester,
        lang: lang,
        handler: (r) async => throw Exception('offline'),
      );
      await connect(tester, 'AAAA-BBBB');
      expect(find.text(Strings(lang)('pair_err_network')), findsOneWidget);
      expect(settings.accessCode, 'oldcode');
      expect(find.byKey(const Key('pair_status')), findsNothing);
    });
  }

  testWidgets('an empty code is not sent', (tester) async {
    await pump(tester, handler: ok);
    await connect(tester, '');
    expect(requests, isEmpty);
    expect(find.byKey(const Key('pair_error')), findsOneWidget);
  });

  testWidgets('a pairing QR exchanges at once and saves', (tester) async {
    await pump(
      tester,
      handler: ok,
      server: 'http://old',
      scan: const ScanText(
        'qoimeks://pair?url=https%3A%2F%2Fnew.example&pair=K7M2-9QXA',
      ),
    );
    await tester.ensureVisible(find.byKey(const Key('scan_qr')));
    await tester.tap(find.byKey(const Key('scan_qr')));
    await tester.pumpAndSettle();
    expect(
      requests.single.url.toString(),
      'https://new.example/pair/v1/exchange',
    );
    expect(settings.serverUrl, 'https://new.example');
    expect(settings.accessCode, _key);
  });

  testWidgets('plain http address shows a warning', (tester) async {
    await pump(tester, handler: ok, server: 'http://192.168.1.10:8080');
    expect(find.byKey(const Key('pair_insecure')), findsOneWidget);
  });

  testWidgets('https address shows no warning', (tester) async {
    await pump(tester, handler: ok);
    expect(find.byKey(const Key('pair_insecure')), findsNothing);
  });

  test('pairing strings exist in ru and kk', () {
    for (final k in [
      'pair_title',
      'pair_code',
      'pair_connect',
      'pair_status',
      'pair_status_plain',
      'pair_insecure',
      'pair_err_code',
      'pair_err_wait',
      'pair_err_tenant',
      'pair_err_network',
      'pair_err_server',
      'device_revoked',
      'pair_again',
    ]) {
      expect(const Strings('ru')(k), isNot(k));
      expect(const Strings('kk')(k), isNot(k));
      expect(const Strings('kk')(k), isNot(const Strings('ru')(k)));
    }
  });

  group('revoked device banner', () {
    test('401 device_revoked is recognised from the response', () async {
      final api = ApiClient(
        'http://s',
        client: MockClient(
          (r) async => _json(401, {
            'error': 'device revoked',
            'reason': 'device_revoked',
          }),
        ),
      );
      await expectLater(
        api.catalog(),
        throwsA(
          isA<ApiException>().having(
            (e) => e.isDeviceRevoked,
            'revoked',
            isTrue,
          ),
        ),
      );
      final plain = ApiClient(
        'http://s',
        client: MockClient((r) async => _json(401, {'error': 'unauthorized'})),
      );
      await expectLater(
        plain.catalog(),
        throwsA(
          isA<ApiException>().having(
            (e) => e.isDeviceRevoked,
            'revoked',
            isFalse,
          ),
        ),
      );
    });

    testWidgets('shown without a dismiss button, gone after pairing again', (
      tester,
    ) async {
      await pump(tester, handler: ok);
      expect(find.byKey(const Key('device_revoked_banner')), findsNothing);
      settings.markDeviceRevoked();
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('device_revoked_banner')), findsOneWidget);
      expect(find.text(const Strings('ru')('device_revoked')), findsOneWidget);
      expect(find.byIcon(Icons.close), findsNothing);
      await connect(tester, 'K7M2-9QXA');
      expect(find.byKey(const Key('device_revoked_banner')), findsNothing);
      expect(settings.deviceRevoked, isFalse);
    });

    test('a revoked 401 gets its own message in both languages', () {
      const e = ApiException(401, 'device revoked', reason: 'device_revoked');
      for (final l in ['ru', 'kk']) {
        expect(Strings(l).error(e), Strings(l)('device_revoked'));
        expect(
          Strings(l).error(const ApiException(401, 'x')),
          Strings(l)('err_access'),
        );
      }
    });
  });
}
