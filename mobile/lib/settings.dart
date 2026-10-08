import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'l10n.dart';

/// Per-device settings. There is no login: the operator types their name once.
class AppSettings extends ChangeNotifier {
  AppSettings({this.serverUrl = '', this.accessCode = '', this.operator = '', this.lang = 'ru'});

  String serverUrl;

  /// Shared secret the server asks for when it is reachable from the internet.
  String accessCode;
  String operator;
  String lang;

  Strings get s => Strings(lang);

  bool get isComplete => serverUrl.isNotEmpty && operator.isNotEmpty;

  static Future<AppSettings> load() async {
    final p = await SharedPreferences.getInstance();
    return AppSettings(
      serverUrl: p.getString('serverUrl') ?? '',
      accessCode: p.getString('accessCode') ?? '',
      operator: p.getString('operator') ?? '',
      lang: p.getString('lang') ?? 'ru',
    );
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
