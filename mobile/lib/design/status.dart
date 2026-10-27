import 'package:flutter/material.dart';

import 'tokens.dart';

/// Status is always an icon, a word and a colour together, never a colour alone.
enum StatusKind { success, warning, error, info }

extension StatusStyle on StatusKind {
  Color get accent => switch (this) {
    StatusKind.success => LightColors.success,
    StatusKind.warning => LightColors.warning,
    StatusKind.error => LightColors.error,
    StatusKind.info => LightColors.info,
  };

  Color get background => switch (this) {
    StatusKind.success => LightColors.successBg,
    StatusKind.warning => LightColors.warningBg,
    StatusKind.error => LightColors.errorBg,
    StatusKind.info => LightColors.infoBg,
  };

  Color get foreground => switch (this) {
    StatusKind.success => LightColors.successText,
    StatusKind.warning => LightColors.warningText,
    StatusKind.error => LightColors.errorText,
    StatusKind.info => LightColors.infoText,
  };

  IconData get icon => switch (this) {
    StatusKind.success => Icons.check_circle_outline,
    StatusKind.warning => Icons.warning_amber_rounded,
    StatusKind.error => Icons.error_outline,
    StatusKind.info => Icons.info_outline,
  };
}

/// An inline message: status icon, text, optional [leading] instead of the icon and [trailing] widget.
class StatusBanner extends StatelessWidget {
  const StatusBanner({
    super.key,
    required this.kind,
    this.text,
    this.child,
    this.leading,
    this.trailing,
  });

  final StatusKind kind;
  final String? text;

  /// Used instead of [text] when the message is more than one line of text.
  final Widget? child;
  final Widget? leading;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final style = Theme.of(
      context,
    ).textTheme.bodyLarge!.copyWith(color: kind.foreground);
    return Container(
      padding: const EdgeInsets.all(AppSpace.s3),
      decoration: BoxDecoration(
        color: kind.background,
        borderRadius: BorderRadius.circular(AppRadius.md),
        border: Border.all(color: kind.accent),
      ),
      child: Row(
        children: [
          leading ??
              Icon(kind.icon, color: kind.foreground, size: AppSize.icon),
          const SizedBox(width: AppSpace.s3),
          Expanded(
            child: DefaultTextStyle(
              style: style,
              child: child ?? Text(text ?? ''),
            ),
          ),
          ?trailing,
        ],
      ),
    );
  }
}

/// A small label with a status icon and a word.
class StatusChip extends StatelessWidget {
  const StatusChip({
    super.key,
    required this.kind,
    required this.label,
    this.icon,
  });

  final StatusKind kind;
  final String label;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpace.s2,
        vertical: AppSpace.s1,
      ),
      decoration: BoxDecoration(
        color: kind.background,
        borderRadius: BorderRadius.circular(AppRadius.full),
        border: Border.all(color: kind.accent),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon ?? kind.icon, color: kind.foreground, size: AppText.tsm),
          const SizedBox(width: AppSpace.s1),
          Flexible(
            child: Text(
              label,
              style: Theme.of(
                context,
              ).textTheme.bodySmall!.copyWith(color: kind.foreground),
            ),
          ),
        ],
      ),
    );
  }
}

/// A toast with a status icon, the text and the status colours.
void showStatusSnack(BuildContext context, String text, StatusKind kind) {
  final style = Theme.of(
    context,
  ).textTheme.bodyLarge!.copyWith(color: kind.foreground);
  ScaffoldMessenger.of(context)
    ..hideCurrentSnackBar()
    ..showSnackBar(
      SnackBar(
        behavior: SnackBarBehavior.floating,
        margin: const EdgeInsets.all(AppSpace.s4),
        backgroundColor: kind.background,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.md),
          side: BorderSide(color: kind.accent),
        ),
        content: Row(
          children: [
            Icon(kind.icon, color: kind.foreground, size: AppSize.icon),
            const SizedBox(width: AppSpace.s3),
            Expanded(child: Text(text, style: style)),
          ],
        ),
      ),
    );
}
