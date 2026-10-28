import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qoimeks/api.dart';
import 'package:qoimeks/l10n.dart';
import 'package:qoimeks/operation_screen.dart';
import 'package:qoimeks/settings.dart';
import 'package:qoimeks/voice.dart';

class FakeVoice implements VoiceCapture {
  @override
  Future<bool> start() async => true;

  @override
  Future<Recording?> stop() async => Recording(Uint8List.fromList([1, 2, 3]), 'phrase.m4a');

  @override
  void dispose() {}
}

const _nut8 = {'id': 1, 'code': 'NUT-M8', 'name': 'Гайка М8', 'unit': 'шт', 'location_id': 10, 'alt_location_id': 11};
const _nut10 = {'id': 2, 'code': 'NUT-M10', 'name': 'Гайка М10', 'unit': 'шт'};
const _a01 = {'id': 10, 'code': 'A-01', 'name': 'Ряд A'};
const _b02 = {'id': 11, 'code': 'B-02', 'name': 'Ряд B'};

/// A stand-in server. [draft] is what /api/voice answers; posted bodies land in [posted].
class FakeServer {
  Map<String, dynamic> draft = {};
  int postStatus = 201;

  /// When set, /api/voice answers this status and body (or the network is down).
  int? voiceStatus;
  Map<String, dynamic> voiceBody = {};
  bool networkDown = false;
  final posted = <Map<String, dynamic>>[];
  final authHeaders = <String?>{};

  http.Response _json(Object body, [int status = 200]) => http.Response.bytes(
        utf8.encode(jsonEncode(body)),
        status,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );

  late final client = MockClient((req) async {
    authHeaders.add(req.headers['Authorization']);
    if (networkDown && req.url.path == '/api/voice') throw http.ClientException('no route to host');
    switch (req.url.path) {
      case '/api/catalog':
        return _json({
          'skus': [_nut8, _nut10],
          'locations': [_a01, _b02],
        });
      case '/api/balances':
        return _json([
          {'sku_id': 1, 'location_id': 10, 'location_code': 'A-01', 'qty': 5},
          {'sku_id': 1, 'location_id': 11, 'location_code': 'B-02', 'qty': 40},
        ]);
      case '/api/voice':
        if (voiceStatus != null) return _json(voiceBody, voiceStatus!);
        return _json(draft);
      case '/api/movements':
        final body = jsonDecode(req.body) as Map<String, dynamic>;
        posted.add(body);
        if (postStatus >= 400) return _json({'error': 'boom'}, postStatus);
        return _json({
          ...body,
          'sku_code': 'NUT-M8',
          'sku_name': 'Гайка М8',
          'unit': 'шт',
          'location_code': 'A-01',
          'created_at': '2026-10-08T10:00:00Z',
          'balances': [
            {'sku_id': 1, 'location_id': 10, 'location_code': 'A-01', 'qty': 17},
          ],
        }, 201);
    }
    return _json({'error': 'not found'}, 404);
  });
}

Future<void> pumpForm(WidgetTester tester, FakeServer server) async {
  tester.view.physicalSize = const Size(1080, 2400);
  tester.view.devicePixelRatio = 2.5;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(
    home: OperationScreen(
      settings: AppSettings(serverUrl: 'http://server', operator: 'Айгуль'),
      api: ApiClient('http://server', accessCode: 'code-1', client: server.client),
      voice: FakeVoice(),
    ),
  ));
  await tester.pumpAndSettle();
}

FilledButton postButton(WidgetTester tester) => tester.widget<FilledButton>(find.byKey(const Key('post')));

