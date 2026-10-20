import 'package:flutter/material.dart';

import 'l10n.dart';
import 'settings.dart';
import 'update.dart';

/// Shown on first launch (nothing works without a server and a name) and from the gear icon.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, required this.settings, this.firstRun = false, this.updates});

  final AppSettings settings;
  final UpdateChecker? updates;
  final bool firstRun;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final _form = GlobalKey<FormState>();
  late final _server = TextEditingController(
    text: widget.settings.serverUrl.isEmpty ? 'http://' : widget.settings.serverUrl,
  );
  late final _accessCode = TextEditingController(text: widget.settings.accessCode);
  late final _operator = TextEditingController(text: widget.settings.operator);
  late String _lang = widget.settings.lang;

  Future<void> _checkUpdates(Strings s) async {
    final messenger = ScaffoldMessenger.of(context);
    final result = await widget.updates!.checkNow();
    if (!mounted) return;
    messenger.showSnackBar(SnackBar(
      content: Text(s(switch (result) {
        UpdateResult.available => 'update_available',
        UpdateResult.failed => 'update_check_failed',
        _ => 'update_up_to_date',
      })),
    ));
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
    String? required(String? v) => (v == null || v.trim().isEmpty) ? s('required') : null;
    return Scaffold(
      appBar: AppBar(title: Text(s('settings'))),
      body: Form(
        key: _form,
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            SegmentedButton<String>(
              segments: const [
                ButtonSegment(value: 'ru', label: Text('Русский')),
                ButtonSegment(value: 'kk', label: Text('Қазақша')),
              ],
              selected: {_lang},
              onSelectionChanged: (v) => setState(() => _lang = v.single),
            ),
            const SizedBox(height: 16),
            TextFormField(
              key: const Key('operator'),
              controller: _operator,
              textCapitalization: TextCapitalization.words,
              decoration: InputDecoration(labelText: s('operator'), border: const OutlineInputBorder()),
              validator: required,
            ),
            const SizedBox(height: 16),
            TextFormField(
              key: const Key('server'),
              controller: _server,
              keyboardType: TextInputType.url,
              autocorrect: false,
              decoration: InputDecoration(
                labelText: s('server'),
                hintText: 'http://192.168.1.10:8080',
                border: const OutlineInputBorder(),
              ),
              validator: (v) {
                final uri = Uri.tryParse((v ?? '').trim());
                return uri != null && uri.host.isNotEmpty ? null : s('required');
              },
            ),
            const SizedBox(height: 16),
            TextFormField(
              key: const Key('access_code'),
              controller: _accessCode,
              autocorrect: false,
              enableSuggestions: false,
              decoration: InputDecoration(
                labelText: s('access_code'),
                helperText: s('access_code_hint'),
                border: const OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 24),
            SizedBox(
              height: 56,
              child: FilledButton(key: const Key('save'), onPressed: _save, child: Text(s('save'))),
            ),
            if (widget.updates != null) ...[
              const SizedBox(height: 24),
              FutureBuilder<String>(
                future: widget.updates!.installedLabel(),
                builder: (context, snap) => Text(
                  key: const Key('app_version'),
                  '${s('app_version')}: ${snap.data ?? ''}',
                ),
              ),
              if (widget.updates!.enabled) ...[
                const SizedBox(height: 8),
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
