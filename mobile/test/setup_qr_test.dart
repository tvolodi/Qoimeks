import 'package:flutter_test/flutter_test.dart';
import 'package:qoimeks/setup_qr.dart';

void main() {
  group('parseSetupQr', () {
    test('full payload', () {
      final p = parseSetupQr(
        'qoimeks://setup?url=http%3A%2F%2F10.0.0.5%3A8080&code=abc123',
      );
      expect(p?.url, 'http://10.0.0.5:8080');
      expect(p?.code, 'abc123');
    });

    test('payload without code', () {
      final p = parseSetupQr(
        'qoimeks://setup?url=https%3A%2F%2Fqoimeks.ai-dala.com',
      );
      expect(p?.url, 'https://qoimeks.ai-dala.com');
      expect(p?.code, isNull);
    });

    test('empty code counts as absent', () {
      final p = parseSetupQr('qoimeks://setup?url=http%3A%2F%2Fa.kz&code=');
      expect(p?.code, isNull);
    });

    test('url-encoded characters, surrounding whitespace and scheme case', () {
      final code = 'a&b+c ё/=';
      final raw =
          ' QOIMEKS://setup?url=${Uri.encodeQueryComponent('http://h.kz:8080/x')}'
          '&code=${Uri.encodeQueryComponent(code)}\n';
      final p = parseSetupQr(raw);
      expect(p?.url, 'http://h.kz:8080/x');
      expect(p?.code, code);
      expect(
        parseSetupQr('qoimeks://setup?url=http%3A%2F%2Fh.kz&code=x%2By')?.code,
        'x+y',
      );
    });

    test('wrong scheme or host', () {
      expect(
        parseSetupQr('https://qoimeks.ai-dala.com/?url=http%3A%2F%2Fh.kz'),
        isNull,
      );
      expect(parseSetupQr('qoimeks://other?url=http%3A%2F%2Fh.kz'), isNull);
    });

    test('missing, empty or unusable url', () {
      expect(parseSetupQr('qoimeks://setup?code=abc'), isNull);
      expect(parseSetupQr('qoimeks://setup?url=&code=abc'), isNull);
      expect(parseSetupQr('qoimeks://setup?url=10.0.0.5%3A8080'), isNull);
      expect(parseSetupQr('qoimeks://setup?url=ftp%3A%2F%2Fh.kz'), isNull);
      expect(parseSetupQr('qoimeks://setup?url=http%3A%2F%2F'), isNull);
    });

    test('garbage input', () {
      for (final raw in [
        '',
        '   ',
        'hello',
        'qoimeks://',
        'qoimeks://setup',
        '%%%',
        'qoimeks://setup?url=%zz',
      ]) {
        expect(parseSetupQr(raw), isNull, reason: raw);
      }
    });

    test('toString hides the code', () {
      final p = parseSetupQr(
        'qoimeks://setup?url=http%3A%2F%2Fh.kz&code=secret99',
      )!;
      expect(p.toString(), isNot(contains('secret99')));
    });
  });
}
