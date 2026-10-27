import 'package:flutter/material.dart';

import 'design/status.dart';
import 'design/theme.dart';
import 'design/tokens.dart';
import 'l10n.dart';
import 'qr_scanner.dart';
import 'setup_qr.dart';
import 'settings.dart';
import 'update.dart';

/// Shown on first launch (nothing works without a server and a name) and from the gear icon.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.settings,
    this.firstRun = false,
    this.updates,
    this.scanner = const CameraQrScanner(),
  });

  final AppSettings settings;
  final UpdateChecker? updates;
  final QrScanner scanner;
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
        final payload = parseSetupQr(text);
        if (payload == null) {
          message = 'qr_invalid';
        } else {
          _server.text = payload.url;
          if (payload.code != null) _accessCode.text = payload.code!;
          message = 'qr_filled';
          kind = StatusKind.success;
        }
      case ScanPermissionDenied():
        message = 'qr_permission_denied';
      case ScanNoCamera():
        message = 'qr_no_camera';
      case ScanCancelled():
        message = null;
    }
    if (message != null) {
      showStatusSnack(context, s(message), kind);
    }
  }

  @override
  void dispose() {
    _server.dispose();
    _accessCode.dispose();
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
