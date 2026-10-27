import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'api.dart';
import 'design/theme.dart';
import 'feedback.dart';
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
    api: () => ApiClient(settings.serverUrl, accessCode: settings.accessCode),
  );
  runApp(QoimeksApp(settings: settings, updates: updates, feedback: feedback));
  unawaited(updates.checkOnStart());
  unawaited(feedback.retryPending());
}

class QoimeksApp extends StatelessWidget {
  const QoimeksApp({super.key, required this.settings, this.updates, this.feedback});

  final AppSettings settings;
  final UpdateChecker? updates;
  final FeedbackService? feedback;

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
                  onSuccess: feedback?.retryPending,
                  onError: feedback?.onApiError,
                ),
                voice: MicrophoneCapture(),
                updates: updates,
                feedback: feedback,
              )
            : SettingsScreen(settings: settings, firstRun: true, updates: updates),
      ),
    );
  }
}
