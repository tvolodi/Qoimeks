import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qoimeks/api.dart';
import 'package:qoimeks/codes.dart';
import 'package:qoimeks/l10n.dart';
import 'package:qoimeks/operation_screen.dart';
import 'package:qoimeks/qr_scanner.dart';
import 'package:qoimeks/settings.dart';
import 'package:qoimeks/voice.dart';

class _Scans implements QrScanner {
  _Scans(this.results);
  final List<ScanResult> results;

  @override
  Future<ScanResult> scan(BuildContext context, Strings s) async =>
      results.length > 1 ? results.removeAt(0) : results.first;
}

class _NoVoice implements VoiceCapture {
  @override
  Future<bool> start() async => false;
  @override
  Future<Recording?> stop() async => null;
  @override
  void dispose() {}
}

http.Response _json(Object body, [int status = 200]) => http.Response.bytes(
  utf8.encode(jsonEncode(body)),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

/// A stand-in codes server: keeps active codes in memory, refuses a code another item holds.
class FakeCodes {
  /// code -> row. 'WASHER' belongs to SKU 2 from the start.
  final rows = <String, Map<String, dynamic>>{
    'WASHER': {
      'id': 'w1',
      'object_type': 'sku',
      'object_id': 2,
      'object_code': 'WSH-M8',
      'object_name': 'Шайба М8',
      'code_type': 'manual',
      'code': 'WASHER',
    },
    '4601234567890': {
      'id': 'c1',
      'object_type': 'sku',
      'object_id': 1,
      'object_code': 'NUT-M8',
      'object_name': 'Гайка М8',
      'code_type': 'ean13',
      'code': '4601234567890',
    },
  };
  final assigned = <Map<String, dynamic>>[];
  final removed = <String>[];
  final headers = <String?>{};

  late final client = MockClient((req) async {
    headers.add(req.headers['Authorization']);
    final path = req.url.path;
    if (path == '/api/catalog') {
      return _json({
        'skus': [
          {'id': 1, 'code': 'NUT-M8', 'name': 'Гайка М8', 'unit': 'шт'},
          {'id': 2, 'code': 'WSH-M8', 'name': 'Шайба М8', 'unit': 'шт'},
        ],
        'locations': [
          {'id': 10, 'code': 'A-01', 'name': 'Ряд A'},
        ],
      });
    }
    if (path == '/api/balances') return _json([]);
    if (path == '/api/codes/lookup') {
      final code = req.url.queryParameters['code']!;
      final r = rows[code];
      if (r != null) return _json(r);
      return _json({
        'error': 'not found',
        'reason': 'code_not_found',
        'code': code,
      }, 404);
    }
    if (path == '/api/codes' && req.method == 'GET') {
      final q = req.url.queryParameters;
      return _json({
        'codes': [
          for (final r in rows.values)
            if ('${r['object_id']}' == q['object_id'] &&
                r['object_type'] == q['object_type'])
              r,
        ],
      });
    }
    if (path == '/api/codes' && req.method == 'POST') {
      final b = jsonDecode(req.body) as Map<String, dynamic>;
      assigned.add(b);
      final held = rows[b['code']];
      if (held != null && held['object_id'] != b['object_id']) {
        return _json({
          'error':
              'code ${b['code']} is already used by ${held['object_code']}',
          'reason': 'code_in_use',
          'code': b['code'],
          'owner': {
            'object_type': held['object_type'],
            'object_id': held['object_id'],
            'object_code': held['object_code'],
            'object_name': held['object_name'],
          },
        }, 409);
      }
      final row = {
        'id': b['id'],
        'object_type': b['object_type'],
        'object_id': b['object_id'],
        'object_code': b['object_id'] == 1 ? 'NUT-M8' : 'WSH-M8',
        'object_name': b['object_id'] == 1 ? 'Гайка М8' : 'Шайба М8',
        'code_type': b['code_type'],
        'code': b['code'],
      };
      rows[b['code'] as String] = row;
      return _json(row, 201);
    }
    if (path.startsWith('/api/codes/') && req.method == 'DELETE') {
      final id = path.substring('/api/codes/'.length);
      removed.add('$id?${req.url.query}');
      rows.removeWhere((_, r) => r['id'] == id);
      return _json({'id': id});
    }
    return _json({'error': 'not found'}, 404);
  });

  ApiClient api() =>
      ApiClient('http://server', accessCode: 'code-1', client: client);
}

void main() {
  group('guessCodeType', () {
    test('EAN only with a valid check digit', () {
      expect(
        guessCodeType('4601234567890'),
        'manual',
        reason: 'check digit of this one is wrong',
      );
      expect(guessCodeType('4006381333931'), 'ean13');
      expect(guessCodeType('96385074'), 'ean8');
      expect(guessCodeType('96385075'), 'manual');
      expect(guessCodeType('RI2-03/B8'), 'manual');
    });
  });

  group('ApiClient codes', () {
    test(
      'assign, list and remove, with the operator and the access code',
      () async {
        final fake = FakeCodes();
        final api = fake.api();
        final c = await api.assignCode(
          id: 'n1',
          objectType: 'sku',
          objectId: 1,
          codeType: 'manual',
          code: 'NEW-1',
          operator: 'Айгуль',
        );
        expect(c.code, 'NEW-1');
        expect(
          (await api.codes('sku', 1)).map((e) => e.code),
          containsAll(['NEW-1', '4601234567890']),
        );
        await api.removeCode('n1', 'Айгуль');
        expect(
          fake.removed.single,
          'n1?operator=%D0%90%D0%B9%D0%B3%D1%83%D0%BB%D1%8C',
        );
        expect(fake.headers, {'Bearer code-1'});
      },
    );

    test('a duplicate carries the reason and the owner', () async {
      final api = FakeCodes().api();
      try {
        await api.assignCode(
          id: 'n2',
          objectType: 'sku',
          objectId: 1,
          codeType: 'manual',
          code: 'WASHER',
          operator: 'a',
        );
        fail('should be refused');
      } on ApiException catch (e) {
        expect(e.status, 409);
        expect(e.reason, 'code_in_use');
        expect(e.owner!.objectName, 'Шайба М8');
      }
    });
  });

  group('CodesScreen', () {
    Future<FakeCodes> open(
      WidgetTester tester, {
      String lang = 'ru',
      List<ScanResult>? scans,
    }) async {
      final fake = FakeCodes();
      await tester.pumpWidget(
        MaterialApp(
          home: CodesScreen(
            settings: AppSettings(
              serverUrl: 'http://server',
              operator: 'Айгуль',
              lang: lang,
            ),
            api: fake.api(),
            objectType: 'sku',
            objectId: 1,
            title: 'Гайка М8',
            scanner: _Scans(scans ?? [const ScanCancelled()]),
          ),
        ),
      );
      await tester.pumpAndSettle();
      return fake;
    }

    testWidgets('lists the codes of the item only', (tester) async {
      await open(tester);
      expect(find.text('4601234567890'), findsOneWidget);
      expect(find.text('WASHER'), findsNothing);
    });

    testWidgets('empty list says so and still offers to add', (tester) async {
      final fake = FakeCodes();
      await tester.pumpWidget(
        MaterialApp(
          home: CodesScreen(
            settings: AppSettings(serverUrl: 'http://server'),
            api: fake.api(),
            objectType: 'location',
            objectId: 10,
            title: 'A-01',
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('codes_empty')), findsOneWidget);
      expect(find.byKey(const Key('code_add')), findsOneWidget);
    });

    testWidgets(
      'remove asks first; keeping changes nothing, removing updates the list',
      (tester) async {
        final fake = await open(tester);
        await tester.tap(find.byKey(const Key('code_remove_c1')));
        await tester.pumpAndSettle();
        expect(find.textContaining('4601234567890'), findsWidgets);
        await tester.tap(find.byKey(const Key('code_remove_keep')));
        await tester.pumpAndSettle();
        expect(fake.removed, isEmpty);
        expect(find.text('4601234567890'), findsOneWidget);

        await tester.tap(find.byKey(const Key('code_remove_c1')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('code_remove_confirm')));
        await tester.pumpAndSettle();
        expect(fake.removed, hasLength(1));
        expect(find.text('4601234567890'), findsNothing);
        expect(find.byKey(const Key('codes_empty')), findsOneWidget);
      },
    );

    testWidgets('add by typing saves and shows the new code', (tester) async {
      final fake = await open(tester);
      await tester.tap(find.byKey(const Key('code_add')));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const Key('code_input')), ' SHELF-9 ');
      await tester.tap(find.byKey(const Key('code_save')));
      await tester.pumpAndSettle();
      expect(fake.assigned.single['code'], 'SHELF-9');
      expect(fake.assigned.single['code_type'], 'manual');
      expect(fake.assigned.single['operator'], 'Айгуль');
      expect(find.text('SHELF-9'), findsOneWidget);
      expect(find.byKey(const Key('code_input')), findsNothing);
    });

    testWidgets('add by scan fills the field; a valid EAN is typed as ean13', (
      tester,
    ) async {
      final fake = await open(tester, scans: [const ScanText('4006381333931')]);
      await tester.tap(find.byKey(const Key('code_add')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('code_scan')));
      await tester.pumpAndSettle();
      expect(find.text('4006381333931'), findsOneWidget);
      await tester.tap(find.byKey(const Key('code_save')));
      await tester.pumpAndSettle();
      expect(fake.assigned.single['code_type'], 'ean13');
    });

    for (final lang in ['ru', 'kk']) {
      testWidgets('a duplicate names the owner, $lang, and opens its codes', (
        tester,
      ) async {
        final fake = await open(tester, lang: lang);
        await tester.tap(find.byKey(const Key('code_add')));
        await tester.pumpAndSettle();
        await tester.enterText(find.byKey(const Key('code_input')), 'WASHER');
        await tester.tap(find.byKey(const Key('code_save')));
        await tester.pumpAndSettle();
        final text = Strings(lang)(
          'code_in_use',
        ).replaceAll('{code}', 'WASHER').replaceAll('{owner}', 'Шайба М8');
        expect(find.text(text), findsOneWidget);
        expect(text, contains('Шайба М8'));
        expect(
          fake.rows['WASHER']!['object_id'],
          2,
          reason: 'nothing was taken over',
        );
        // Still on the form; the owner's list opens from the banner.
        await tester.tap(find.byKey(const Key('code_open_owner')));
        await tester.pumpAndSettle();
        expect(find.text('WASHER'), findsOneWidget);
      });
    }
  });

  group('operation form: unknown code', () {
    const unknown = ScanText('NEW-777');

    Future<FakeCodes> pump(
      WidgetTester tester,
      List<ScanResult> scans, {
      String lang = 'ru',
    }) async {
      tester.view.physicalSize = const Size(1080, 2400);
      tester.view.devicePixelRatio = 2.5;
      addTearDown(tester.view.reset);
      final fake = FakeCodes();
      await tester.pumpWidget(
        MaterialApp(
          home: OperationScreen(
            settings: AppSettings(
              serverUrl: 'http://server',
              operator: 'Айгуль',
              lang: lang,
            ),
            api: fake.api(),
            voice: _NoVoice(),
            scanner: _Scans(scans),
          ),
        ),
      );
      await tester.pumpAndSettle();
      return fake;
    }

    Future<void> scanOnce(WidgetTester tester) async {
      await tester.tap(find.byKey(const Key('find_by_code_button')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('code_scan')));
      await tester.pumpAndSettle();
    }

    testWidgets(
      'with an item chosen, offers to assign it and picks it afterwards',
      (tester) async {
        final fake = await pump(tester, [
          const ScanText('4601234567890'),
          unknown,
        ]);
        await scanOnce(tester); // known code: chooses the nut
        expect(find.text('Гайка М8'), findsWidgets);
        await scanOnce(tester); // unknown
        expect(find.byKey(const Key('code_assign_offer')), findsOneWidget);
        expect(find.textContaining('Гайка М8'), findsWidgets);
        await tester.tap(find.byKey(const Key('code_assign_offer')));
        await tester.pumpAndSettle();
        expect(fake.assigned.single['object_id'], 1);
        expect(fake.assigned.single['code'], 'NEW-777');
        expect(fake.rows['NEW-777'], isNotNull);
        expect(
          find.byKey(const Key('code_input')),
          findsNothing,
          reason: 'back on the form',
        );
      },
    );

    testWidgets('without an item there is no offer', (tester) async {
      await pump(tester, [unknown]);
      await scanOnce(tester);
      expect(find.byKey(const Key('code_banner')), findsOneWidget);
      expect(find.byKey(const Key('code_assign_offer')), findsNothing);
    });

    testWidgets('the offer for a code another item holds shows the owner', (
      tester,
    ) async {
      final fake = await pump(tester, [
        const ScanText('4601234567890'),
        const ScanText('WASHER'),
      ]);
      await scanOnce(tester);
      fake.rows.remove('WASHER'); // lookup says unknown ...
      await scanOnce(tester);
      fake.rows['WASHER'] = {
        'id': 'w1',
        'object_type': 'sku',
        'object_id': 2,
        'object_code': 'WSH-M8',
        'object_name': 'Шайба М8',
        'code_type': 'manual',
        'code': 'WASHER',
      }; // ... then another phone took it before we saved
      await tester.tap(find.byKey(const Key('code_assign_offer')));
      await tester.pumpAndSettle();
      expect(find.textContaining('Шайба М8'), findsOneWidget);
      expect(
        find.byKey(const Key('code_input')),
        findsOneWidget,
        reason: 'stays to scan another',
      );
    });

    testWidgets('the item and location code lists open from the form', (
      tester,
    ) async {
      await pump(tester, [const ScanText('4601234567890')]);
      await scanOnce(tester);
      await tester.tap(find.byKey(const Key('sku_codes')));
      await tester.pumpAndSettle();
      expect(find.text('4601234567890'), findsOneWidget);
    });
  });

  test('ru and kk have every codes string', () {
    for (final k in [
      'codes_of',
      'codes_empty',
      'code_add',
      'code_save',
      'code_saved',
      'code_remove',
      'code_remove_q',
      'code_keep',
      'code_remove_action',
      'code_type_label',
      'code_type_ean13',
      'code_type_ean8',
      'code_type_code128',
      'code_type_qr',
      'code_type_manual',
      'code_in_use',
      'code_open_owner',
      'code_invalid',
      'code_object_gone',
      'code_assign_offer',
      'code_scan_again',
      'item_codes',
      'location_codes',
    ]) {
      expect(Strings('ru')(k), isNot(k), reason: k);
      expect(Strings('kk')(k), isNot(k), reason: k);
      if (!k.startsWith('code_type_')) {
        expect(Strings('kk')(k), isNot(Strings('ru')(k)), reason: k);
      }
    }
  });
}
