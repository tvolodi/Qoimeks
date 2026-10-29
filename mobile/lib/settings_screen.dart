import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'api.dart';
import 'design/status.dart';
import 'design/theme.dart';
import 'design/tokens.dart';
import 'feedback.dart';
import 'feedback_screen.dart';
import 'l10n.dart';
import 'qr_scanner.dart';
import 'setup_qr.dart';
import 'settings.dart';
import 'update.dart';

ApiClient _defaultPairClient(String baseUrl) => ApiClient(baseUrl);

/// Upper-cases what is typed into the pairing code field.
class _UpperCase extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(TextEditingValue old, TextEditingValue next) =>
      next.copyWith(text: next.text.toUpperCase());
}

bool _isPlainHttp(String url) {
  final v = url.trim().toLowerCase();
  return v.startsWith('http://') && v.length > 'http://'.length;
}

/// Said on every screen while the server refuses this phone's revoked device key. It cannot be
/// dismissed; it goes away when the phone is paired again. [onConnect] opens the pairing form.
class DeviceRevokedBanner extends StatelessWidget {
  const DeviceRevokedBanner({super.key, required this.settings, this.onConnect});

  final AppSettings settings;
  final VoidCallback? onConnect;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: settings,
      builder: (context, _) {
        if (!settings.deviceRevoked) return const SizedBox.shrink();
        final s = settings.s;
        return Padding(
          key: const Key('device_revoked_banner'),
          padding: const EdgeInsets.all(AppSpace.s3),
          child: StatusBanner(
            kind: StatusKind.error,
            text: s('device_revoked'),
            trailing: onConnect == null
                ? null
                : TextButton(
                    key: const Key('device_revoked_connect'),
                    onPressed: onConnect,
                    child: Text(s('pair_again')),
                  ),
          ),
        );
      },
    );
  }
}

