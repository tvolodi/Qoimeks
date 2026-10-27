import 'package:flutter/material.dart';

import 'tokens.dart';

/// The type style for codes and numbers (SKU, location, quantities): monospace, equal-width digits.
TextStyle monoStyle([TextStyle? base]) => (base ?? const TextStyle()).copyWith(
  fontFamily: AppFonts.mono,
  fontFamilyFallback: const ['monospace'],
  fontFeatures: const [FontFeature.tabularFigures()],
);

/// The app's light theme, built only from the design tokens (design system 0.2, light only).
/// The font files are not bundled yet: the families are named and fall back to the system fonts.
ThemeData buildTheme() {
  const scheme = ColorScheme(
    brightness: Brightness.light,
    primary: LightColors.primary,
    onPrimary: LightColors.onPrimary,
    primaryContainer: LightColors.infoBg,
    onPrimaryContainer: LightColors.infoText,
    secondary: LightColors.primary,
    onSecondary: LightColors.onPrimary,
    secondaryContainer: LightColors.surface2,
    onSecondaryContainer: LightColors.text,
    error: LightColors.error,
    onError: LightColors.onError,
    errorContainer: LightColors.errorBg,
    onErrorContainer: LightColors.errorText,
    surface: LightColors.surface,
    onSurface: LightColors.text,
    onSurfaceVariant: LightColors.textSoft,
    outline: LightColors.controlBorder,
    outlineVariant: LightColors.border,
    surfaceTint: Colors.transparent,
    surfaceContainerLowest: LightColors.surface,
    surfaceContainerLow: LightColors.surface,
    surfaceContainer: LightColors.surface,
    surfaceContainerHigh: LightColors.surface2,
    surfaceContainerHighest: LightColors.surface2,
    inverseSurface: LightColors.text,
    onInverseSurface: LightColors.bg,
    inversePrimary: LightColors.primaryHover,
  );

  const sans = AppFonts.sans;
  TextStyle t(
    double size, {
    FontWeight weight = FontWeight.w400,
    Color color = LightColors.text,
  }) => TextStyle(
    fontFamily: sans,
    fontSize: size,
    fontWeight: weight,
    color: color,
    height: 1.4,
  );

  final textTheme = TextTheme(
    displayLarge: t(AppText.t4XL, weight: FontWeight.w600),
    displayMedium: t(AppText.t3XL, weight: FontWeight.w600),
    displaySmall: t(AppText.t2XL, weight: FontWeight.w600),
    headlineLarge: t(AppText.t3XL, weight: FontWeight.w600),
    headlineMedium: t(AppText.t2XL, weight: FontWeight.w600),
    headlineSmall: t(AppText.t2XL, weight: FontWeight.w500),
    titleLarge: t(AppText.txl, weight: FontWeight.w600),
    titleMedium: t(AppText.tbase, weight: FontWeight.w500),
    titleSmall: t(AppText.tsm, weight: FontWeight.w500),
    bodyLarge: t(AppText.tbase),
    bodyMedium: t(AppText.tsm),
    bodySmall: t(AppText.txs, color: LightColors.textSoft),
    labelLarge: t(AppText.tbase, weight: FontWeight.w600),
    labelMedium: t(AppText.tsm, weight: FontWeight.w500),
    labelSmall: t(AppText.txs, weight: FontWeight.w500),
  );

  final shape = RoundedRectangleBorder(
    borderRadius: BorderRadius.circular(AppRadius.md),
  );
  const touch = Size(AppSize.touchMin, AppSize.touchMin);
  OutlineInputBorder border(Color color, [double width = 1]) =>
      OutlineInputBorder(
        borderRadius: BorderRadius.circular(AppRadius.md),
        borderSide: BorderSide(color: color, width: width),
      );

  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    fontFamily: sans,
    scaffoldBackgroundColor: LightColors.bg,
    textTheme: textTheme,
    primaryTextTheme: textTheme,
    dividerTheme: const DividerThemeData(
      color: LightColors.border,
      thickness: 1,
      space: 1,
    ),
    appBarTheme: AppBarTheme(
      backgroundColor: LightColors.surface,
      foregroundColor: LightColors.text,
      elevation: 0,
      scrolledUnderElevation: 0,
      surfaceTintColor: Colors.transparent,
      titleTextStyle: textTheme.titleLarge,
      shape: const Border(bottom: BorderSide(color: LightColors.border)),
    ),
    // Filled = primary (night blue, white text), outlined = secondary, text = ghost.
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: LightColors.primary,
        foregroundColor: LightColors.onPrimary,
        disabledBackgroundColor: LightColors.surface2,
        disabledForegroundColor: LightColors.textMute,
        minimumSize: touch,
        shape: shape,
        textStyle: textTheme.labelLarge,
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: LightColors.primary,
        disabledForegroundColor: LightColors.textMute,
        side: const BorderSide(color: LightColors.primary, width: 1.5),
        minimumSize: touch,
        shape: shape,
        textStyle: textTheme.labelLarge,
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        foregroundColor: LightColors.accentText,
        minimumSize: touch,
        shape: shape,
        textStyle: textTheme.labelLarge,
      ),
    ),
    iconButtonTheme: IconButtonThemeData(
      style: IconButton.styleFrom(
        foregroundColor: LightColors.text,
        minimumSize: touch,
        shape: shape,
      ),
    ),
    segmentedButtonTheme: SegmentedButtonThemeData(
      style: SegmentedButton.styleFrom(
        backgroundColor: LightColors.surface,
        foregroundColor: LightColors.primary,
        selectedBackgroundColor: LightColors.primary,
        selectedForegroundColor: LightColors.onPrimary,
        side: const BorderSide(color: LightColors.primary),
        minimumSize: touch,
        shape: shape,
        textStyle: textTheme.labelLarge,
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: LightColors.surface,
      constraints: const BoxConstraints(minHeight: AppSize.inputHeight),
      contentPadding: const EdgeInsets.symmetric(
        horizontal: AppSpace.s4,
        vertical: AppSpace.s3,
      ),
      border: border(LightColors.controlBorder),
      enabledBorder: border(LightColors.controlBorder),
      focusedBorder: border(LightColors.focus, 2),
      errorBorder: border(LightColors.error),
      focusedErrorBorder: border(LightColors.error, 2),
      labelStyle: textTheme.bodyLarge?.copyWith(color: LightColors.textSoft),
      floatingLabelStyle: textTheme.bodyMedium?.copyWith(
        color: LightColors.text,
      ),
      hintStyle: textTheme.bodyLarge?.copyWith(color: LightColors.textMute),
      helperStyle: textTheme.bodySmall,
      errorStyle: textTheme.bodySmall?.copyWith(color: LightColors.errorText),
      suffixStyle: textTheme.bodyLarge?.copyWith(color: LightColors.textSoft),
    ),
    chipTheme: ChipThemeData(
      backgroundColor: LightColors.surface,
      selectedColor: LightColors.primary,
      checkmarkColor: LightColors.onPrimary,
      disabledColor: LightColors.surface2,
      side: const BorderSide(color: LightColors.controlBorder),
      shape: shape,
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpace.s2,
        vertical: AppSpace.s3,
      ),
      labelStyle: textTheme.bodyLarge,
      secondaryLabelStyle: textTheme.bodyLarge?.copyWith(
        color: LightColors.onPrimary,
      ),
    ),
    cardTheme: CardThemeData(
      color: LightColors.surface,
      elevation: 0,
      margin: EdgeInsets.zero,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.md),
        side: const BorderSide(color: LightColors.border),
      ),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: LightColors.surface,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.lg),
      ),
      titleTextStyle: textTheme.titleLarge,
      contentTextStyle: textTheme.bodyLarge,
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: LightColors.surface,
      contentTextStyle: textTheme.bodyLarge,
      shape: shape,
    ),
    listTileTheme: ListTileThemeData(
      titleTextStyle: textTheme.bodyLarge,
      subtitleTextStyle: textTheme.bodyMedium?.copyWith(
        color: LightColors.textSoft,
      ),
      iconColor: LightColors.textSoft,
      contentPadding: const EdgeInsets.symmetric(horizontal: AppSpace.s4),
    ),
    progressIndicatorTheme: const ProgressIndicatorThemeData(
      color: LightColors.primary,
    ),
    textSelectionTheme: const TextSelectionThemeData(
      cursorColor: LightColors.primary,
    ),
    focusColor: LightColors.focus,
  );
}
