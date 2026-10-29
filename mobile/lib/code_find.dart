import 'package:flutter/material.dart';

import 'api.dart';
import 'design/status.dart';
import 'design/theme.dart';
import 'design/tokens.dart';
import 'l10n.dart';
import 'qr_scanner.dart';
import 'settings.dart';

/// "Find by code": scan a barcode or type a code (a keyboard-style reader types it and presses Enter),
/// look it up on the server and return what it points to. Nothing is changed here.
class CodeFindScreen extends StatefulWidget {
  const CodeFindScreen({
    super.key,
    required this.settings,
    required this.api,
    this.scanner = const CameraQrScanner(anyBarcode: true),
  });

  final AppSettings settings;
  final ApiClient api;
  final QrScanner scanner;

  @override
  State<CodeFindScreen> createState() => _CodeFindScreenState();
}

class _CodeFindScreenState extends State<CodeFindScreen> {
  final _input = TextEditingController();
  bool _busy = false;

  // What the last attempt said, shown as a banner under the input.
  StatusKind? _bannerKind;
  String? _banner;

  Strings get _s => widget.settings.s;

  @override
  void dispose() {
    _input.dispose();
    super.dispose();
  }

  void _show(StatusKind? kind, String? text) => setState(() {
    _bannerKind = kind;
    _banner = text;
  });

  Future<void> _scan() async {
    final result = await widget.scanner.scan(context, _s);
    if (!mounted) return;
    switch (result) {
      case ScanText(:final text):
        _input.text = text.trim();
        await _lookup();
      case ScanPermissionDenied():
        _show(StatusKind.warning, _s('code_camera_off'));
      case ScanNoCamera():
        _show(StatusKind.warning, _s('qr_no_camera'));
      case ScanCancelled():
        break;
    }
  }

  Future<void> _lookup() async {
    final code = _input.text.trim();
    if (code.isEmpty || _busy) return;
    setState(() => _busy = true);
    try {
      final found = await widget.api.lookupCode(code);
      if (!mounted) return;
      if (found != null) {
        Navigator.of(context).pop(found);
        return;
      }
      _show(StatusKind.info, _s('code_not_found').replaceAll('{code}', code));
    } catch (e) {
      if (mounted) _show(StatusKind.error, _s.error(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(_s('find_by_code'))),
      body: ListView(
        padding: const EdgeInsets.all(AppSpace.s4),
        children: [
          SizedBox(
            height: AppSize.touchMin,
            child: FilledButton.icon(
              key: const Key('code_scan'),
              onPressed: _busy ? null : _scan,
              icon: const Icon(Icons.qr_code_scanner),
              label: Text(_s('code_scan')),
            ),
          ),
          const SizedBox(height: AppSpace.s4),
          Text(
            _s('code_type_hint'),
            style: Theme.of(context).textTheme.bodyMedium,
          ),
          const SizedBox(height: AppSpace.s2),
          TextField(
            key: const Key('code_input'),
            controller: _input,
            style: monoStyle(),
            autocorrect: false,
            enableSuggestions: false,
            textInputAction: TextInputAction.search,
            onSubmitted: (_) => _lookup(),
            decoration: InputDecoration(labelText: _s('code_field')),
          ),
          const SizedBox(height: AppSpace.s2),
          SizedBox(
            height: AppSize.touchMin,
            child: OutlinedButton(
              key: const Key('code_find'),
              onPressed: _busy ? null : _lookup,
              child: _busy
                  ? const SizedBox(
                      key: Key('code_busy'),
                      width: 24,
                      height: 24,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Text(_s('code_find')),
            ),
          ),
          if (_banner != null) ...[
            const SizedBox(height: AppSpace.s4),
            StatusBanner(
              key: const Key('code_banner'),
              kind: _bannerKind!,
              text: _banner,
            ),
          ],
        ],
      ),
    );
  }
}
