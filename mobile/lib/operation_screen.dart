import 'package:flutter/material.dart';
import 'package:uuid/uuid.dart';

import 'api.dart';
import 'design/status.dart';
import 'design/theme.dart';
import 'design/tokens.dart';
import 'history_screen.dart';
import 'l10n.dart';
import 'picker.dart';
import 'settings.dart';
import 'settings_screen.dart';
import 'update.dart';
import 'voice.dart';

enum _Mic { idle, recording, busy }

/// The one form of the app. Every field can be typed or picked by hand, or the
/// operator can say the whole operation and only check what was filled in.
class OperationScreen extends StatefulWidget {
  const OperationScreen({super.key, required this.settings, required this.api, required this.voice, this.updates});

  final AppSettings settings;
  final UpdateChecker? updates;
  final ApiClient api;
  final VoiceCapture voice;

  @override
  State<OperationScreen> createState() => _OperationScreenState();
}

class _OperationScreenState extends State<OperationScreen> {
  static const _uuid = Uuid();

  Catalog? _catalog;
  Object? _loadError;

  String _kind = 'receipt';
  Sku? _sku;
  Location? _location;
  // True while _location is the SKU's usual place filled in by the app, not chosen by the operator.
  bool _locationIsUsual = false;
  Location? _toLocation;
  final _qty = TextEditingController();

  // Kept until the server confirms the operation, so a retry cannot post twice.
  String _draftId = _uuid.v4();
  String _source = 'keyboard';
  String _transcript = '';
  List<Sku> _candidates = const [];

  List<BalanceRow>? _balances;
  _Mic _mic = _Mic.idle;
  bool _posting = false;

  Strings get _s => widget.settings.s;

  @override
  void initState() {
    super.initState();
    _loadCatalog();
  }

  @override
  void dispose() {
    _qty.dispose();
    widget.voice.dispose();
    super.dispose();
  }

  Future<void> _loadCatalog() async {
    setState(() => _loadError = null);
    try {
      final c = await widget.api.catalog();
      if (mounted) setState(() => _catalog = c);
    } catch (e) {
      if (mounted) setState(() => _loadError = e);
    }
  }

  double? get _qtyValue => double.tryParse(_qty.text.trim().replaceAll(',', '.'));

  bool get _valid {
    final q = _qtyValue;
    if (_sku == null || _location == null || q == null) return false;
    if (_kind == 'transfer' && (_toLocation == null || _toLocation == _location)) return false;
    return q > 0 || (_kind == 'adjustment' && q == 0);
  }

  double? get _balanceHere {
    final rows = _balances;
    if (rows == null || _location == null) return null;
    for (final b in rows) {
      if (b.locationId == _location!.id) return b.qty;
    }
    return 0;
  }

  void _toast(String text, StatusKind kind) => showStatusSnack(context, text, kind);

  void _setSku(Sku? sku) {
    setState(() {
      _sku = sku;
      _balances = null;
      if (_location == null || _locationIsUsual) {
        _location = _catalog!.location(sku?.locationId);
        _locationIsUsual = _location != null;
      }
    });
    if (sku == null) return;
    widget.api.balances(sku.id).then((rows) {
      if (mounted && _sku == sku) setState(() => _balances = rows);
    }).catchError((_) {
      // The balance line is a hint; posting works without it.
    });
  }

  Future<void> _pickSku() async {
    final sku = await pick<Sku>(
      context,
      title: _s('sku'),
      searchHint: _s('search'),
      emptyText: _s('nothing_found'),
      items: _catalog!.skus,
      titleOf: (s) => s.name,
      subtitleOf: (s) => '${s.code} · ${s.unit}',
      monoSubtitle: true,
    );
    if (sku != null) {
      _candidates = const [];
      _setSku(sku);
    }
  }

  Future<Location?> _pickLocation(String title) {
    return pick<Location>(
      context,
      title: title,
      searchHint: _s('search'),
      emptyText: _s('nothing_found'),
      items: _catalog!.locations,
      titleOf: (l) => l.code,
      subtitleOf: (l) => l.name,
      monoTitle: true,
    );
  }

  Future<void> _toggleMic() async {
    if (_mic == _Mic.idle) {
      final started = await widget.voice.start().catchError((_) => false);
      if (!mounted) return;
      if (started) {
        setState(() => _mic = _Mic.recording);
      } else {
        _toast(_s('err_mic'), StatusKind.error);
      }
      return;
    }
    setState(() => _mic = _Mic.busy);
    try {
      final rec = await widget.voice.stop();
      if (rec == null) throw const ApiException(422, 'empty recording');
      final draft = await widget.api.voice(rec.bytes, rec.filename, widget.settings.lang);
      if (mounted) _applyDraft(draft);
    } catch (e) {
      if (mounted) _toast(_s.error(e), StatusKind.error);
    } finally {
      if (mounted) setState(() => _mic = _Mic.idle);
    }
  }

