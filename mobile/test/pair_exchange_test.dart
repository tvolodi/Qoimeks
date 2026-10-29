import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qoimeks/api.dart';

http.Response _json(int status, Object body) => http.Response.bytes(
  utf8.encode(jsonEncode(body)),
  status,
  headers: {'content-type': 'application/json'},
);

void main() {
  test(
    'exchange posts the code without Authorization and parses the key',
    () async {
      late http.Request seen;
      final errors = <String>[];
      final api = ApiClient(
        'http://server/',
        accessCode: 'old-code',
        client: MockClient((req) async {
          seen = req;
          return _json(200, {
            'device_id': 'd-1',
            'device_key': 'qdk_FAKE',
            'tenant': {'name': 'Demo', 'language': 'ru'},
          });
        }),
        onError: (p, e) => errors.add(p),
      );
      final r = await api.pairExchange(
        code: 'K7M2-9QXA',
        deviceLabel: 'Samsung A15',
        operator: ' Aidar ',
        appVersion: '0.2.0',
      );
      expect(seen.url.toString(), 'http://server/pair/v1/exchange');
      expect(seen.method, 'POST');
      expect(seen.headers.containsKey('Authorization'), isFalse);
      expect(jsonDecode(seen.body), {
        'code': 'K7M2-9QXA',
        'device_label': 'Samsung A15',
        'operator': 'Aidar',
        'app_version': '0.2.0',
      });
      expect(r.deviceKey, 'qdk_FAKE');
      expect(r.deviceId, 'd-1');
      expect(r.tenantName, 'Demo');
      expect(r.tenantLanguage, 'ru');
      expect(r.toString(), isNot(contains('qdk_')));
    },
  );

  test('optional fields are left out when empty', () async {
    late Map body;
    final api = ApiClient(
      'http://s',
      client: MockClient((req) async {
        body = jsonDecode(req.body);
        return _json(200, {
          'device_id': 'd',
          'device_key': 'qdk_x',
          'tenant': {},
        });
      }),
    );
    await api.pairExchange(code: 'A', deviceLabel: 'Phone');
    expect(body.keys.toSet(), {'code', 'device_label'});
  });

  test(
    'server errors keep status and error code and are not observed',
    () async {
      final errors = <String>[];
      for (final c in [
        (400, 'invalid_pairing_code'),
        (403, 'tenant_unavailable'),
        (429, 'rate_limited'),
      ]) {
        final api = ApiClient(
          'http://s',
          client: MockClient((req) async => _json(c.$1, {'error': c.$2})),
          onError: (p, e) => errors.add(p),
        );
        await expectLater(
          api.pairExchange(code: 'A', deviceLabel: 'P'),
          throwsA(
            isA<ApiException>()
                .having((e) => e.status, 'status', c.$1)
                .having((e) => e.message, 'message', c.$2),
          ),
        );
      }
      expect(errors, isEmpty);
    },
  );

  test(
    'no network is ApiException status 0; a response without key is rejected',
    () async {
      final down = ApiClient(
        'http://s',
        client: MockClient((req) async => throw Exception('offline')),
      );
      await expectLater(
        down.pairExchange(code: 'A', deviceLabel: 'P'),
        throwsA(isA<ApiException>().having((e) => e.status, 'status', 0)),
      );
      final bad = ApiClient(
        'http://s',
        client: MockClient((req) async => _json(200, {'tenant': {}})),
      );
      await expectLater(
        bad.pairExchange(code: 'A', deviceLabel: 'P'),
        throwsA(isA<ApiException>().having((e) => e.status, 'status', 0)),
      );
    },
  );
}
