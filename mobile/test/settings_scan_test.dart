import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qoimeks/l10n.dart';
import 'package:qoimeks/qr_scanner.dart';
import 'package:qoimeks/settings.dart';
import 'package:qoimeks/settings_screen.dart';
import 'package:shared_preferences/shared_preferences.dart';

class FakeScanner implements QrScanner {
  FakeScanner(this.result);
  final ScanResult result;
  int calls = 0;

  @override
  Future<ScanResult> scan(BuildContext context, Strings s) async {
    calls++;
    return result;
  }
}

const _secret = 'sEcr3t-code';

String _field(WidgetTester tester, String key) =>
    tester.widget<TextFormField>(find.byKey(Key(key))).controller!.text;

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  Future<AppSettings> pump(
    WidgetTester tester,
    ScanResult result, {
    String lang = 'ru',
  }) async {
    final settings = AppSettings(
      serverUrl: 'http://old',
      accessCode: 'oldcode',
      operator: 'Айгуль',
      lang: lang,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(settings: settings, scanner: FakeScanner(result)),
      ),
    );
    await tester.pumpAndSettle();
    return settings;
  }

  Future<void> tapScan(WidgetTester tester) async {
    await tester.ensureVisible(find.byKey(const Key('scan_qr')));
    await tester.tap(find.byKey(const Key('scan_qr')));
    await tester.pumpAndSettle();
  }

  testWidgets('a valid payload fills address and code, nothing saved yet', (
    tester,
  ) async {
    final settings = await pump(
      tester,
      const ScanText(
        'qoimeks://setup?url=http%3A%2F%2F10.0.0.5%3A8080&code=$_secret',
      ),
    );
    expect(find.text('Сканировать QR'), findsOneWidget);
    await tapScan(tester);
    expect(_field(tester, 'server'), 'http://10.0.0.5:8080');
    expect(_field(tester, 'access_code'), _secret);
    expect(settings.serverUrl, 'http://old');
    expect(find.text(const Strings('ru')('qr_filled')), findsOneWidget);
    expect(
      find.descendant(
        of: find.byType(SnackBar),
        matching: find.textContaining(_secret),
      ),
      findsNothing,
    );
  });

  testWidgets('a payload without code leaves the code field alone', (
    tester,
  ) async {
    await pump(
      tester,
      const ScanText('qoimeks://setup?url=http%3A%2F%2F10.0.0.5%3A8080'),
    );
    await tapScan(tester);
    expect(_field(tester, 'server'), 'http://10.0.0.5:8080');
    expect(_field(tester, 'access_code'), 'oldcode');
  });

  for (final lang in ['ru', 'kk']) {
    testWidgets('a foreign QR changes nothing and says so ($lang)', (
      tester,
    ) async {
      await pump(
        tester,
        const ScanText('https://example.com/?code=$_secret'),
        lang: lang,
      );
      await tapScan(tester);
      expect(_field(tester, 'server'), 'http://old');
      expect(_field(tester, 'access_code'), 'oldcode');
      expect(find.text(Strings(lang)('qr_invalid')), findsOneWidget);
      expect(find.textContaining(_secret), findsNothing);
    });
  }

  for (final lang in ['ru', 'kk']) {
    testWidgets(
      'permission denied: message, manual entry and save still work ($lang)',
      (tester) async {
        final settings = await pump(
          tester,
          const ScanPermissionDenied(),
          lang: lang,
        );
        await tapScan(tester);
        expect(
          find.text(Strings(lang)('qr_permission_denied')),
          findsOneWidget,
        );
        expect(_field(tester, 'server'), 'http://old');

        await tester.enterText(
          find.byKey(const Key('server')),
          'http://typed:1',
        );
        await tester.enterText(
          find.byKey(const Key('access_code')),
          'typed-code',
        );
        await tester.ensureVisible(find.byKey(const Key('save')));
        await tester.tap(find.byKey(const Key('save')));
        await tester.pumpAndSettle();
        expect(settings.serverUrl, 'http://typed:1');
        expect(settings.accessCode, 'typed-code');
      },
    );

    testWidgets('no camera: message and manual entry ($lang)', (tester) async {
      await pump(tester, const ScanNoCamera(), lang: lang);
      await tapScan(tester);
      expect(find.text(Strings(lang)('qr_no_camera')), findsOneWidget);
      await tester.enterText(find.byKey(const Key('server')), 'http://typed:2');
      expect(_field(tester, 'server'), 'http://typed:2');
    });
  }

  testWidgets('cancelled scan shows nothing and changes nothing', (
    tester,
  ) async {
    await pump(tester, const ScanCancelled());
    await tapScan(tester);
    expect(find.byType(SnackBar), findsNothing);
    expect(_field(tester, 'server'), 'http://old');
    expect(_field(tester, 'access_code'), 'oldcode');
  });

  test('every new message exists in both languages', () {
    for (final k in [
      'scan_qr',
      'qr_scan_title',
      'qr_invalid',
      'qr_filled',
      'qr_permission_denied',
      'qr_no_camera',
    ]) {
      expect(const Strings('ru')(k), isNot(k));
      expect(const Strings('kk')(k), isNot(k));
      expect(const Strings('kk')(k), isNot(const Strings('ru')(k)));
    }
  });
}
