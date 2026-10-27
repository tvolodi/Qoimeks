import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';

import '../tool/cache_bust.dart';

const _cfg =
    '_flutter.buildConfig = {"engineRevision":"x","builds":'
    '[{"compileTarget":"dart2js","renderer":"canvaskit","mainJsPath":"main.dart.js"},{}],'
    '"useLocalCanvasKit":true};';

void main() {
  test('replaces the path with the versioned one, rest unchanged', () {
    final out = bustMainJs(_cfg, 'abc123abc123');
    expect(out, contains('"mainJsPath":"main.dart.js?v=abc123abc123"'));
    expect(out.replaceAll('?v=abc123abc123', ''), _cfg);
  });

  test('replaces every occurrence', () {
    final out = bustMainJs('$_cfg\n$_cfg', 'aaaaaaaaaaaa');
    expect('?v=aaaaaaaaaaaa'.allMatches(out).length, 2);
    expect(out, isNot(contains('"mainJsPath":"main.dart.js"')));
  });

  test('throws when the pattern is missing', () {
    expect(() => bustMainJs('var x = 1;', 'abc'), throwsFormatException);
    expect(
      () => bustMainJs('"mainJsPath":"other.js"', 'abc'),
      throwsFormatException,
    );
  });

  test('build id is 12 hex chars and differs for different input', () {
    final a = buildId(utf8.encode('one'));
    final b = buildId(utf8.encode('two'));
    expect(a, matches(RegExp(r'^[0-9a-f]{12}$')));
    expect(a, isNot(b));
    expect(buildId(utf8.encode('one')), a);
  });
}
