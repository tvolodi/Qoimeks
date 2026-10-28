import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qoimeks/api.dart';
import 'package:qoimeks/l10n.dart';
import 'package:qoimeks/offline_queue.dart';
import 'package:qoimeks/operation_screen.dart';
import 'package:qoimeks/settings.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'operation_screen_test.dart' show FakeVoice;

const _url = 'http://server';
const _code = 'code-1';

/// A stand-in ledger: stores movements by id, so the same id twice posts once.
class LedgerServer {
  bool networkDown = false;

  /// When true the next post is stored but its response is lost (the phone sees a network error).
  bool loseNextResponse = false;
  int status = 201;
  Map<String, int> failFor = {}; // qty (as text) -> status to answer, for rejection tests
  bool balancesDown = false;
  final requests = <Map<String, dynamic>>[];
  final ledger = <String, Map<String, dynamic>>{};

  http.Response _json(Object body, [int code = 200]) => http.Response.bytes(
        utf8.encode(jsonEncode(body)),
        code,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );

  late final client = MockClient((req) async {
    if (networkDown) throw http.ClientException('no route to host');
    switch (req.url.path) {
      case '/api/catalog':
        return _json({
          'skus': [
            {'id': 1, 'code': 'NUT-M8', 'name': 'Гайка М8', 'unit': 'шт', 'location_id': 10},
          ],
          'locations': [
            {'id': 10, 'code': 'A-01', 'name': 'Ряд A'},
          ],
        });
      case '/api/balances':
        if (balancesDown) throw http.ClientException('no route to host');
        return _json([
          {'sku_id': 1, 'location_id': 10, 'location_code': 'A-01', 'qty': 5},
        ]);
      case '/api/movements':
        final body = jsonDecode(req.body) as Map<String, dynamic>;
        requests.add(body);
        final fail = failFor['${body['qty']}'];
        if (fail != null) return _json({'error': 'bad item'}, fail);
        // Cloudflare answers 52x with an HTML page when the origin is down.
        if (status >= 520) return http.Response('<html>origin down</html>', status, headers: {'content-type': 'text/html'});
        if (status >= 400) return _json({'error': 'boom'}, status);
        ledger.putIfAbsent(body['id'] as String, () => body);
        if (loseNextResponse) {
          loseNextResponse = false;
          throw http.ClientException('connection reset');
        }
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

  ApiClient api() => ApiClient(_url, accessCode: _code, client: client);
}

QueuedOperation op(String id, {double qty = 1, String kind = 'receipt', String url = _url, String code = _code}) =>
    QueuedOperation(
      id: id,
      kind: kind,
      skuId: 1,
      locationId: 10,
      qty: qty,
      operator: 'Айгуль',
      source: 'keyboard',
      skuName: 'Гайка М8',
      skuCode: 'NUT-M8',
      unit: 'шт',
      locationCode: 'A-01',
      serverUrl: url,
      accessCode: code,
      queuedAt: DateTime.utc(2026, 10, 28),
    );

Future<void> flush(OfflineQueue q, LedgerServer s, {String url = _url, String code = _code}) =>
    q.flush(s.api(), serverUrl: url, accessCode: code);

Future<OfflineQueue> newQueue() async {
  SharedPreferences.setMockInitialValues({});
  final q = OfflineQueue();
  await q.load();
  return q;
}

Future<void> pumpForm(WidgetTester tester, LedgerServer server, OfflineQueue queue, {AppSettings? settings}) async {
  tester.view.physicalSize = const Size(1080, 2400);
  tester.view.devicePixelRatio = 2.5;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(
    home: OperationScreen(
      settings: settings ?? AppSettings(serverUrl: _url, accessCode: _code, operator: 'Айгуль'),
      api: server.api(),
      voice: FakeVoice(),
      offlineQueue: queue,
    ),
  ));
  await tester.pumpAndSettle();
}

Future<void> enterAndPost(WidgetTester tester, String qty, {String? kindKey}) async {
  if (kindKey != null) {
    await tester.tap(find.byKey(Key(kindKey)));
    await tester.pumpAndSettle();
  }
  await tester.tap(find.byKey(const Key('sku')));
  await tester.pumpAndSettle();
  await tester.tap(find.text('Гайка М8'));
  await tester.pumpAndSettle();
  await tester.enterText(find.byKey(const Key('qty')), qty);
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(const Key('post')));
  await tester.pumpAndSettle();
}

void main() {
  l10nParity();
  group('queue', () {
    test('items are sent oldest first and leave the queue', () async {
      final q = await newQueue();
      final s = LedgerServer();
      await q.enqueue(op('a', qty: 1));
      await q.enqueue(op('b', qty: 2));
      expect(q.waiting(_url, _code), 2);

      await flush(q, s);
      expect(s.requests.map((r) => r['id']), ['a', 'b']);
      expect(q.items, isEmpty);
    });

    test('survives a restart: storage is the source', () async {
      final q = await newQueue();
      await q.enqueue(op('a'));
      final again = OfflineQueue();
      await again.load();
      expect(again.items.single.id, 'a');
    });

    test('a lost response re-sends the same id and the server posts once', () async {
      final q = await newQueue();
      final s = LedgerServer()..loseNextResponse = true;
      await q.enqueue(op('a'));

      await flush(q, s);
      expect(q.waiting(_url, _code), 1, reason: 'no confirmation yet, the item stays');
      await flush(q, s);

      expect(s.requests.map((r) => r['id']), ['a', 'a']);
      expect(s.ledger, hasLength(1));
      expect(q.items, isEmpty);
    });

    test('a rejected item does not block later items and stays with its reason', () async {
      final q = await newQueue();
      final s = LedgerServer()..failFor = {'1.0': 400};
      await q.enqueue(op('a', qty: 1));
      await q.enqueue(op('b', qty: 2));

      await flush(q, s);
      expect(s.ledger.keys, ['b']);
      expect(q.items.single.id, 'a');
      expect(q.items.single.rejection, 'bad item');
      expect(q.waiting(_url, _code), 0);

      // Not re-sent until the operator asks.
      s.requests.clear();
      await flush(q, s);
      expect(s.requests, isEmpty);
      s.failFor = {};
      await q.retry('a');
      await flush(q, s);
      expect(s.ledger.keys, containsAll(['a', 'b']));
      expect(q.items, isEmpty);
    });

    test('a network error, 500 or 401 stops the flush and rejects nothing', () async {
      for (final status in [500, 503, 401, 522, 521, 404, 429]) {
        final q = await newQueue();
        final s = LedgerServer()..status = status;
        await q.enqueue(op('a'));
        await q.enqueue(op('b'));
        await flush(q, s);
        expect(s.requests, hasLength(1), reason: 'status $status stops at the first item');
        expect(q.items.map((o) => o.rejection), [null, null]);
      }
      final q = await newQueue();
      final s = LedgerServer()..networkDown = true;
      await q.enqueue(op('a'));
      await flush(q, s);
      expect(q.waiting(_url, _code), 1);
    });

    test('items of another server or access code are not sent', () async {
      final q = await newQueue();
      final s = LedgerServer();
      await q.enqueue(op('a'));

      await flush(q, s, url: 'http://other');
      await flush(q, s, code: 'code-2');
      expect(s.requests, isEmpty);
      expect(q.waiting('http://other', _code), 0);
      expect(q.foreign('http://other', _code).single.id, 'a');

      // The same address with a trailing slash is the same server.
      await flush(q, s, url: '$_url/');
      expect(s.ledger.keys, ['a']);
    });

    test('the queue is capped and refuses more instead of dropping silently', () async {
      final q = await newQueue();
      for (var i = 0; i < OfflineQueue.cap; i++) {
        expect(await q.enqueue(op('i$i')), isTrue);
      }
      expect(await q.enqueue(op('extra')), isFalse);
      expect(q.items, hasLength(OfflineQueue.cap));
    });

    test('corrupt storage is treated as empty', () async {
      SharedPreferences.setMockInitialValues({OfflineQueue.prefsKey: '{not json'});
      final q = OfflineQueue();
      await q.load();
      expect(q.items, isEmpty);
    });
  });

  group('operation screen', () {
    testWidgets('a post without network is queued, counted, and sent in order when the server is back', (tester) async {
      final q = await newQueue();
      final s = LedgerServer();
      await pumpForm(tester, s, q);
      s.networkDown = true;

      await enterAndPost(tester, '3');
      expect(find.text('Ожидают отправки: 1'), findsOneWidget);
      expect(find.textContaining('сохранена на телефоне'), findsOneWidget);
      // The form is ready for the next operation.
      expect(tester.widget<FilledButton>(find.byKey(const Key('post'))).onPressed, isNull);

      await enterAndPost(tester, '4');
      expect(find.text('Ожидают отправки: 2'), findsOneWidget);
      expect(q.items.map((o) => o.qty), [3, 4]);

      s.networkDown = false;
      await flush(q, s);
      await tester.pumpAndSettle();
      expect(s.requests.map((r) => r['qty']), [3, 4]);
      expect(find.byKey(const Key('queue_banner')), findsNothing);
    });

    testWidgets('a post answered 522 by the proxy (origin down) is queued', (tester) async {
      final q = await newQueue();
      final s = LedgerServer();
      await pumpForm(tester, s, q);
      s.status = 522;

      await enterAndPost(tester, '3');
      expect(find.text('Ожидают отправки: 1'), findsOneWidget);
      expect(find.textContaining('сохранена на телефоне'), findsOneWidget);
      expect(q.items.single.rejection, isNull);
    });

    testWidgets('a new operation goes behind the waiting ones, so a count cannot overtake a receipt', (tester) async {
      final q = await newQueue();
      final s = LedgerServer();
      await pumpForm(tester, s, q);
      s.networkDown = true;
      await enterAndPost(tester, '3');
      s.networkDown = false; // back in coverage, but nothing has flushed yet

      await enterAndPost(tester, '7', kindKey: 'kind_adjustment');
      expect(s.requests.map((r) => [r['kind'], r['qty']]), [
        ['receipt', 3],
        ['adjustment', 7],
      ]);
      expect(find.textContaining('Проведено'), findsOneWidget);
      expect(q.items, isEmpty);
    });

    testWidgets('behind the queue, with the server down again, both stay queued in order', (tester) async {
      final q = await newQueue();
      final s = LedgerServer();
      await pumpForm(tester, s, q);
      s.networkDown = true;
      await enterAndPost(tester, '3');
      await enterAndPost(tester, '7', kindKey: 'kind_adjustment');
      expect(q.items.map((o) => o.kind), ['receipt', 'adjustment']);
      expect(find.text('Ожидают отправки: 2'), findsOneWidget);
    });

    testWidgets('a refused operation behind the queue shows the reason, keeps the form and takes a new id', (tester) async {
      final q = await newQueue();
      final s = LedgerServer();
      await pumpForm(tester, s, q);
      s.networkDown = true;
      await enterAndPost(tester, '3');
      s.networkDown = false;
      s.failFor = {'7.0': 422};

      await enterAndPost(tester, '7');
      expect(find.textContaining('Сервер не принял: bad item'), findsOneWidget);
      expect(q.items, isEmpty, reason: 'receipt 3 was sent, the refused one removed');
      expect(find.text('Гайка М8'), findsOneWidget);
      s.failFor = {};
      await tester.tap(find.byKey(const Key('post')));
      await tester.pumpAndSettle();
      final ids = s.requests.where((r) => r['qty'] == 7).map((r) => r['id']).toSet();
      expect(ids, hasLength(2), reason: 'the retry carries a fresh id');
    });

    testWidgets('a rejected item stays visible on the queue screen with its reason', (tester) async {
      final q = await newQueue();
      final s = LedgerServer()..failFor = {'1.0': 400};
      await q.enqueue(op('a', qty: 1));
      await q.enqueue(op('b', qty: 2));
      await pumpForm(tester, s, q);
      await flush(q, s);
      await tester.pumpAndSettle();

      expect(s.ledger.keys, ['b']);
      await tester.tap(find.byKey(const Key('queue_banner')));
      await tester.pumpAndSettle();
      expect(find.textContaining('Сервер не принял: bad item'), findsOneWidget);
      expect(find.byKey(const Key('queue_retry_a')), findsOneWidget);

      await tester.tap(find.byKey(const Key('queue_delete_a')));
      await tester.pumpAndSettle();
      expect(find.text('Удалить неотправленную операцию?'), findsOneWidget);
      await tester.tap(find.byKey(const Key('queue_delete_keep')));
      await tester.pumpAndSettle();
      expect(q.items, hasLength(1));

      await tester.tap(find.byKey(const Key('queue_delete_a')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('queue_delete_confirm')));
      await tester.pumpAndSettle();
      expect(q.items, isEmpty);
      expect(find.text('Очередь пуста'), findsOneWidget);
    });

    testWidgets('an item for another server is shown with delete only', (tester) async {
      final q = await newQueue();
      final s = LedgerServer();
      await q.enqueue(op('a', url: 'http://other'));
      await pumpForm(tester, s, q);

      await tester.tap(find.byKey(const Key('queue_banner')));
      await tester.pumpAndSettle();
      expect(find.textContaining('Для другого сервера'), findsOneWidget);
      expect(find.byKey(const Key('queue_retry_a')), findsNothing);
      expect(find.byKey(const Key('queue_delete_a')), findsOneWidget);
      expect(s.requests, isEmpty);
    });

    testWidgets('balances fetched while offline are marked as possibly old', (tester) async {
      final q = await newQueue();
      final s = LedgerServer();
      await pumpForm(tester, s, q);

      await tester.tap(find.byKey(const Key('sku')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Гайка М8'));
      await tester.pumpAndSettle();
      expect(find.text('A-01 — остаток 5 шт'), findsOneWidget);
      expect(find.textContaining('могли устареть'), findsNothing);

      s.balancesDown = true;
      await tester.tap(find.byKey(const Key('sku')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Гайка М8'));
      await tester.pumpAndSettle();
      expect(find.text('A-01 — остаток 5 шт'), findsOneWidget);
      expect(find.textContaining('могли устареть'), findsOneWidget);
    });
  });
}

void l10nParity() {
  test('queue texts exist in Russian and Kazakh', () {
    for (final key in ['queued_saved', 'queue_waiting', 'queue_title', 'queue_empty', 'queue_rejected', 'queue_delete', 'queue_delete_q', 'queue_keep', 'queue_delete_action', 'queue_foreign', 'balance_stale']) {
      final ru = const Strings('ru')(key);
      final kk = const Strings('kk')(key);
      expect(ru, isNot(key));
      expect(kk, isNot(key));
      expect(kk, isNot(ru));
    }
  });
}
