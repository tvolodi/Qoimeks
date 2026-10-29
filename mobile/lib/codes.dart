import 'package:flutter/material.dart';
import 'package:uuid/uuid.dart';

import 'api.dart';
import 'design/status.dart';
import 'design/theme.dart';
import 'design/tokens.dart';
import 'l10n.dart';
import 'qr_scanner.dart';
import 'settings.dart';

const codeTypes = ['ean13', 'ean8', 'code128', 'qr', 'manual'];

bool _eanOk(String digits) {
  var sum = 0;
  for (var i = 0; i < digits.length - 1; i++) {
    final d = digits.codeUnitAt(i) - 48;
    // From the right, the first digit after the check digit weighs 3, the next 1, and so on.
    sum += ((digits.length - 1 - i) % 2 == 1) ? d * 3 : d;
  }
  return (10 - sum % 10) % 10 == digits.codeUnitAt(digits.length - 1) - 48;
}

/// The code type for a scanned or typed value: EAN only when the check digit is right,
/// otherwise `manual` (the server stores it as read).
String guessCodeType(String code) {
  if (RegExp(r'^\d{13}$').hasMatch(code) && _eanOk(code)) return 'ean13';
  if (RegExp(r'^\d{8}$').hasMatch(code) && _eanOk(code)) return 'ean8';
  return 'manual';
}

/// A message for a failed codes call, built from `reason` and `owner` (not the server's English line).
String codeErrorText(Strings s, Object e, String code) {
  if (e is ApiException) {
    if (e.reason == 'code_in_use') {
      final o = e.owner;
      return s('code_in_use')
          .replaceAll('{code}', code)
          .replaceAll('{owner}', o == null ? '' : _ownerName(o));
    }
    if (e.reason == 'invalid_code') return s('code_invalid');
    if (e.reason == 'object_not_found') return s('code_object_gone');
  }
  return s.error(e);
}

String _ownerName(CodeOwner o) =>
    o.objectName.isEmpty ? o.objectCode : o.objectName;

String _objectTitle(String type, String code, String name) =>
    type == 'sku' ? name : code;

/// The codes of one SKU or location: list, add, remove (soft; the server keeps the history).
class CodesScreen extends StatefulWidget {
  const CodesScreen({
    super.key,
    required this.settings,
    required this.api,
    required this.objectType,
    required this.objectId,
    required this.title,
    this.scanner = const CameraQrScanner(anyBarcode: true),
  });

  final AppSettings settings;
  final ApiClient api;
  final String objectType;
  final int objectId;

  /// What the codes belong to, shown in the app bar and the remove question.
  final String title;
  final QrScanner scanner;

  @override
  State<CodesScreen> createState() => _CodesScreenState();
}

class _CodesScreenState extends State<CodesScreen> {
  late Future<List<ItemCode>> _codes = widget.api.codes(
    widget.objectType,
    widget.objectId,
  );

  Strings get _s => widget.settings.s;

  void _reload() {
    setState(() {
      _codes = widget.api.codes(widget.objectType, widget.objectId);
    });
  }

  Future<void> _add() async {
    final saved = await Navigator.of(context).push<bool>(
      MaterialPageRoute(
        builder: (_) => CodeAssignScreen(
          settings: widget.settings,
          api: widget.api,
          objectType: widget.objectType,
          objectId: widget.objectId,
          title: widget.title,
          scanner: widget.scanner,
        ),
      ),
    );
    if (!mounted) return;
    if (saved == true) {
      showStatusSnack(context, _s('code_saved'), StatusKind.success);
      _reload();
    }
  }

