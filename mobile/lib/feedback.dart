import 'dart:convert';
import 'dart:io' show Platform;

import 'package:device_info_plus/device_info_plus.dart';
import 'package:flutter/foundation.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart';
import 'settings.dart';

const maxLoggedErrors = 50;
const maxPendingReports = 20;
const redactedMark = '[redacted]';

typedef DeviceInfo = ({String appVersion, String androidVersion, String device});
typedef DeviceInfoReader = Future<DeviceInfo> Function();

Future<DeviceInfo> readDeviceInfo() async {
  try {
    final p = await PackageInfo.fromPlatform();
    final version = '${p.version}+${p.buildNumber}';
    if (kIsWeb || !Platform.isAndroid) {
      return (appVersion: version, androidVersion: '', device: kIsWeb ? 'web' : Platform.operatingSystem);
    }
    final a = await DeviceInfoPlugin().androidInfo;
    return (
      appVersion: version,
      androidVersion: a.version.release,
      device: '${a.manufacturer} ${a.model}'.trim(),
    );
  } catch (_) {
    return (appVersion: '', androidVersion: '', device: '');
  }
}

/// What the operator did with the form.
enum FeedbackOutcome { sent, savedForLater }

/// The app's last [maxLoggedErrors] errors, kept in shared preferences, and the reports
/// that could not be sent yet. Never holds the access code: it is replaced on the way in.
class FeedbackService {
  FeedbackService({
    required this.settings,
    required this.api,
    DeviceInfoReader? deviceInfo,
    DateTime Function()? now,
  })  : _deviceInfo = deviceInfo ?? readDeviceInfo,
        _now = now ?? DateTime.now;

  static const _errorsKey = 'errorLog';
  static const _pendingKey = 'pendingFeedback';

  final AppSettings settings;
  final ApiClient Function() api;
  final DeviceInfoReader _deviceInfo;
  final DateTime Function() _now;

  /// Name of the screen the operator is on; sent with a report and with logged errors.
  String screen = 'operation';

  bool _retrying = false;

  String redact(String v) {
    final code = settings.accessCode;
    if (code.isNotEmpty) v = v.replaceAll(code, redactedMark);
    return v.replaceAll(RegExp(r'Bearer\s+[A-Za-z0-9._~+/=-]+', caseSensitive: false), redactedMark);
  }

  Future<List<Map<String, dynamic>>> errors() async {
    final p = await SharedPreferences.getInstance();
    return _decode(p.getString(_errorsKey));
  }

  Future<void> logError(String message, {String? where}) async {
    final p = await SharedPreferences.getInstance();
    final list = _decode(p.getString(_errorsKey))
      ..add({
        'time': _now().toUtc().toIso8601String(),
        'screen': redact(where ?? screen),
        'message': redact(message),
      });
    final kept = list.length > maxLoggedErrors ? list.sublist(list.length - maxLoggedErrors) : list;
    await p.setString(_errorsKey, jsonEncode(kept));
  }

  /// Called by [ApiClient] for every failed call.
  void onApiError(String path, ApiException e) => logError('$path: ${e.status} ${e.message}');

  Future<List<Map<String, dynamic>>> pending() async {
    final p = await SharedPreferences.getInstance();
    return _decode(p.getString(_pendingKey));
  }

  Future<void> _savePending(List<Map<String, dynamic>> list) async {
    final p = await SharedPreferences.getInstance();
    await p.setString(_pendingKey, jsonEncode(list));
  }

  /// Sends the report; when the server cannot be reached it is kept to be sent later.
  /// Rethrows when the server refuses the report itself (HTTP 400).
  Future<FeedbackOutcome> submit(String text, {String? from}) async {
    final info = await _deviceInfo();
    final report = <String, dynamic>{
      'text': text.trim(),
      'voice_text': '',
      'app_version': info.appVersion,
      'android_version': info.androidVersion,
      'device': info.device,
      'operator': settings.operator,
      'screen': redact(from ?? screen),
      'errors': await errors(),
    };
    try {
      await api().postFeedback(report);
      return FeedbackOutcome.sent;
    } on ApiException catch (e) {
      if (e.status == 400) rethrow;
      final list = await pending()
        ..add(report);
      await _savePending(list.length > maxPendingReports ? list.sublist(list.length - maxPendingReports) : list);
      return FeedbackOutcome.savedForLater;
    }
  }

  /// Sends the kept reports, oldest first, each one once. Stops at the first failure.
  /// Called on app start and after a successful API call.
  Future<void> retryPending() async {
    if (_retrying) return;
    _retrying = true;
    try {
      while (true) {
        final list = await pending();
        if (list.isEmpty) return;
        try {
          await api().postFeedback(list.first);
        } on ApiException catch (e) {
          // A report the server refuses can never succeed: drop it rather than loop.
          if (e.status != 400) return;
        }
        // The first entry is removed before the next send, so a report goes out once.
        await _savePending((await pending())..removeAt(0));
      }
    } finally {
      _retrying = false;
    }
  }

  static List<Map<String, dynamic>> _decode(String? raw) {
    if (raw == null) return [];
    try {
      return [for (final e in jsonDecode(raw) as List) Map<String, dynamic>.from(e as Map)];
    } catch (_) {
      return [];
    }
  }
}
