import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('manifest.json carries the Qoimeks name, colours and description', () {
    final m = jsonDecode(File('web/manifest.json').readAsStringSync()) as Map;
    expect(m['name'], 'Qoimeks');
    expect(m['short_name'], 'Qoimeks');
    expect(m['theme_color'], '#14304f');
    expect(m['background_color'], '#f6f1e7');
    expect(m['display'], 'standalone');
    expect(m['description'], isNot(contains('Flutter')));
    expect((m['description'] as String).trim(), isNotEmpty);
    expect(m['icons'], isNotEmpty);
  });

  test('index.html carries the Qoimeks title and no Flutter default', () {
    final h = File('web/index.html').readAsStringSync();
    expect(h, contains('<title>Qoimeks</title>'));
    expect(h, contains('name="apple-mobile-web-app-title" content="Qoimeks"'));
    expect(h, isNot(contains('A new Flutter project')));
  });
}
