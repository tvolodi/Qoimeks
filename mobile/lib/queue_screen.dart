import 'package:flutter/material.dart';

import 'api.dart';
import 'design/status.dart';
import 'design/tokens.dart';
import 'l10n.dart';
import 'offline_queue.dart';
import 'settings.dart';

/// Operations waiting for the server, the ones it refused (with the reason) and the ones
/// made for another server.
class QueueScreen extends StatelessWidget {
  const QueueScreen({super.key, required this.settings, required this.queue, required this.api});

  final AppSettings settings;
  final OfflineQueue queue;
  final ApiClient api;

  @override
  Widget build(BuildContext context) {
    final s = settings.s;
    return Scaffold(
      appBar: AppBar(title: Text(s('queue_title'))),
      body: ListenableBuilder(
        listenable: queue,
        builder: (context, _) {
          final items = queue.items;
          if (items.isEmpty) return Center(child: Text(s('queue_empty')));
          return ListView(
            padding: const EdgeInsets.all(AppSpace.s4),
            children: [for (final o in items) _tile(context, s, o)],
          );
        },
      ),
    );
  }

  Widget _tile(BuildContext context, Strings s, QueuedOperation o) {
    final here = o.serverUrl == OfflineQueue.normalizeUrl(settings.serverUrl) && o.accessCode == settings.accessCode;
    final place = o.toLocationCode.isEmpty ? o.locationCode : '${o.locationCode} → ${o.toLocationCode}';
    final rejected = here && o.rejection != null;
    return Card(
      key: Key('queue_item_${o.id}'),
      child: Padding(
        padding: const EdgeInsets.all(AppSpace.s3),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('${s.kind(o.kind)} · ${o.skuName}', style: Theme.of(context).textTheme.titleMedium),
            Text('$place · ${fmtQty(o.qty)} ${o.unit}'),
            if (!here) ...[
              const SizedBox(height: AppSpace.s2),
              StatusBanner(kind: StatusKind.info, text: s('queue_foreign')),
            ],
            if (rejected) ...[
              const SizedBox(height: AppSpace.s2),
              StatusBanner(kind: StatusKind.error, text: '${s('queue_rejected')}: ${o.rejection}'),
            ],
            if (!here || rejected)
              Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  if (rejected)
                    TextButton(
                      key: Key('queue_retry_${o.id}'),
                      onPressed: () async {
                        await queue.retry(o.id);
                        await queue.flush(api, serverUrl: settings.serverUrl, accessCode: settings.accessCode);
                      },
                      child: Text(s('retry')),
                    ),
                  TextButton(
                    key: Key('queue_delete_${o.id}'),
                    onPressed: () => queue.dismiss(o.id),
                    child: Text(s('queue_delete')),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}
