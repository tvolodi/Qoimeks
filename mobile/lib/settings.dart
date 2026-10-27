import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'l10n.dart';

/// Per-device settings. There is no login: the operator types their name once.
/// Server address a build prefills in the settings form (production web build); empty = none.
const defaultServerUrl = String.fromEnvironment('DEFAULT_SERVER_URL');

/// What the server field starts with: the saved address, else the build's default, else `http://`.
String initialServerText(String saved, [String define = defaultServerUrl]) {
  if (saved.isNotEmpty) return saved;
  return define.isNotEmpty ? define : 'http://';
}

class AppSettings extends ChangeNotifier {
  AppSettings({
    this.serverUrl = '',
    this.accessCode = '',
    this.operator = '',
    this.lang = 'ru',
    this.lastUpdateCheck = 0,
  });

  String serverUrl;

  /// Shared secret the server asks for when it is reachable from the internet.
  String accessCode;
  String operator;
  String lang;

  /// When the update manifest was last read successfully (ms since epoch, 0 = never).
  int lastUpdateCheck;

  Strings get s => Strings(lang);

  bool get isComplete => serverUrl.isNotEmpty && operator.isNotEmpty;

  static Future<AppSettings> load() async {
    final p = await SharedPreferences.getInstance();
    return AppSettings(
      serverUrl: p.getString('serverUrl') ?? '',
      accessCode: p.getString('accessCode') ?? '',
      operator: p.getString('operator') ?? '',
      lang: p.getString('lang') ?? 'ru',
      lastUpdateCheck: p.getInt('lastUpdateCheck') ?? 0,
    );
  }

  /// Not a notifying change: nothing on screen depends on it.
  Future<void> markUpdateChecked(DateTime t) async {
    lastUpdateCheck = t.millisecondsSinceEpoch;
    final p = await SharedPreferences.getInstance();
    await p.setInt('lastUpdateCheck', lastUpdateCheck);
  }

  Future<void> update({
    required String serverUrl,
    required String accessCode,
    required String operator,
    required String lang,
  }) async {
    this.serverUrl = serverUrl.trim();
    this.accessCode = accessCode.trim();
    this.operator = operator.trim();
    this.lang = lang;
    notifyListeners();
    final p = await SharedPreferences.getInstance();
    await p.setString('serverUrl', this.serverUrl);
    await p.setString('accessCode', this.accessCode);
    await p.setString('operator', this.operator);
    await p.setString('lang', this.lang);
  }
}
