import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qoimeks/design/theme.dart';
import 'package:qoimeks/design/tokens.dart';

void main() {
  final theme = buildTheme();

  test('the theme is built from the light tokens', () {
    expect(theme.colorScheme.primary, LightColors.primary);
    expect(theme.colorScheme.onPrimary, Colors.white);
    expect(theme.scaffoldBackgroundColor, LightColors.bg);
    expect(theme.colorScheme.brightness, Brightness.light);
  });

  test(
    'amber is not used by the colour scheme: it is only for the brand and the scanner frame',
    () {
      final scheme = theme.colorScheme;
      for (final c in [
        scheme.primary,
        scheme.secondary,
        scheme.tertiary,
        scheme.error,
        scheme.surface,
      ]) {
        expect(c, isNot(LightColors.brand));
      }
    },
  );

  testWidgets(
    'the main button is night blue with white text and at least 48 px high',
    (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: theme,
          home: Scaffold(
            body: Center(
              child: FilledButton(onPressed: () {}, child: const Text('OK')),
            ),
          ),
        ),
      );
      final style = theme.filledButtonTheme.style!;
      expect(style.backgroundColor!.resolve({}), LightColors.primary);
      expect(style.foregroundColor!.resolve({}), LightColors.onPrimary);
      expect(
        tester.getSize(find.byType(FilledButton)).height,
        greaterThanOrEqualTo(AppSize.touchMin),
      );
    },
  );

  testWidgets('an input is at least 52 px high', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: theme,
        home: const Scaffold(
          body: Center(
            child: TextField(decoration: InputDecoration(labelText: 'Имя')),
          ),
        ),
      ),
    );
    expect(
      tester.getSize(find.byType(TextField)).height,
      greaterThanOrEqualTo(AppSize.inputHeight),
    );
  });
}
