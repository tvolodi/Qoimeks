import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qoimeks/api.dart';
import 'package:qoimeks/operation_screen.dart';
import 'package:qoimeks/settings.dart';
import 'package:qoimeks/settings_screen.dart';
import 'package:qoimeks/update.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'operation_screen_test.dart' show FakeServer, FakeVoice;

const _url = 'https://ai-dala.com/qoimeks/latest.json';
const _apk = 'https://ai-dala.com/qoimeks/qoimeks-0.2.0.apk';

String manifest({
  int code = 3,
  int min = 1,
  Map<String, Object?>? notes,
  bool withUrl = true,
}) => jsonEncode({
  'versionCode': code,
  'versionName': '0.2.0',
  if (withUrl) 'url': _apk,
  'sha256': 'abc',
  'minVersionCode': min,
  'notes':
      notes ?? {'ru': 'Быстрее работает', 'kk': 'Жылдамырақ жұмыс істейді'},
  'published': '2026-10-20T10:00:00Z',
});

class Fixture {
  Fixture({
    this.body,
    this.error,
    String lang = 'ru',
    String url = _url,
    this.installed = 2,
    bool isWeb = false,
  }) {
    settings = AppSettings(
      serverUrl: 'http://server',
      operator: 'Айгуль',
      lang: lang,
    );
    checker = UpdateChecker(
      settings: settings,
      manifestUrl: url,
      fetch: (u) async {
        fetched.add(u);
        if (error != null) throw error!;
        return body!;
      },
      open: (u) async {
        opened.add(u);
        return true;
      },
      installed: () async => (name: '0.1.1', code: installed),
      now: () => clock,
      isWeb: isWeb,
    );
  }

  String? body;
  Object? error;
  int installed;
  late final AppSettings settings;
  late final UpdateChecker checker;
  final fetched = <Uri>[];
  final opened = <Uri>[];
  DateTime clock = DateTime.utc(2026, 10, 20, 10);
}

Widget bannerApp(Fixture f) => MaterialApp(
  home: Scaffold(body: UpdateBanner(checker: f.checker)),
);

