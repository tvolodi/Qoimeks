import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qoimeks/api.dart';
import 'package:qoimeks/design/theme.dart';
import 'package:qoimeks/design/tokens.dart';
import 'package:qoimeks/history_screen.dart';
import 'package:qoimeks/operation_screen.dart';
import 'package:qoimeks/picker.dart';
import 'package:qoimeks/qr_scanner.dart';
import 'package:qoimeks/settings.dart';
import 'package:qoimeks/settings_screen.dart';
import 'package:qoimeks/update.dart';
import 'package:qoimeks/voice.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Where the images are compared with (or written to by `--update-goldens`).
/// `--dart-define=GOLDEN_DIR=../../docs/design-screens/before` selects another folder.
const _dir = String.fromEnvironment(
  'GOLDEN_DIR',
  defaultValue: '../../docs/design-screens/after',
);

/// Test fonts: the bundled Onest and JetBrains Mono (and the SDK's Material icons), so the
/// images do not depend on the fonts installed on the machine.
Future<void> _loadFonts() async {
  Future<void> load(String family, String path) async {
    final loader = FontLoader(family)
      ..addFont(Future.value(ByteData.view(File(path).readAsBytesSync().buffer)));
    await loader.load();
  }

  final root = Platform.environment['FLUTTER_ROOT'];
  if (root != null) {
    await load(
      'MaterialIcons',
      '$root/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf',
    );
  }
  await load(AppFonts.sans, 'assets/fonts/Onest-Variable.ttf');
  await load(AppFonts.mono, 'assets/fonts/JetBrainsMono-Variable.ttf');
}

