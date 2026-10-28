import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qoimeks/api.dart';
import 'package:qoimeks/feedback.dart' show DeviceInfo;

Future<DeviceInfo> _device() async =>
    (appVersion: '0.1.2+3', androidVersion: '14', device: 'Samsung A15');

Future<DeviceInfo> _noDevice() async =>
    (appVersion: '', androidVersion: '', device: '');

void main() {
  test('voice, catalog and movements calls carry the app headers', () async {
    final seen = <String, Map<String, String>>{};
    final client = MockClient((req) async {
      seen[req.url.path] = req.headers;
      final body = req.url.path == '/api/movements'
          ? {
              'id': '11111111-1111-1111-1111-111111111111',
              'seq': 1,
              'kind': 'receipt',
              'sku_code': 'N',
              'sku_name': 'Nut',
              'unit': 'pc',
              'location_code': 'A-01',
              'qty': 1,
              'operator': 'Айгуль',
              'created_at': '2026-10-28T10:00:00Z',
              'balances': [],
            }
          : req.url.path == '/api/catalog'
          ? {'skus': [], 'locations': []}
          : {'transcript': 'x', 'skus': []};
      return http.Response.bytes(
        utf8.encode(jsonEncode(body)),
        200,
        headers: {'content-type': 'application/json'},
      );
    });
    final api = ApiClient(
      'http://server',
      accessCode: 'code-1',
      client: client,
      operator: 'Айгуль',
      deviceInfo: _device,
    );

    await api.voice(Uint8List.fromList([1]), 'p.m4a', 'ru');
    await api.catalog();
    await api.post(
      id: '11111111-1111-1111-1111-111111111111',
      kind: 'receipt',
      skuId: 1,
      locationId: 10,
      qty: 1,
      operator: 'Айгуль',
      source: 'keyboard',
    );

    expect(
      seen.keys,
      containsAll(['/api/voice', '/api/catalog', '/api/movements']),
    );
    for (final h in seen.values) {
      expect(h['Authorization'], 'Bearer code-1');
      expect(Uri.decodeComponent(h['X-App-Version']!), '0.1.2+3');
      expect(Uri.decodeComponent(h['X-Device']!), 'Samsung A15');
      expect(Uri.decodeComponent(h['X-Operator']!), 'Айгуль');
      // Header values stay ASCII.
      for (final v in h.values) {
        expect(v.codeUnits.every((c) => c < 128), isTrue, reason: v);
      }
    }
  });

  test('unknown values are left out, not sent empty', () async {
    late Map<String, String> headers;
    final client = MockClient((req) async {
      headers = req.headers;
      return http.Response('{"skus":[],"locations":[]}', 200);
    });
    await ApiClient(
      'http://server',
      client: client,
      deviceInfo: _noDevice,
    ).catalog();
    expect(
      headers.keys.map((k) => k.toLowerCase()),
      isNot(
        anyOf(
          contains('x-app-version'),
          contains('x-device'),
          contains('x-operator'),
        ),
      ),
    );
  });
}