Future<void> pumpOperation(
  WidgetTester tester,
  Fixture f,
  FakeServer server,
) async {
  tester.view.physicalSize = const Size(1080, 2400);
  tester.view.devicePixelRatio = 2.5;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    MaterialApp(
      home: OperationScreen(
        settings: f.settings,
        api: ApiClient('http://server', accessCode: 'c', client: server.client),
        voice: FakeVoice(),
        updates: f.checker,
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  group('on the web', () {
    test('the check makes no request and offers no update', () async {
      final f = Fixture(body: manifest(), isWeb: true);
      expect(f.checker.enabled, isFalse);
      await f.checker.checkOnStart();
      expect(await f.checker.checkNow(), UpdateResult.off);
      expect(f.fetched, isEmpty);
      expect(f.checker.available, isNull);
    });

    testWidgets('no banner and no check button', (tester) async {
      final f = Fixture(body: manifest(), isWeb: true);
      await f.checker.checkOnStart();
      await tester.pumpWidget(bannerApp(f));
      expect(find.byKey(const Key('update_banner')), findsNothing);
      await tester.pumpWidget(
        MaterialApp(
          home: SettingsScreen(settings: f.settings, updates: f.checker),
        ),
      );
      await tester.pump();
      expect(find.byKey(const Key('check_update')), findsNothing);
    });
  });

  group('parseManifest', () {
    test('reads a valid manifest', () {
      final i = parseManifest(manifest())!;
      expect(i.versionCode, 3);
      expect(i.url, _apk);
      expect(i.notesFor('kk'), 'Жылдамырақ жұмыс істейді');
      expect(i.notesFor('de'), 'Быстрее работает');
    });

    test('notes are optional', () {
      final i = parseManifest(
        jsonEncode({
          'versionCode': 3,
          'versionName': 'x',
          'url': _apk,
          'minVersionCode': 1,
        }),
      )!;
      expect(i.notesFor('ru'), '');
    });

    test('bad input gives null', () {
      expect(parseManifest('{bad'), isNull);
      expect(parseManifest(''), isNull);
      expect(parseManifest('[]'), isNull);
      for (final k in ['versionCode', 'versionName', 'url', 'minVersionCode']) {
        final j = jsonDecode(manifest()) as Map<String, dynamic>..remove(k);
        expect(parseManifest(jsonEncode(j)), isNull, reason: 'without $k');
      }
      final j = jsonDecode(manifest()) as Map<String, dynamic>
        ..['versionCode'] = '3';
      expect(parseManifest(jsonEncode(j)), isNull);
    });
  });

  group('banner', () {
    testWidgets('a newer version shows a dismissible banner with ru notes', (
      tester,
    ) async {
      final f = Fixture(body: manifest());
      await tester.pumpWidget(bannerApp(f));
      expect(find.byKey(const Key('update_banner')), findsNothing);
      await f.checker.checkOnStart();
      await tester.pump();
      expect(find.byKey(const Key('update_banner')), findsOneWidget);
      expect(find.textContaining('Доступна новая версия'), findsOneWidget);
      expect(find.text('Быстрее работает'), findsOneWidget);
      await tester.tap(find.byKey(const Key('update_dismiss')));
      await tester.pump();
      expect(find.byKey(const Key('update_banner')), findsNothing);
    });

    testWidgets('the notes follow the language (kk)', (tester) async {
      final f = Fixture(body: manifest(), lang: 'kk');
      await tester.pumpWidget(bannerApp(f));
      await f.checker.checkOnStart();
      await tester.pump();
      expect(find.text('Жылдамырақ жұмыс істейді'), findsOneWidget);
      expect(find.textContaining('Жаңа нұсқа бар'), findsOneWidget);
    });

    testWidgets('a code below minVersionCode cannot be dismissed', (
      tester,
    ) async {
      final f = Fixture(body: manifest(min: 3));
      await tester.pumpWidget(bannerApp(f));
      await f.checker.checkOnStart();
      await tester.pump();
      expect(find.textContaining('Требуется обновление'), findsOneWidget);
      expect(find.byKey(const Key('update_dismiss')), findsNothing);
      f.checker.dismiss();
      await tester.pump();
      expect(find.byKey(const Key('update_banner')), findsOneWidget);
    });

    testWidgets('the same or an older version shows nothing', (tester) async {
      for (final installed in [3, 4]) {
        final f = Fixture(
          body: manifest(code: 3, min: 3),
          installed: installed,
        );
        await tester.pumpWidget(bannerApp(f));
        await f.checker.checkOnStart();
        await tester.pump();
        expect(f.fetched, hasLength(1));
        expect(find.byKey(const Key('update_banner')), findsNothing);
      }
    });

    testWidgets('tapping the banner opens the manifest url', (tester) async {
      final f = Fixture(body: manifest());
      await tester.pumpWidget(bannerApp(f));
      await f.checker.checkOnStart();
      await tester.pump();
      await tester.tap(find.text('Быстрее работает'));
      await tester.pump();
      expect(f.opened, [Uri.parse(_apk)]);
    });
  });

  group('when it checks', () {
    test('on start, then at most once a day', () async {
      final f = Fixture(body: manifest());
      await f.checker.checkOnStart();
      expect(f.fetched, hasLength(1));
      expect(f.settings.lastUpdateCheck, f.clock.millisecondsSinceEpoch);
      f.clock = f.clock.add(const Duration(hours: 1));
      await f.checker.checkOnStart();
      expect(f.fetched, hasLength(1));
      f.clock = f.clock.add(const Duration(hours: 24));
      await f.checker.checkOnStart();
      expect(f.fetched, hasLength(2));
    });

    test('the time is kept in the stored settings', () async {
      final f = Fixture(body: manifest());
      await f.checker.checkOnStart();
      final loaded = await AppSettings.load();
      expect(loaded.lastUpdateCheck, f.clock.millisecondsSinceEpoch);
    });

    test('a failed check is not counted and is retried', () async {
      final f = Fixture(error: const SocketException('down'));
      await f.checker.checkOnStart();
      expect(f.settings.lastUpdateCheck, 0);
      await f.checker.checkOnStart();
      expect(f.fetched, hasLength(2));
    });

    test('checkNow ignores the daily limit', () async {
      final f = Fixture(body: manifest());
      await f.checker.checkOnStart();
      expect(await f.checker.checkNow(), UpdateResult.available);
      expect(f.fetched, hasLength(2));
    });

    test('an empty manifest url makes no request', () async {
      final f = Fixture(body: manifest(), url: '');
      expect(f.checker.enabled, isFalse);
      await f.checker.checkOnStart();
      expect(await f.checker.checkNow(), UpdateResult.off);
      expect(f.fetched, isEmpty);
    });
  });

  group('failures', () {
    final cases = <String, Fixture Function()>{
      'network': () => Fixture(error: const SocketException('down')),
      'timeout': () => Fixture(error: TimeoutException('slow')),
      'bad json': () => Fixture(body: '{bad'),
      'missing url': () => Fixture(body: manifest(withUrl: false)),
    };
    for (final e in cases.entries) {
      test(
        '${e.key}: nothing thrown, nothing shown, manual check says failed',
        () async {
          final f = e.value();
          await f.checker.checkOnStart();
          expect(f.checker.available, isNull);
          expect(await f.checker.checkNow(), UpdateResult.failed);
        },
      );

      testWidgets('${e.key}: the operation screen still works', (tester) async {
        final f = e.value();
        final server = FakeServer();
        await pumpOperation(tester, f, server);
        await f.checker.checkOnStart();
        await tester.pumpAndSettle();
        expect(find.byKey(const Key('update_banner')), findsNothing);
        expect(find.byKey(const Key('sku')), findsOneWidget);
        expect(find.byKey(const Key('post')), findsOneWidget);
      });
    }

    testWidgets('a banner sits above the form without hiding it', (
      tester,
    ) async {
      final f = Fixture(body: manifest());
      await pumpOperation(tester, f, FakeServer());
      await f.checker.checkOnStart();
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('update_banner')), findsOneWidget);
      expect(find.byKey(const Key('sku')), findsOneWidget);
    });
  });

  group('settings screen', () {
    Future<void> pumpSettings(WidgetTester tester, Fixture f) async {
    // The form got longer with the pairing section: a taller screen keeps Save and the version in view.
    tester.view.physicalSize = const Size(800, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
      await tester.pumpWidget(
        MaterialApp(
          home: SettingsScreen(settings: f.settings, updates: f.checker),
        ),
      );
      await tester.pumpAndSettle();
    }

    testWidgets('shows the version and the check button (ru)', (tester) async {
      final f = Fixture(body: manifest(code: 2));
      await pumpSettings(tester, f);
      expect(find.text('Версия приложения: 0.1.1 (2)'), findsOneWidget);
      expect(find.text('Проверить обновления'), findsOneWidget);
      await tester.tap(find.byKey(const Key('check_update')));
      await tester.pumpAndSettle();
      expect(f.fetched, hasLength(1));
      expect(find.text('Установлена последняя версия'), findsOneWidget);
    });

    testWidgets('in kk', (tester) async {
      final f = Fixture(body: manifest(code: 2), lang: 'kk');
      await pumpSettings(tester, f);
      expect(find.text('Қолданба нұсқасы: 0.1.1 (2)'), findsOneWidget);
      expect(find.text('Жаңартуларды тексеру'), findsOneWidget);
    });

    testWidgets('a failed manual check shows a message', (tester) async {
      final f = Fixture(body: '{bad');
      await pumpSettings(tester, f);
      await tester.tap(find.byKey(const Key('check_update')));
      await tester.pumpAndSettle();
      expect(find.text('Не удалось проверить обновления'), findsOneWidget);
    });

    testWidgets('no button when the check is off, the version stays', (
      tester,
    ) async {
      final f = Fixture(body: manifest(), url: '');
      await pumpSettings(tester, f);
      expect(find.byKey(const Key('check_update')), findsNothing);
      expect(find.text('Версия приложения: 0.1.1 (2)'), findsOneWidget);
    });
  });

  test('the Android manifest does not ask to install packages', () {
    final xml = File(
      'android/app/src/main/AndroidManifest.xml',
    ).readAsStringSync();
    expect(xml, isNot(contains('REQUEST_INSTALL_PACKAGES')));
  });
}