http.Response _json(Object body) => http.Response.bytes(
  utf8.encode(jsonEncode(body)),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

Map<String, dynamic> _movement(
  String id,
  String kind,
  num qty,
  String at, {
  String? to,
  String? reversedBy,
}) => {
  'id': id,
  'kind': kind,
  'sku_code': 'NUT-M8',
  'sku_name': 'Гайка М8',
  'unit': 'шт',
  'location_code': 'A-01',
  'to_location_code': to,
  'qty': qty,
  'operator': 'Айгуль',
  'reversed_by': reversedBy,
  'created_at': at,
  'balances': [],
};

final _client = MockClient((req) async {
  switch (req.url.path) {
    case '/api/catalog':
      return _json({
        'skus': [
          {
            'id': 1,
            'code': 'NUT-M8',
            'name': 'Гайка М8',
            'unit': 'шт',
            'location_id': 10,
            'alt_location_id': 11,
          },
          {'id': 2, 'code': 'NUT-M10', 'name': 'Гайка М10', 'unit': 'шт'},
          {'id': 3, 'code': 'BOLT-M8x40', 'name': 'Болт М8×40', 'unit': 'шт'},
        ],
        'locations': [
          {'id': 10, 'code': 'A-01', 'name': 'Ряд A'},
          {'id': 11, 'code': 'B-02', 'name': 'Ряд B'},
        ],
      });
    case '/api/balances':
      return _json([
        {'sku_id': 1, 'location_id': 10, 'location_code': 'A-01', 'qty': 5},
        {
          'sku_id': 1,
          'location_id': 11,
          'location_code': 'B-02',
          'qty': 1250.5,
        },
      ]);
    case '/api/movements':
      return _json([
        _movement('4', 'transfer', 20, '2026-10-26T14:30:00Z', to: 'B-02'),
        _movement('3', 'issue', 3, '2026-10-26T12:10:00Z', reversedBy: '5'),
        _movement('2', 'receipt', 100, '2026-10-26T09:05:00Z'),
        _movement('1', 'adjustment', 12.5, '2026-10-25T16:40:00Z'),
      ]);
  }
  return _json({'error': 'not found'});
});

class _NoVoice implements VoiceCapture {
  @override
  Future<bool> start() async => true;
  @override
  Future<Recording?> stop() async => null;
  @override
  void dispose() {}
}

void main() {
  setUpAll(_loadFonts);
  setUp(() => SharedPreferences.setMockInitialValues({}));

  Future<void> phone(WidgetTester tester, Widget home) async {
    tester.view.physicalSize = const Size(360, 800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(),
        debugShowCheckedModeBanner: false,
        home: home,
      ),
    );
    await tester.pumpAndSettle();
  }

  final settings = AppSettings(serverUrl: 'http://server', operator: 'Айгуль');
  ApiClient api() =>
      ApiClient('http://server', accessCode: 'code-1', client: _client);
  UpdateChecker checker({String? manifest}) => UpdateChecker(
    settings: settings,
    manifestUrl: 'http://m/latest.json',
    fetch: (_) async => manifest ?? '{}',
    installed: () async => (name: '0.1.1', code: 2),
  );

  testWidgets('operation screen', (tester) async {
    await phone(
      tester,
      OperationScreen(settings: settings, api: api(), voice: _NoVoice()),
    );
    await tester.tap(find.byKey(const Key('kind_issue')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('sku')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Гайка М8'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('qty')), '12,5');
    await tester.pumpAndSettle();
    await expectLater(
      find.byType(MaterialApp),
      matchesGoldenFile('$_dir/operation.png'),
    );
  });

  testWidgets('operation screen with an update banner', (tester) async {
    final updates = checker(
      manifest: jsonEncode({
        'versionCode': 9,
        'versionName': '0.2.0',
        'url': 'http://m/q.apk',
        'minVersionCode': 1,
        'notes': {'ru': 'Новая версия: быстрее и понятнее.'},
      }),
    );
    await updates.checkNow();
    await phone(
      tester,
      OperationScreen(
        settings: settings,
        api: api(),
        voice: _NoVoice(),
        updates: updates,
      ),
    );
    await expectLater(
      find.byType(MaterialApp),
      matchesGoldenFile('$_dir/operation_update.png'),
    );
  });

  testWidgets('history screen', (tester) async {
    await phone(tester, HistoryScreen(settings: settings, api: api()));
    await expectLater(
      find.byType(MaterialApp),
      matchesGoldenFile('$_dir/history.png'),
    );
  });

  testWidgets('settings screen', (tester) async {
    await phone(tester, SettingsScreen(settings: settings, updates: checker()));
    await expectLater(
      find.byType(MaterialApp),
      matchesGoldenFile('$_dir/settings.png'),
    );
  });

  testWidgets('picker screen', (tester) async {
    late BuildContext ctx;
    await phone(
      tester,
      Builder(
        builder: (c) {
          ctx = c;
          return const SizedBox();
        },
      ),
    );
    pick<String>(
      ctx,
      title: 'Номенклатура',
      searchHint: 'Поиск',
      emptyText: 'Ничего не найдено',
      items: const ['NUT-M8', 'NUT-M10', 'BOLT-M8x40'],
      titleOf: (s) => s,
      subtitleOf: (s) => 'шт',
    );
    await tester.pumpAndSettle();
    await expectLater(
      find.byType(MaterialApp),
      matchesGoldenFile('$_dir/picker.png'),
    );
  });

  testWidgets('scanner frame', (tester) async {
    await phone(
      tester,
      Scaffold(
        appBar: AppBar(title: const Text('Сканировать QR')),
        body: const Stack(
          fit: StackFit.expand,
          children: [
            ColoredBox(color: Color(0xFF202020)),
            Center(child: ScannerFrame()),
          ],
        ),
      ),
    );
    await expectLater(
      find.byType(MaterialApp),
      matchesGoldenFile('$_dir/scanner_frame.png'),
    );
  });

  // The scanner page needs the camera plugin (MobileScanner), which does not exist in a
  // widget test, so only its frame is rendered (above), over a dark stand-in for the camera picture.
}