/// Shown on first launch (nothing works without a server and a name) and from the gear icon.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.settings,
    this.firstRun = false,
    this.updates,
    this.feedback,
    this.scanner = const CameraQrScanner(),
    this.pairClient = _defaultPairClient,
    this.deviceInfo = readDeviceInfo,
  });

  final AppSettings settings;
  final UpdateChecker? updates;
  final FeedbackService? feedback;
  final QrScanner scanner;

  /// Builds the client for the pairing exchange; tests pass one with a fake HTTP layer.
  final ApiClient Function(String baseUrl) pairClient;
  final DeviceInfoReader deviceInfo;
  final bool firstRun;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final _form = GlobalKey<FormState>();
  late final _server = TextEditingController(
    text: initialServerText(widget.settings.serverUrl),
  );
  late final _accessCode = TextEditingController(
    text: widget.settings.accessCode,
  );
  late final _operator = TextEditingController(text: widget.settings.operator);
  late String _lang = widget.settings.lang;
  final _pairCode = TextEditingController();
  bool _pairing = false;
  String? _pairLabel;
  String? _pairError;

  @override
  void initState() {
    super.initState();
    _accessCode.addListener(() => setState(() {}));
    _server.addListener(() => setState(() {}));
  }

  bool get _hasDeviceKey => _accessCode.text.trim().startsWith('qdk_');

  /// Trades [code] for this phone's device key at [url], then saves address and key at once.
  Future<void> _exchange(Strings s, String url, String code) async {
    final target = Uri.tryParse(url.trim());
    if (target == null || target.host.isEmpty) {
      setState(() => _pairError = s('required'));
      return;
    }
    if (code.trim().isEmpty) {
      setState(() => _pairError = s('required'));
      return;
    }
    setState(() {
      _pairing = true;
      _pairError = null;
    });
    final String label;
    final String version;
    try {
      final info = await widget.deviceInfo();
      label = info.device.isEmpty ? 'Android' : info.device;
      version = info.appVersion;
    } catch (_) {
      return _pairFailed(s, const ApiException(0, 'device info'));
    }
    try {
      final r = await widget.pairClient(url.trim()).pairExchange(
            code: code.trim(),
            deviceLabel: label,
            operator: _operator.text,
            appVersion: version,
          );
      if (!mounted) return;
      _server.text = url.trim();
      _accessCode.text = r.deviceKey;
      await widget.settings.update(
        serverUrl: _server.text,
        accessCode: r.deviceKey,
        operator: _operator.text,
        lang: widget.settings.lang,
      );
      if (!mounted) return;
      setState(() {
        _pairing = false;
        _pairLabel = label;
        _pairCode.clear();
      });
    } catch (e) {
      _pairFailed(s, e);
    }
  }

  void _pairFailed(Strings s, Object e) {
    if (!mounted) return;
    setState(() {
      _pairing = false;
      _pairError = s.pairError(e);
    });
  }

  Future<void> _checkUpdates(Strings s) async {
    final result = await widget.updates!.checkNow();
    if (!mounted) return;
    showStatusSnack(
      context,
      s(switch (result) {
        UpdateResult.available => 'update_available',
        UpdateResult.failed => 'update_check_failed',
        _ => 'update_up_to_date',
      }),
      switch (result) {
        UpdateResult.available => StatusKind.info,
        UpdateResult.failed => StatusKind.warning,
        _ => StatusKind.success,
      },
    );
  }

  Future<void> _scan(Strings s) async {
    final result = await widget.scanner.scan(context, s);
    if (!mounted) return;
    final String? message;
    var kind = StatusKind.error;
    switch (result) {
      case ScanText(:final text):
        final payload = parseScannedQr(text);
        if (payload is PairPayload) {
          _server.text = payload.url;
          message = null;
          await _exchange(s, payload.url, payload.code);
        } else if (payload is SetupPayload) {
          _server.text = payload.url;
          if (payload.code != null) _accessCode.text = payload.code!;
          message = 'qr_filled';
          kind = StatusKind.success;
        } else {
          message = 'qr_invalid';
        }
      case ScanPermissionDenied():
        message = 'qr_permission_denied';
      case ScanNoCamera():
        message = 'qr_no_camera';
      case ScanCancelled():
        message = null;
    }
    if (message != null && mounted) {
      showStatusSnack(context, s(message), kind);
    }
  }

  @override
  void dispose() {
    _server.dispose();
    _accessCode.dispose();
    _pairCode.dispose();
    _operator.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (!_form.currentState!.validate()) return;
    final navigator = Navigator.of(context);
    await widget.settings.update(
      serverUrl: _server.text,
      accessCode: _accessCode.text,
      operator: _operator.text,
      lang: _lang,
    );
    if (!widget.firstRun) navigator.pop();
  }

  @override
  Widget build(BuildContext context) {
    // Follows the language being chosen, before it is saved.
    final s = AppSettings(lang: _lang).s;
    String? required(String? v) =>
        (v == null || v.trim().isEmpty) ? s('required') : null;
    return Scaffold(
      appBar: AppBar(title: Text(s('settings'))),
      body: Form(
        key: _form,
        child: ListView(
          padding: const EdgeInsets.all(AppSpace.s4),
          children: [
            DeviceRevokedBanner(settings: widget.settings),
            SegmentedButton<String>(
              segments: const [
                ButtonSegment(value: 'ru', label: Text('Русский')),
                ButtonSegment(value: 'kk', label: Text('Қазақша')),
              ],
              selected: {_lang},
              onSelectionChanged: (v) => setState(() => _lang = v.single),
            ),
            const SizedBox(height: AppSpace.s4),
            TextFormField(
              key: const Key('operator'),
              controller: _operator,
              textCapitalization: TextCapitalization.words,
              decoration: InputDecoration(
                labelText: s('operator'),
              ),
              validator: required,
            ),
            const SizedBox(height: AppSpace.s4),
            TextFormField(
              key: const Key('server'),
              controller: _server,
              style: monoStyle(Theme.of(context).textTheme.bodyLarge),
              keyboardType: TextInputType.url,
              autocorrect: false,
              decoration: InputDecoration(
                labelText: s('server'),
                hintText: 'http://192.168.1.10:8080',
              ),
              validator: (v) {
                final uri = Uri.tryParse((v ?? '').trim());
                return uri != null && uri.host.isNotEmpty
                    ? null
                    : s('required');
              },
            ),
            const SizedBox(height: AppSpace.s4),
            TextFormField(
              key: const Key('access_code'),
              controller: _accessCode,
              style: monoStyle(Theme.of(context).textTheme.bodyLarge),
              autocorrect: false,
              enableSuggestions: false,
              obscureText: _hasDeviceKey,
              decoration: InputDecoration(
                labelText: s('access_code'),
                helperText: s('access_code_hint'),
              ),
            ),
            const SizedBox(height: AppSpace.s2),
            OutlinedButton.icon(
              key: const Key('scan_qr'),
              onPressed: () => _scan(s),
              icon: const Icon(Icons.qr_code_scanner),
              label: Text(s('scan_qr')),
            ),
            const SizedBox(height: AppSpace.s6),
            SizedBox(
              height: AppSize.inputHeight,
              child: FilledButton(
                key: const Key('save'),
                onPressed: _save,
                child: Text(s('save')),
              ),
            ),
            const SizedBox(height: AppSpace.s6),
            Text(s('pair_title'), style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: AppSpace.s2),
            TextField(
              key: const Key('pair_code'),
              controller: _pairCode,
              style: monoStyle(Theme.of(context).textTheme.bodyLarge),
              textCapitalization: TextCapitalization.characters,
              autocorrect: false,
              enableSuggestions: false,
              inputFormatters: [
                FilteringTextInputFormatter.allow(RegExp(r'[A-Za-z0-9-]')),
                _UpperCase(),
              ],
              decoration: InputDecoration(
                labelText: s('pair_code'),
                hintText: 'K7M2-9QXA',
              ),
            ),
            const SizedBox(height: AppSpace.s2),
            SizedBox(
              height: AppSize.inputHeight,
              child: FilledButton(
                key: const Key('pair_connect'),
                onPressed: _pairing ? null : () => _exchange(s, _server.text, _pairCode.text),
                child: _pairing
                    ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                    : Text(s('pair_connect')),
              ),
            ),
            if (_isPlainHttp(_server.text)) ...[
              const SizedBox(height: AppSpace.s2),
              StatusBanner(key: const Key('pair_insecure'), kind: StatusKind.warning, text: s('pair_insecure')),
            ],
            if (_pairError != null) ...[
              const SizedBox(height: AppSpace.s2),
              StatusBanner(key: const Key('pair_error'), kind: StatusKind.error, text: _pairError),
            ],
            if (_hasDeviceKey) ...[
              const SizedBox(height: AppSpace.s2),
              StatusBanner(
                key: const Key('pair_status'),
                kind: StatusKind.success,
                text: _pairLabel == null
                    ? s('pair_status_plain')
                    : s('pair_status').replaceFirst('{label}', _pairLabel!),
              ),
            ],
            if (widget.feedback != null) ...[
              const SizedBox(height: AppSpace.s6),
              OutlinedButton.icon(
                key: const Key('report_problem'),
                onPressed: () => Navigator.of(context).push(MaterialPageRoute(
                  builder: (_) => FeedbackScreen(
                    settings: widget.settings,
                    feedback: widget.feedback!,
                    from: 'settings',
                  ),
                )),
                icon: const Icon(Icons.feedback_outlined),
                label: Text(s('report_problem')),
              ),
            ],
            if (widget.updates != null) ...[
              const SizedBox(height: AppSpace.s6),
              FutureBuilder<String>(
                future: widget.updates!.installedLabel(),
                builder: (context, snap) => Text.rich(
                  key: const Key('app_version'),
                  TextSpan(children: [
                    TextSpan(text: '${s('app_version')}: '),
                    TextSpan(text: snap.data ?? '', style: monoStyle()),
                  ]),
                ),
              ),
              if (widget.updates!.enabled) ...[
                const SizedBox(height: AppSpace.s2),
                OutlinedButton(
                  key: const Key('check_update'),
                  onPressed: () => _checkUpdates(s),
                  child: Text(s('check_updates')),
                ),
              ],
            ],
          ],
        ),
      ),
    );
  }
}
