import 'package:flutter/material.dart';
import 'package:uuid/uuid.dart';

import 'api.dart';
import 'design/status.dart';
import 'design/theme.dart';
import 'design/tokens.dart';
import 'l10n.dart';
import 'settings.dart';

/// Recent operations of all operators. A mistake is not edited: it is cancelled
/// here by a reversing entry, and the right operation is posted again.
class HistoryScreen extends StatefulWidget {
  const HistoryScreen({super.key, required this.settings, required this.api});

  final AppSettings settings;
  final ApiClient api;

  @override
  State<HistoryScreen> createState() => _HistoryScreenState();
}

class _HistoryScreenState extends State<HistoryScreen> {
  late Future<List<Movement>> _movements = widget.api.movements();

  Strings get _s => widget.settings.s;

  static String _two(int v) => v.toString().padLeft(2, '0');

  static String _time(DateTime t) => '${_two(t.day)}.${_two(t.month)} ${_two(t.hour)}:${_two(t.minute)}';

  Future<void> _reverse(Movement m) async {
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(_s('reverse_q')),
        content: Text('${_s.kind(m.kind)}: ${m.skuName}, ${fmtQty(m.qty)} ${m.unit}'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: Text(_s('cancel'))),
          // The destructive choice is the only place the danger colour is used.
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: LightColors.error, foregroundColor: LightColors.onError),
            onPressed: () => Navigator.pop(context, true),
            child: Text(_s('reverse')),
          ),
        ],
      ),
    );
    if (yes != true) return;
    try {
      await widget.api.reverse(movementId: m.id, id: const Uuid().v4(), operator: widget.settings.operator);
    } catch (e) {
      if (mounted) showStatusSnack(context, _s.error(e), StatusKind.error);
    }
    if (mounted) setState(() => _movements = widget.api.movements());
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(_s('history'))),
      body: FutureBuilder<List<Movement>>(
        future: _movements,
        builder: (context, snap) {
          if (snap.hasError) {
            return Padding(
              padding: const EdgeInsets.all(AppSpace.s4),
              child: Align(
                alignment: Alignment.topCenter,
                child: StatusBanner(kind: StatusKind.error, text: _s.error(snap.error!)),
              ),
            );
          }
          if (!snap.hasData) return const Center(child: CircularProgressIndicator());
          final items = snap.data!;
          if (items.isEmpty) return Center(child: Text(_s('empty_history')));
          return ListView.separated(
            itemCount: items.length,
            separatorBuilder: (_, _) => const Divider(height: 1),
            itemBuilder: (context, i) {
              final m = items[i];
              final cancelled = m.isReversed || m.isReversal;
              final where = m.toLocationCode.isEmpty ? m.locationCode : '${m.locationCode} → ${m.toLocationCode}';
              final label = m.isReversal ? '${_s('reversal')}: ${_s.kind(m.kind)}' : _s.kind(m.kind);
              final mono = monoStyle();
              final text = theme.textTheme;
              return Padding(
                padding: const EdgeInsets.symmetric(horizontal: AppSpace.s4, vertical: AppSpace.s3),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text.rich(
                      TextSpan(children: [
                        TextSpan(text: '$label · '),
                        TextSpan(text: '${fmtQty(m.qty)} ${m.unit}', style: mono),
                        const TextSpan(text: ' · '),
                        TextSpan(text: where, style: mono),
                      ]),
                      style: cancelled
                          ? text.bodyLarge?.copyWith(
                              color: LightColors.textMute,
                              decoration: m.isReversed ? TextDecoration.lineThrough : null,
                            )
                          : text.bodyLarge,
                    ),
                    Text.rich(
                      TextSpan(children: [
                        TextSpan(text: '${m.skuName} ('),
                        TextSpan(text: m.skuCode, style: mono),
                        const TextSpan(text: ')'),
                      ]),
                      style: text.bodyMedium?.copyWith(color: LightColors.textSoft),
                    ),
                    Text(
                      '${_time(m.createdAt)} · ${m.operator}',
                      style: text.bodyMedium?.copyWith(color: LightColors.textSoft),
                    ),
                    if (m.isReversed) ...[
                      const SizedBox(height: AppSpace.s1),
                      StatusChip(kind: StatusKind.warning, label: _s('reversed'), icon: Icons.undo),
                    ],
                    if (!cancelled)
                      Align(
                        alignment: Alignment.centerRight,
                        child: TextButton.icon(
                          onPressed: () => _reverse(m),
                          icon: const Icon(Icons.undo),
                          label: Text(_s('reverse')),
                        ),
                      ),
                  ],
                ),
              );
            },
          );
        },
      ),
    );
  }
}
