import 'package:flutter/material.dart';

import 'api.dart';
import 'design/status.dart';
import 'design/tokens.dart';
import 'feedback.dart';
import 'settings.dart';

/// Free-text problem report. [from] is the name of the screen it was opened from.
class FeedbackScreen extends StatefulWidget {
  const FeedbackScreen({super.key, required this.settings, required this.feedback, required this.from});

  final AppSettings settings;
  final FeedbackService feedback;
  final String from;

  @override
  State<FeedbackScreen> createState() => _FeedbackScreenState();
}

class _FeedbackScreenState extends State<FeedbackScreen> {
  final _text = TextEditingController();
  bool _sending = false;

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    final s = widget.settings.s;
    final navigator = Navigator.of(context);
    setState(() => _sending = true);
    try {
      final outcome = await widget.feedback.submit(_text.text, from: widget.from);
      if (!mounted) return;
      showStatusSnack(
        context,
        s(outcome == FeedbackOutcome.sent ? 'report_sent' : 'report_saved'),
        outcome == FeedbackOutcome.sent ? StatusKind.success : StatusKind.info,
      );
      navigator.pop();
    } on ApiException catch (e) {
      if (!mounted) return;
      showStatusSnack(context, e.status == 400 ? s('report_rejected') : e.message, StatusKind.error);
      setState(() => _sending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final s = widget.settings.s;
    return Scaffold(
      appBar: AppBar(title: Text(s('report_problem'))),
      body: ListView(
        padding: const EdgeInsets.all(AppSpace.s4),
        children: [
          TextField(
            key: const Key('report_text'),
            controller: _text,
            minLines: 5,
            maxLines: 10,
            maxLength: 4000,
            textCapitalization: TextCapitalization.sentences,
            onChanged: (_) => setState(() {}),
            decoration: InputDecoration(labelText: s('report_hint')),
          ),
          const SizedBox(height: AppSpace.s4),
          SizedBox(
            height: AppSize.inputHeight,
            child: FilledButton(
              key: const Key('report_send'),
              onPressed: _sending || _text.text.trim().isEmpty ? null : _send,
              child: _sending
                  ? const SizedBox(
                      width: AppSize.icon,
                      height: AppSize.icon,
                      child: CircularProgressIndicator(strokeWidth: AppSpace.s1, color: LightColors.onPrimary),
                    )
                  : Text(s('report_send')),
            ),
          ),
        ],
      ),
    );
  }
}
