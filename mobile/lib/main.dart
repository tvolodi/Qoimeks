import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'api.dart';
import 'design/theme.dart';
import 'feedback.dart';
import 'offline_queue.dart';
import 'operation_screen.dart';
import 'settings.dart';
import 'settings_screen.dart';
import 'update.dart';
import 'voice.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final settings = await AppSettings.load();
  final updates = UpdateChecker(settings: settings);
  final feedback = FeedbackService(
    settings: settings,
    api: () => ApiClient(settings.serverUrl, accessCode: settings.accessCode, operator: settings.operator, deviceInfo: readDeviceInfo),
  );
  final queue = OfflineQueue();
  await queue.load();
  runApp(QoimeksApp(settings: settings, updates: updates, feedback: feedback, queue: queue));
  unawaited(updates.checkOnStart());
  unawaited(feedback.retryPending());
  unawaited(flushQueue(queue, settings));
}

/// Sends the queued operations with a client of its own, so its successes do not start another flush.
Future<void> flushQueue(OfflineQueue queue, AppSettings settings) {
  if (!settings.isComplete) return Future.value();
  return queue.flush(
    ApiClient(settings.serverUrl, accessCode: settings.accessCode, operator: settings.operator, deviceInfo: readDeviceInfo),
    serverUrl: settings.serverUrl,
    accessCode: settings.accessCode,
  );
}

class QoimeksApp extends StatelessWidget {
  const QoimeksApp({super.key, required this.settings, this.updates, this.feedback, this.queue});

  final AppSettings settings;
  final UpdateChecker? updates;
  final FeedbackService? feedback;
  final OfflineQueue? queue;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: settings,
      builder: (context, _) => MaterialApp(
        title: 'Qoimeks',
        debugShowCheckedModeBanner: false,
        theme: buildTheme(),
        locale: Locale(settings.lang),
        supportedLocales: const [Locale('ru'), Locale('kk')],
        localizationsDelegates: GlobalMaterialLocalizations.delegates,
        home: settings.isComplete
            ? OperationScreen(
                // A new server address or access code means a new catalog: rebuild the form.
                key: ValueKey('${settings.serverUrl} ${settings.accessCode}'),
                settings: settings,
                api: ApiClient(
                  settings.serverUrl,
                  accessCode: settings.accessCode,
                  operator: settings.operator,
                  deviceInfo: readDeviceInfo,
                  // Any successful call shows the server is reachable: retry what is waiting.
                  onSuccess: () {
                    feedback?.retryPending();
                    if (queue != null) unawaited(flushQueue(queue!, settings));
                  },
                  onError: feedback?.onApiError,
                ),
                voice: MicrophoneCapture(),
                updates: updates,
                feedback: feedback,
                offlineQueue: queue,
              )
            : SettingsScreen(settings: settings, firstRun: true, updates: updates),
      ),
    );
  }
}