void main() {
  testWidgets('an operation can be entered entirely by hand', (tester) async {
    final server = FakeServer();
    await pumpForm(tester, server);
    expect(postButton(tester).onPressed, isNull);

    await tester.tap(find.byKey(const Key('sku')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('picker_search')), 'гайка m8');
    await tester.pumpAndSettle();
    expect(find.text('Гайка М10'), findsNothing);
    await tester.tap(find.text('Гайка М8'));
    await tester.pumpAndSettle();

    // The SKU's usual place is offered straight away.
    expect(find.text('A-01'), findsOneWidget);
    expect(find.text('Смотреть также: B-02'), findsOneWidget);

    await tester.tap(find.byKey(const Key('location')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('picker_search')), 'b02');
    await tester.pumpAndSettle();
    await tester.tap(find.text('B-02'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('location')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('picker_search')), 'a01');
    await tester.pumpAndSettle();
    await tester.tap(find.text('A-01'));
    await tester.pumpAndSettle();

    await tester.enterText(find.byKey(const Key('qty')), '12,5');
    await tester.pumpAndSettle();
    expect(find.text('A-01 — остаток 5 шт'), findsOneWidget);
    expect(find.text('Есть в: B-02 (40)'), findsOneWidget);

    await tester.tap(find.byKey(const Key('post')));
    await tester.pumpAndSettle();

    final body = server.posted.single;
    expect(body['kind'], 'receipt');
    expect(body['sku_id'], 1);
    expect(body['location_id'], 10);
    expect(body['to_location_id'], isNull);
    expect(body['qty'], 12.5);
    expect(body['operator'], 'Айгуль');
    expect(body['source'], 'keyboard');
    expect(find.textContaining('Проведено · A-01: 17 шт'), findsOneWidget);
    // The form is empty again, ready for the next operation.
    expect(postButton(tester).onPressed, isNull);
    expect(find.text('A-01'), findsNothing);
  });

  testWidgets('a spoken phrase fills the form and waits for the operator to confirm', (tester) async {
    final server = FakeServer()
      ..draft = {
        'transcript': 'переместил семь гаек эм восемь из а один в бэ два',
        'kind': 'transfer',
        'skus': [_nut8],
        'location': _a01,
        'to_location': _b02,
        'qty': 7,
      };
    await pumpForm(tester, server);

    await tester.tap(find.byKey(const Key('mic')));
    await tester.pumpAndSettle();
    expect(find.textContaining('Говорите'), findsOneWidget);
    await tester.tap(find.byKey(const Key('mic')));
    await tester.pumpAndSettle();

    expect(server.posted, isEmpty, reason: 'voice must never post by itself');
    expect(find.textContaining('переместил семь гаек'), findsOneWidget);
    expect(find.text('A-01'), findsOneWidget);
    expect(find.text('B-02'), findsOneWidget);
    expect(find.text('7'), findsOneWidget);
    // 7 out of a location holding 5.
    expect(find.text('Остаток станет отрицательным'), findsOneWidget);

    await tester.tap(find.byKey(const Key('post')));
    await tester.pumpAndSettle();
    final body = server.posted.single;
    expect(body['kind'], 'transfer');
    expect(body['to_location_id'], 11);
    expect(body['qty'], 7);
    expect(body['source'], 'voice');
    expect(body['transcript'], contains('переместил'));
    // Catalog, balances, voice and post all carried the access code.
    expect(server.authHeaders, {'Bearer code-1'});
  });

  testWidgets('when several SKUs fit, the operator has to choose one', (tester) async {
    final server = FakeServer()
      ..draft = {
        'transcript': 'приход гайки двадцать штук на а один',
        'kind': 'receipt',
        'skus': [_nut8, _nut10],
        'location': _a01,
        'qty': 20,
      };
    await pumpForm(tester, server);
    await tester.tap(find.byKey(const Key('mic')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('mic')));
    await tester.pumpAndSettle();

    expect(find.text('Какой товар?'), findsOneWidget);
    expect(postButton(tester).onPressed, isNull);

    await tester.tap(find.text('Гайка М10 (NUT-M10)'));
    await tester.pumpAndSettle();
    expect(find.text('Какой товар?'), findsNothing);
    await tester.tap(find.byKey(const Key('post')));
    await tester.pumpAndSettle();
    expect(server.posted.single['sku_id'], 2);
  });

  testWidgets('a failed post keeps the form and retries with the same id', (tester) async {
    final server = FakeServer()
      ..postStatus = 500
      ..draft = {
        'transcript': 'приход',
        'kind': 'receipt',
        'skus': [_nut8],
        'location': _a01,
        'qty': 3,
      };
    await pumpForm(tester, server);
    await tester.tap(find.byKey(const Key('mic')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('mic')));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('post')));
    await tester.pumpAndSettle();
    expect(find.textContaining('Ошибка'), findsOneWidget);
    expect(find.text('A-01'), findsOneWidget);

    server.postStatus = 201;
    await tester.tap(find.byKey(const Key('post')));
    await tester.pumpAndSettle();
    expect(server.posted, hasLength(2));
    expect(server.posted[0]['id'], server.posted[1]['id']);
  });

  group('unavailable parts are named, and the form still works by hand', () {
    final cases = <String, void Function(FakeServer)>{
      'server': (s) => s.networkDown = true,
      'speech': (s) => s
        ..voiceStatus = 503
        ..voiceBody = {'error': 'speech recognition is not configured on the server', 'source': 'speech'},
      'llm': (s) => s
        ..voiceStatus = 502
        ..voiceBody = {'error': 'could not understand the phrase', 'source': 'llm'},
    };
    const ru = {
      'server': 'Сервер недоступен',
      'speech': 'Распознавание речи недоступно',
      'llm': 'Языковая модель недоступна',
    };

    for (final entry in cases.entries) {
      testWidgets('${entry.key} down', (tester) async {
        final server = FakeServer();
        entry.value(server);
        await pumpForm(tester, server);
        await tester.tap(find.byKey(const Key('mic')));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('mic')));
        await tester.pumpAndSettle();

        expect(find.textContaining(ru[entry.key]!), findsOneWidget);
        for (final other in ru.entries.where((e) => e.key != entry.key)) {
          expect(find.textContaining(other.value), findsNothing);
        }
        expect(find.textContaining('вручную'), entry.key == 'server' ? findsNothing : findsOneWidget);

        // Fill the form by hand and post it.
        await tester.tap(find.byKey(const Key('sku')));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Гайка М8'));
        await tester.pumpAndSettle();
        await tester.enterText(find.byKey(const Key('qty')), '3');
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('post')));
        await tester.pumpAndSettle();
        expect(server.posted.single['qty'], 3);
        expect(server.posted.single['source'], 'keyboard');
      });
    }

    test('messages exist in Russian and Kazakh', () {
      for (final e in [
        const ApiException(0, 'x'),
        const ApiException(503, 'x', source: 'speech'),
        const ApiException(502, 'x', source: 'llm'),
      ]) {
        final ru = const Strings('ru').error(e);
        final kk = const Strings('kk').error(e);
        expect(ru, isNot(startsWith('Ошибка')));
        expect(kk, isNot(ru));
      }
      expect(const Strings('kk').error(const ApiException(502, 'x', source: 'llm')), contains('қолмен'));
    });

    test('a failed call is written to the local error log with its source', () async {
      final seen = <String>[];
      final api = ApiClient('http://s',
          client: MockClient((_) async => http.Response('{"error":"e","source":"llm"}', 502)),
          onError: (path, e) => seen.add('$path ${e.status} ${e.source}'));
      await expectLater(api.voice(Uint8List(1), 'a.m4a', 'ru'), throwsA(isA<ApiException>()));
      expect(seen, ['/api/voice 502 llm']);
    });
  });
}