  Future<void> _remove(ItemCode c) async {
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(
          _s(
            'code_remove_q',
          ).replaceAll('{code}', c.code).replaceAll('{name}', widget.title),
        ),
        actions: [
          TextButton(
            key: const Key('code_remove_keep'),
            onPressed: () => Navigator.pop(context, false),
            child: Text(_s('code_keep')),
          ),
          // The destructive choice is the only place the danger colour is used.
          FilledButton(
            key: const Key('code_remove_confirm'),
            style: FilledButton.styleFrom(
              backgroundColor: LightColors.error,
              foregroundColor: LightColors.onError,
            ),
            onPressed: () => Navigator.pop(context, true),
            child: Text(_s('code_remove_action')),
          ),
        ],
      ),
    );
    if (yes != true) return;
    try {
      await widget.api.removeCode(c.id, _operator(widget.settings));
    } catch (e) {
      if (mounted) showStatusSnack(context, _s.error(e), StatusKind.error);
    }
    if (mounted) _reload();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('${_s('codes_of')}: ${widget.title}')),
      body: FutureBuilder<List<ItemCode>>(
        future: _codes,
        builder: (context, snap) {
          if (snap.connectionState != ConnectionState.done) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snap.hasError) {
            return ListView(
              padding: const EdgeInsets.all(AppSpace.s4),
              children: [
                StatusBanner(
                  key: const Key('codes_error'),
                  kind: StatusKind.error,
                  text: _s.error(snap.error!),
                ),
                const SizedBox(height: AppSpace.s2),
                OutlinedButton(onPressed: _reload, child: Text(_s('retry'))),
              ],
            );
          }
          final codes = snap.data!;
          return ListView(
            padding: const EdgeInsets.all(AppSpace.s4),
            children: [
              if (codes.isEmpty)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: AppSpace.s4),
                  child: Text(
                    _s('codes_empty'),
                    key: const Key('codes_empty'),
                    style: Theme.of(context).textTheme.bodyLarge,
                  ),
                ),
              for (final c in codes)
                Card(
                  key: Key('code_row_${c.id}'),
                  child: Padding(
                    padding: const EdgeInsets.only(left: AppSpace.s4),
                    child: Row(
                      children: [
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                _s('code_type_${c.codeType}'),
                                style: Theme.of(context).textTheme.bodySmall,
                              ),
                              Text(
                                c.code,
                                style: monoStyle(
                                  Theme.of(context).textTheme.bodyLarge,
                                ),
                              ),
                            ],
                          ),
                        ),
                        TextButton(
                          key: Key('code_remove_${c.id}'),
                          style: TextButton.styleFrom(
                            minimumSize: const Size(
                              AppSize.touchMin,
                              AppSize.touchMin,
                            ),
                          ),
                          onPressed: () => _remove(c),
                          child: Text(_s('code_remove')),
                        ),
                      ],
                    ),
                  ),
                ),
              const SizedBox(height: AppSpace.s2),
              SizedBox(
                height: AppSize.touchMin,
                child: OutlinedButton.icon(
                  key: const Key('code_add'),
                  icon: const Icon(Icons.add),
                  label: Text(_s('code_add')),
                  onPressed: _add,
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}

String _operator(AppSettings settings) =>
    settings.operator.trim().isEmpty ? 'app' : settings.operator.trim();

/// Adds one code to an item: scan or type, pick the type, save. Pops `true` when saved.
class CodeAssignScreen extends StatefulWidget {
  const CodeAssignScreen({
    super.key,
    required this.settings,
    required this.api,
    required this.objectType,
    required this.objectId,
    required this.title,
    this.scanner = const CameraQrScanner(anyBarcode: true),
  });

  final AppSettings settings;
  final ApiClient api;
  final String objectType;
  final int objectId;
  final String title;
  final QrScanner scanner;

  @override
  State<CodeAssignScreen> createState() => _CodeAssignScreenState();
}

class _CodeAssignScreenState extends State<CodeAssignScreen> {
  final _input = TextEditingController();
  // Made once per screen: a retry after a lost answer is the same assign, not a second one.
  final _id = const Uuid().v4();
  String _type = 'manual';
  bool _typeByHand = false;
  bool _busy = false;
  StatusKind? _bannerKind;
  String? _banner;
  CodeOwner? _owner;

  Strings get _s => widget.settings.s;

  @override
  void initState() {
    super.initState();
    _input.addListener(() {
      if (!_typeByHand) {
        final t = guessCodeType(_input.text.trim());
        if (t != _type) setState(() => _type = t);
      }
    });
  }

  @override
  void dispose() {
    _input.dispose();
    super.dispose();
  }

  void _show(StatusKind? kind, String? text, [CodeOwner? owner]) =>
      setState(() {
        _bannerKind = kind;
        _banner = text;
        _owner = owner;
      });

  Future<void> _scan() async {
    final result = await widget.scanner.scan(context, _s);
    if (!mounted) return;
    switch (result) {
      case ScanText(:final text):
        _input.text = text.trim();
        _show(null, null);
      case ScanPermissionDenied():
        _show(StatusKind.warning, _s('code_camera_off'));
      case ScanNoCamera():
        _show(StatusKind.warning, _s('qr_no_camera'));
      case ScanCancelled():
        break;
    }
  }

  Future<void> _save() async {
    final code = _input.text.trim();
    if (code.isEmpty || _busy) return;
    setState(() => _busy = true);
    try {
      await widget.api.assignCode(
        id: _id,
        objectType: widget.objectType,
        objectId: widget.objectId,
        codeType: _type,
        code: code,
        operator: _operator(widget.settings),
      );
      if (mounted) Navigator.of(context).pop(true);
      return;
    } catch (e) {
      if (mounted) {
        _show(
          StatusKind.error,
          codeErrorText(_s, e, code),
          e is ApiException && e.reason == 'code_in_use' ? e.owner : null,
        );
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _openOwner() async {
    final o = _owner;
    if (o == null) return;
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => CodesScreen(
          settings: widget.settings,
          api: widget.api,
          objectType: o.objectType,
          objectId: o.objectId,
          title: _objectTitle(o.objectType, o.objectCode, o.objectName),
          scanner: widget.scanner,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(_s('code_add'))),
      body: ListView(
        padding: const EdgeInsets.all(AppSpace.s4),
        children: [
          Text(widget.title, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: AppSpace.s4),
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
            textInputAction: TextInputAction.done,
            onSubmitted: (_) => _save(),
            decoration: InputDecoration(labelText: _s('code_field')),
          ),
          const SizedBox(height: AppSpace.s4),
          DropdownButtonFormField<String>(
            // A new key rebuilds the field when the guess changes the type.
            key: ValueKey('code_type_$_type'),
            initialValue: _type,
            // Follows the guess until the operator picks one.
            items: [
              for (final t in codeTypes)
                DropdownMenuItem(value: t, child: Text(_s('code_type_$t'))),
            ],
            onChanged: (v) => setState(() {
              if (v != null) _type = v;
              _typeByHand = true;
            }),
            decoration: InputDecoration(labelText: _s('code_type_label')),
          ),
          const SizedBox(height: AppSpace.s4),
          SizedBox(
            height: AppSize.touchMin,
            child: FilledButton(
              key: const Key('code_save'),
              onPressed: _busy ? null : _save,
              child: _busy
                  ? const SizedBox(
                      width: 24,
                      height: 24,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Text(_s('code_save')),
            ),
          ),
          if (_banner != null) ...[
            const SizedBox(height: AppSpace.s4),
            StatusBanner(
              key: const Key('code_banner'),
              kind: _bannerKind!,
              text: _banner,
            ),
            if (_owner != null) ...[
              const SizedBox(height: AppSpace.s2),
              TextButton(
                key: const Key('code_open_owner'),
                style: TextButton.styleFrom(minimumSize: const Size.fromHeight(AppSize.touchMin)),
                onPressed: _openOwner,
                child: Text(
                  _s('code_open_owner').replaceAll('{name}', _ownerName(_owner!)),
                  textAlign: TextAlign.center,
                ),
              ),
            ],
          ],
        ],
      ),
    );
  }
}
