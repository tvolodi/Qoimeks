import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qoimeks/api.dart';
import 'package:qoimeks/code_find.dart';
import 'package:qoimeks/l10n.dart';
import 'package:qoimeks/qr_scanner.dart';
import 'package:qoimeks/settings.dart';

class FakeScanner implements QrScanner {
  FakeScanner(this.result);
  final ScanResult result;

  @override
  Future<ScanResult> scan(BuildContext context, Strings s) async => result;
}

const _bolt = {
  'id': 'c1',
  'object_type': 'sku',
  'object_id': 1,
  'object_code': 'NUT-M8',
  'object_name': 'Гайка М8',
  'code_type': 'ean13',
  'code': '4601234567890',
};

http.Response _json(Object body, int status) => http.Response.bytes(
  utf8.encode(jsonEncode(body)),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

/// Answers lookup for [_bolt] only; records the codes asked for.
ApiClient _api(List<String> asked, {bool down = false}) => ApiClient(
  'http://server',
  accessCode: 'code-1',
  client: MockClient((req) async {
    if (down) throw http.ClientException('no route');
    final code = req.url.queryParameters['code']!;
    asked.add(code);
    if (code == '4601234567890') return _json(_bolt, 200);
    return _json({
      'error': 'not found',
      'reason': 'code_not_found',
      'code': code,
    }, 404);
  }),
);

void main() {
  group('ApiClient.lookupCode', () {
    test('returns the object, encodes the code', () async {
      final asked = <String>[];
      final c = await _api(asked).lookupCode('4601234567890');
      expect(c!.objectType, 'sku');
      expect(c.objectId, 1);
      expect(c.objectName, 'Гайка М8');
    });

    test('unknown code is null, other failures throw', () async {
      expect(await _api([]).lookupCode('nope'), isNull);
      expect(
        _api([], down: true).lookupCode('x'),
        throwsA(isA<ApiException>()),
      );
    });
  });

  group('CodeFindScreen', () {
    Future<CodeInfo?> Function() open(
      WidgetTester tester,
      ScanResult scan,
      List<String> asked, {
      String lang = 'ru',
      bool down = false,
    }) {
      CodeInfo? result;
      var done = false;
      return () async {
        await tester.pumpWidget(
          MaterialApp(
            home: Builder(
              builder: (context) => TextButton(
                onPressed: () async {
                  result = await Navigator.of(context).push<CodeInfo>(
                    MaterialPageRoute(
                      builder: (_) => CodeFindScreen(
                        settings: AppSettings(
                          serverUrl: 'http://server',
                          lang: lang,
                        ),
                        api: _api(asked, down: down),
                        scanner: FakeScanner(scan),
                      ),
                    ),
                  );
                  done = true;
                },
                child: const Text('open'),
              ),
            ),
          ),
        );
        await tester.tap(find.text('open'));
        await tester.pumpAndSettle();
        expect(done, isFalse);
        return result;
      };
    }

    testWidgets('a scanned known code returns the item', (tester) async {
      final asked = <String>[];
      await open(tester, const ScanText('4601234567890'), asked)();
      await tester.tap(find.byKey(const Key('code_scan')));
      await tester.pumpAndSettle();
      expect(asked, ['4601234567890']);
      expect(
        find.byKey(const Key('code_input')),
        findsNothing,
        reason: 'screen closed with the result',
      );
      expect(find.text('open'), findsOneWidget);
    });

    testWidgets('a typed code and Enter looks it up', (tester) async {
      final asked = <String>[];
      await open(tester, const ScanCancelled(), asked)();
      await tester.enterText(
        find.byKey(const Key('code_input')),
        ' 4601234567890 ',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pumpAndSettle();
      expect(asked, ['4601234567890']);
    });

    testWidgets('unknown code shows nothing found, ru and kk', (tester) async {
      for (final lang in ['ru', 'kk']) {
        await open(tester, const ScanText('4600000000000'), [], lang: lang)();
        await tester.tap(find.byKey(const Key('code_scan')));
        await tester.pumpAndSettle();
        final banner = tester.widget<Text>(
          find.descendant(
            of: find.byKey(const Key('code_banner')),
            matching: find.byType(Text),
          ),
        );
        expect(
          banner.data,
          Strings(lang)('code_not_found').replaceAll('{code}', '4600000000000'),
        );
        expect(banner.data, contains('4600000000000'));
        expect(find.byKey(const Key('code_input')), findsOneWidget);
        await tester.pumpWidget(const SizedBox());
      }
    });

    testWidgets('denied camera shows its own message and typing still works', (
      tester,
    ) async {
      final asked = <String>[];
      await open(tester, const ScanPermissionDenied(), asked)();
      await tester.tap(find.byKey(const Key('code_scan')));
      await tester.pumpAndSettle();
      expect(find.text(Strings('ru')('code_camera_off')), findsOneWidget);
      expect(find.text(Strings('ru')('code_not_found')), findsNothing);
      await tester.enterText(
        find.byKey(const Key('code_input')),
        '4601234567890',
      );
      await tester.tap(find.byKey(const Key('code_find')));
      await tester.pumpAndSettle();
      expect(asked, ['4601234567890']);
    });

    testWidgets('server down shows the network error, not nothing found', (
      tester,
    ) async {
      await open(tester, const ScanText('4601234567890'), [], down: true)();
      await tester.tap(find.byKey(const Key('code_scan')));
      await tester.pumpAndSettle();
      expect(find.text(Strings('ru')('err_network')), findsOneWidget);
    });
  });

  test('ru and kk have every find-by-code string', () {
    for (final k in [
      'find_by_code',
      'code_scan_title',
      'code_scan',
      'code_type_hint',
      'code_field',
      'code_find',
      'code_not_found',
      'code_camera_off',
      'code_found_sku',
      'code_found_location',
    ]) {
      expect(Strings('ru')(k), isNot(k));
      expect(Strings('kk')(k), isNot(k));
      if (k != 'code_field') expect(Strings('kk')(k), isNot(Strings('ru')(k)));
    }
  });
}
