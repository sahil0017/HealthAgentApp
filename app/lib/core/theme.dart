import 'package:flutter/material.dart';

class Pal {
  static const bg = Color(0xFF0B1210);
  static const card = Color(0xFF141D1A);
  static const ink = Color(0xFFEAF2EF);
  static const mut = Color(0xFF8FA39C);
  static const pri = Color(0xFF2BC4AE);
  static const priBg = Color(0xFF12332E);
  static const warn = Color(0xFFF2B04A);
  static const warnBg = Color(0xFF3A2B10);
  static const bad = Color(0xFFFF8A80);
  static const badBg = Color(0xFF3C1815);
  static const ok = Color(0xFF5FD18B);
  static const okBg = Color(0xFF10301F);
  static const line = Color(0xFF24312D);
  static const onPri = Color(0xFF04201B);
}

ThemeData buildTheme() {
  final base = ThemeData(
    brightness: Brightness.dark,
    useMaterial3: true,
    scaffoldBackgroundColor: Pal.bg,
    colorScheme: const ColorScheme.dark(
      primary: Pal.pri,
      onPrimary: Pal.onPri,
      surface: Pal.card,
      onSurface: Pal.ink,
      error: Pal.bad,
    ),
  );
  final text = base.textTheme
      .apply(bodyColor: Pal.ink, displayColor: Pal.ink)
      .copyWith(
        headlineSmall: TextStyle(
          fontSize: 20,
          fontWeight: FontWeight.w700,
          letterSpacing: -0.3,
        ),
        titleMedium: TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
        bodyMedium: TextStyle(fontSize: 14, height: 1.35),
        bodySmall: TextStyle(fontSize: 12, color: Pal.mut, height: 1.3),
        labelSmall: TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w600,
          color: Pal.mut,
          letterSpacing: .6,
        ),
      );
  return base.copyWith(
    textTheme: text,
    appBarTheme: AppBarTheme(
      backgroundColor: Pal.bg,
      elevation: 0,
      scrolledUnderElevation: 0,
      titleTextStyle: text.titleMedium?.copyWith(fontSize: 17),
    ),
    cardTheme: CardThemeData(
      color: Pal.card,
      margin: EdgeInsets.zero,
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
        side: const BorderSide(color: Color(0x0FFFFFFF)),
      ),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: const Color(0xFF0F1715),
      indicatorColor: Pal.priBg,
      height: 62,
      labelTextStyle: WidgetStatePropertyAll(
        TextStyle(fontSize: 11, fontWeight: FontWeight.w500),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: const Size.fromHeight(48),
        backgroundColor: Pal.pri,
        foregroundColor: Pal.onPri,
        textStyle: TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: const Size.fromHeight(48),
        foregroundColor: Pal.ink,
        side: const BorderSide(color: Pal.line),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Pal.card,
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: const BorderSide(color: Pal.line),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: const BorderSide(color: Pal.line),
      ),
      hintStyle: const TextStyle(color: Pal.mut),
    ),
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith(
        (s) => s.contains(WidgetState.selected) ? Pal.onPri : Pal.mut,
      ),
      trackColor: WidgetStateProperty.resolveWith(
        (s) => s.contains(WidgetState.selected)
            ? Pal.pri
            : const Color(0xFF3A4A45),
      ),
    ),
    dividerColor: Pal.line,
  );
}
