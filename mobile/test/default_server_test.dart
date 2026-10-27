import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qoimeks/settings.dart';
import 'package:qoimeks/settings_screen.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('initialServerText: saved wins, then define, then http://', () {
    expect(initialServerText('http://saved', 'https://d'), 'http://saved');
    expect(initialServerText('', 'https://d'), 'https://d');
    expect(initialServerText('', ''), 'http://');
  });

  testWidgets('without the define the empty server field starts with http://', (
    tester,
  ) async {
    expect(defaultServerUrl, isEmpty);
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(settings: AppSettings(), firstRun: true),
      ),
    );
    await tester.pumpAndSettle();
    final field = tester.widget<TextFormField>(find.byKey(const Key('server')));
    expect(field.controller!.text, 'http://');
  });
}
