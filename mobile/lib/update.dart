import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:package_info_plus/package_info_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import 'settings.dart';

/// Where the release manifest (`latest.json`) is read from. Set with
/// `--dart-define=UPDATE_MANIFEST_URL=...` at build time; empty turns the check off
/// (a Google Play build must not update itself).
const updateManifestUrl = String.fromEnvironment('UPDATE_MANIFEST_URL');

const _checkEvery = Duration(hours: 24);

class UpdateInfo {
  const UpdateInfo({
    required this.versionCode,
    required this.versionName,
    required this.url,
    required this.minVersionCode,
    this.notes = const {},
  });

  final int versionCode;
  final String versionName;
  final String url;
  final int minVersionCode;
  final Map<String, String> notes;

  /// The notes in [lang], else in Russian, else nothing.
  String notesFor(String lang) => notes[lang] ?? notes['ru'] ?? '';
}

/// The manifest, or null when it is not JSON or a required field is missing or of the wrong type.
UpdateInfo? parseManifest(String body) {
  try {
    final j = jsonDecode(body);
    if (j is! Map) return null;
    final code = j['versionCode'];
    final name = j['versionName'];
    final url = j['url'];
    final min = j['minVersionCode'];
    if (code is! int ||
        name is! String ||
        url is! String ||
        url.isEmpty ||
        min is! int)
      return null;
    final notes = <String, String>{};
    final n = j['notes'];
    if (n is Map) {
      for (final e in n.entries) {
        if (e.key is String && e.value is String)
          notes[e.key as String] = e.value as String;
      }
    }
    return UpdateInfo(
      versionCode: code,
      versionName: name,
      url: url,
      minVersionCode: min,
      notes: notes,
    );
  } catch (_) {
    return null;
  }
}

typedef ManifestFetcher = Future<String> Function(Uri url);
typedef UrlOpener = Future<bool> Function(Uri url);
typedef InstalledVersion = Future<({String name, int code})> Function();

Future<String> _httpFetch(Uri url) async {
  final r = await http.get(url).timeout(const Duration(seconds: 8));
  if (r.statusCode != 200)
    throw http.ClientException('HTTP ${r.statusCode}', url);
  return utf8.decode(r.bodyBytes);
}

Future<bool> _launchExternal(Uri url) =>
    launchUrl(url, mode: LaunchMode.externalApplication);

Future<({String name, int code})> _packageVersion() async {
  final p = await PackageInfo.fromPlatform();
  return (name: p.version, code: int.parse(p.buildNumber));
}

enum UpdateResult { off, upToDate, available, failed }

/// Reads the release manifest and tells whether a newer app version exists. Never throws:
/// a missing network or a broken manifest must not get in the way of the work.
class UpdateChecker extends ChangeNotifier {
  UpdateChecker({
    required this.settings,
    this.manifestUrl = updateManifestUrl,
    ManifestFetcher? fetch,
    UrlOpener? open,
    InstalledVersion? installed,
    DateTime Function()? now,
  }) : _fetch = fetch ?? _httpFetch,
       _open = open ?? _launchExternal,
       _installed = installed ?? _packageVersion,
       _now = now ?? DateTime.now;

  final AppSettings settings;
  final String manifestUrl;
  final ManifestFetcher _fetch;
  final UrlOpener _open;
  final InstalledVersion _installed;
  final DateTime Function() _now;

  UpdateInfo? available;
  bool mandatory = false;
  bool dismissed = false;
  bool _running = false;

  bool get enabled => manifestUrl.isNotEmpty;

  /// "0.1.1 (2)" for the settings screen.
  Future<String> installedLabel() async {
    try {
      final v = await _installed();
      return '${v.name} (${v.code})';
    } catch (_) {
      return '';
    }
  }

  /// The check on start: at most once per day.
  Future<void> checkOnStart() async {
    if (!enabled || _running) return;
    final last = settings.lastUpdateCheck;
    if (last > 0 &&
        _now().difference(DateTime.fromMillisecondsSinceEpoch(last)) <
            _checkEvery)
      return;
    await _run();
  }

  /// The "check for updates" button: ignores the daily limit.
  Future<UpdateResult> checkNow() async {
    if (!enabled) return UpdateResult.off;
    dismissed = false;
    return _run();
  }

  Future<UpdateResult> _run() async {
    _running = true;
    try {
      final info = parseManifest(await _fetch(Uri.parse(manifestUrl)));
      if (info == null) return UpdateResult.failed;
      final installed = (await _installed()).code;
      available = info.versionCode > installed ? info : null;
      mandatory = available != null && installed < info.minVersionCode;
      // Only a successful check counts, so a failed one is retried on the next start.
      await settings.markUpdateChecked(_now());
      return available == null ? UpdateResult.upToDate : UpdateResult.available;
    } catch (_) {
      return UpdateResult.failed;
    } finally {
      _running = false;
      notifyListeners();
    }
  }

  void dismiss() {
    if (mandatory) return;
    dismissed = true;
    notifyListeners();
  }

  /// Opens the APK address from the manifest in the browser.
  Future<void> openUpdate() async {
    final info = available;
    if (info == null) return;
    try {
      await _open(Uri.parse(info.url));
    } catch (_) {}
  }
}

/// The update notice at the top of the operation screen; empty when there is nothing to say.
class UpdateBanner extends StatelessWidget {
  const UpdateBanner({super.key, required this.checker});

  final UpdateChecker checker;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: checker,
      builder: (context, _) {
        final info = checker.available;
        if (info == null || checker.dismissed) return const SizedBox.shrink();
        final s = checker.settings.s;
        final notes = info.notesFor(checker.settings.lang);
        final scheme = Theme.of(context).colorScheme;
        final color = checker.mandatory
            ? scheme.errorContainer
            : scheme.primaryContainer;
        return Material(
          key: const Key('update_banner'),
          color: color,
          child: InkWell(
            onTap: checker.openUpdate,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 4, 8),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${s(checker.mandatory ? 'update_required' : 'update_available')}: ${info.versionName}',
                          style: const TextStyle(fontWeight: FontWeight.bold),
                        ),
                        if (notes.isNotEmpty) Text(notes),
                      ],
                    ),
                  ),
                  if (!checker.mandatory)
                    IconButton(
                      key: const Key('update_dismiss'),
                      icon: const Icon(Icons.close),
                      onPressed: checker.dismiss,
                    ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}