  /// Fills in what the phrase said and leaves the other fields as they were.
  void _applyDraft(Draft d) {
    setState(() {
      _source = 'voice';
      _transcript = d.transcript;
      if (d.kind != null) _kind = d.kind!;
      if (d.location != null) {
        _location = d.location;
        _locationIsUsual = false;
      }
      if (d.toLocation != null) _toLocation = d.toLocation;
      if (d.qty != null) _qty.text = fmtQty(d.qty!);
      _candidates = d.skus.length > 1 ? d.skus : const [];
    });
    if (d.skus.length == 1) {
      _setSku(d.skus.single);
    } else if (d.skus.length > 1) {
      // Several SKUs fit: never guess, make the operator choose.
      _setSku(null);
    }
  }

  void _reset() {
    setState(() {
      _draftId = _uuid.v4();
      _source = 'keyboard';
      _transcript = '';
      _candidates = const [];
      _sku = null;
      _balances = null;
      _location = null;
      _locationIsUsual = false;
      _toLocation = null;
      _qty.clear();
    });
  }

  Future<void> _submit() async {
    setState(() => _posting = true);
    try {
      final m = await widget.api.post(
        id: _draftId,
        kind: _kind,
        skuId: _sku!.id,
        locationId: _location!.id,
        toLocationId: _kind == 'transfer' ? _toLocation!.id : null,
        qty: _qtyValue!,
        operator: widget.settings.operator,
        source: _source,
        transcript: _transcript,
      );
      if (!mounted) return;
      final after = m.balances.map((b) => '${b.locationCode}: ${fmtQty(b.qty)} ${m.unit}').join(', ');
      _toast('${_s('posted')} · $after', StatusKind.success);
      _reset();
    } on ApiException catch (e) {
      if (!mounted) return;
      _toast(_s.error(e), StatusKind.error);
      // 409: this draft was already saved by an earlier attempt, start a new one.
      if (e.status == 409) _reset();
    } finally {
      if (mounted) setState(() => _posting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.settings.operator),
        actions: [
          IconButton(
            tooltip: _s('history'),
            icon: const Icon(Icons.history),
            onPressed: () => Navigator.of(context).push(MaterialPageRoute(
              builder: (_) => HistoryScreen(settings: widget.settings, api: widget.api),
            )),
          ),
          IconButton(
            tooltip: _s('settings'),
            icon: const Icon(Icons.settings),
            onPressed: () => Navigator.of(context).push(MaterialPageRoute(
              builder: (_) => SettingsScreen(settings: widget.settings, updates: widget.updates),
            )),
          ),
        ],
      ),
      body: Column(
        children: [
          if (widget.updates != null) UpdateBanner(checker: widget.updates!),
          Expanded(child: _catalog == null ? _buildLoading() : _buildForm(context)),
        ],
      ),
      bottomNavigationBar: _catalog == null ? null : _buildActions(context),
    );
  }

  Widget _buildLoading() {
    if (_loadError == null) return const Center(child: CircularProgressIndicator());
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(AppSpace.s6),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(_s('loading_failed'), style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: AppSpace.s2),
            StatusBanner(kind: StatusKind.error, text: _s.error(_loadError!)),
            const SizedBox(height: AppSpace.s4),
            FilledButton(onPressed: _loadCatalog, child: Text(_s('retry'))),
          ],
        ),
      ),
    );
  }

  Widget _buildForm(BuildContext context) {
    final theme = Theme.of(context);
    const gap = SizedBox(height: AppSpace.s4);
    return ListView(
      padding: const EdgeInsets.all(AppSpace.s4),
      children: [
        if (_mic != _Mic.idle) ...[
          StatusBanner(
            kind: StatusKind.info,
            leading: _mic == _Mic.recording
                ? const Icon(Icons.mic, color: LightColors.infoText, size: AppSize.icon)
                : const SizedBox(
                    width: AppSize.icon,
                    height: AppSize.icon,
                    child: CircularProgressIndicator(strokeWidth: AppSpace.s1, color: LightColors.infoText),
                  ),
            text: _s(_mic == _Mic.recording ? 'listening' : 'recognizing'),
          ),
          gap,
        ],
        if (_transcript.isNotEmpty) ...[
          Card(
            child: ListTile(
              leading: const Icon(Icons.record_voice_over),
              title: Text('«$_transcript»'),
              subtitle: Text(_s('heard')),
            ),
          ),
          gap,
        ],
        Wrap(
          spacing: AppSpace.s2,
          runSpacing: AppSpace.s2,
          children: [
            for (final k in kinds)
              ChoiceChip(
                key: Key('kind_$k'),
                label: Text(_s.kind(k)),
                selected: _kind == k,
                onSelected: (_) => setState(() => _kind = k),
              ),
          ],
        ),
        gap,
        PickerField(
          key: const Key('sku'),
          label: _s('sku'),
          text: _sku?.name,
          code: _sku?.code,
          onTap: _pickSku,
        ),
        if (_candidates.isNotEmpty) ...[
          const SizedBox(height: AppSpace.s2),
          StatusBanner(kind: StatusKind.warning, text: _s('which_sku')),
          const SizedBox(height: AppSpace.s2),
          Wrap(
            spacing: AppSpace.s2,
            runSpacing: AppSpace.s2,
            children: [
              for (final c in _candidates)
                ActionChip(
                  label: Text.rich(TextSpan(children: [
                    TextSpan(text: '${c.name} ('),
                    TextSpan(text: c.code, style: monoStyle()),
                    const TextSpan(text: ')'),
                  ])),
                  onPressed: () {
                    _candidates = const [];
                    _setSku(c);
                  },
                ),
            ],
          ),
        ],
        gap,
        PickerField(
          key: const Key('location'),
          label: _s(_kind == 'transfer' ? 'from' : 'location'),
          text: _location?.code,
          onTap: () async {
            final l = await _pickLocation(_s(_kind == 'transfer' ? 'from' : 'location'));
            if (l != null) {
              setState(() {
                _location = l;
                _locationIsUsual = false;
              });
            }
          },
        ),
        if (_kind == 'transfer') ...[
          gap,
          PickerField(
            key: const Key('to_location'),
            label: _s('to'),
            text: _toLocation?.code,
            onTap: () async {
              final l = await _pickLocation(_s('to'));
              if (l != null) setState(() => _toLocation = l);
            },
          ),
        ],
        gap,
        TextField(
          key: const Key('qty'),
          controller: _qty,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          style: monoStyle(theme.textTheme.headlineSmall),
          decoration: InputDecoration(
            labelText: _s(_kind == 'adjustment' ? 'qty_counted' : 'qty'),
            suffixText: _sku?.unit,
          ),
          onChanged: (_) => setState(() {}),
        ),
        const SizedBox(height: AppSpace.s3),
        ..._buildBalanceHint(theme),
      ],
    );
  }

  List<Widget> _buildBalanceHint(ThemeData theme) {
    final rows = _balances;
    final sku = _sku;
    if (rows == null || sku == null) return const [];
    final here = _balanceHere;
    final mono = monoStyle();
    final elsewhere = [
      for (final b in rows)
        if (b.locationId != _location?.id) '${b.locationCode} (${fmtQty(b.qty)})',
    ];
    final alt = _catalog!.location(sku.altLocationId);
    final q = _qtyValue;
    final goesNegative = here != null && q != null && (_kind == 'issue' || _kind == 'transfer') && q > here;
    return [
      if (here != null)
        Text.rich(
          TextSpan(children: [
            TextSpan(text: '${_location!.code} — ', style: mono),
            TextSpan(text: '${_s('balance')} '),
            TextSpan(text: fmtQty(here), style: mono),
            TextSpan(text: ' ${sku.unit}'),
          ]),
          style: theme.textTheme.bodyLarge,
        ),
      if (alt != null && alt != _location)
        Text.rich(
          TextSpan(children: [
            TextSpan(text: '${_s('look_also')} '),
            TextSpan(text: alt.code, style: mono),
          ]),
          style: theme.textTheme.bodyMedium,
        ),
      if (elsewhere.isNotEmpty)
        Text.rich(
          TextSpan(children: [
            TextSpan(text: '${_s('also_at')} '),
            TextSpan(text: elsewhere.join(', '), style: mono),
          ]),
          style: theme.textTheme.bodyMedium,
        ),
      if (goesNegative)
        Padding(
          padding: const EdgeInsets.only(top: AppSpace.s2),
          child: StatusBanner(kind: StatusKind.warning, text: _s('will_be_negative')),
        ),
    ];
  }

  Widget _buildActions(BuildContext context) {
    final recording = _mic == _Mic.recording;
    final onMic = _mic == _Mic.busy || _posting ? null : _toggleMic;
    final micContent = Column(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        Icon(recording ? Icons.stop : Icons.mic, size: AppSize.icon),
        Text(_s('speak'), style: Theme.of(context).textTheme.labelMedium),
      ],
    );
    final micStyle = ButtonStyle(padding: WidgetStateProperty.all(const EdgeInsets.symmetric(horizontal: AppSpace.s1)));
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(AppSpace.s4, AppSpace.s2, AppSpace.s4, AppSpace.s3),
        child: Row(
          children: [
            SizedBox(
              width: AppSpace.s16 + AppSpace.s6,
              height: AppSpace.s16,
              // Secondary while idle; the primary look shows that it is recording.
              child: recording
                  ? FilledButton(key: const Key('mic'), style: micStyle, onPressed: onMic, child: micContent)
                  : OutlinedButton(key: const Key('mic'), style: micStyle, onPressed: onMic, child: micContent),
            ),
            const SizedBox(width: AppSpace.s3),
            Expanded(
              child: SizedBox(
                height: AppSpace.s16,
                child: FilledButton(
                  key: const Key('post'),
                  onPressed: _valid && !_posting && _mic == _Mic.idle ? _submit : null,
                  child: _posting
                      ? const SizedBox(
                          width: AppSize.icon,
                          height: AppSize.icon,
                          child: CircularProgressIndicator(strokeWidth: AppSpace.s1, color: LightColors.onPrimary),
                        )
                      : Text(_s('post'), style: const TextStyle(fontSize: AppText.txl)),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
