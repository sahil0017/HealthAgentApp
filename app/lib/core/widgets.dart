import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'state.dart';
import 'theme.dart';

class SCard extends StatelessWidget {
  const SCard({
    super.key,
    required this.child,
    this.onTap,
    this.color,
    this.padding = const EdgeInsets.all(12),
  });
  final Widget child;
  final VoidCallback? onTap;
  final Color? color;
  final EdgeInsets padding;
  @override
  Widget build(BuildContext context) => Card(
    color: color,
    margin: const EdgeInsets.only(bottom: 8),
    child: InkWell(
      borderRadius: BorderRadius.circular(14),
      onTap: onTap,
      child: Padding(padding: padding, child: child),
    ),
  );
}

class SectionTitle extends StatelessWidget {
  const SectionTitle(this.text, {super.key, this.trailing});
  final String text;
  final Widget? trailing;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(2, 10, 2, 6),
    child: Row(
      children: [
        Expanded(
          child: Text(
            text,
            style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
          ),
        ),
        ?trailing,
      ],
    ),
  );
}

class Pill extends StatelessWidget {
  const Pill(this.text, {super.key, this.fg = Pal.pri, this.bg = Pal.priBg});
  final String text;
  final Color fg, bg;
  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
    decoration: BoxDecoration(
      color: bg,
      borderRadius: BorderRadius.circular(99),
    ),
    child: Text(
      text,
      style: TextStyle(fontSize: 9.5, color: fg, fontWeight: FontWeight.w600),
    ),
  );

  factory Pill.flag(String? f) => switch (f) {
    'above_range' ||
    'high' ||
    'critical_high' ||
    'abnormal' => const Pill('Above range', fg: Pal.bad, bg: Pal.badBg),
    'below_range' ||
    'low' ||
    'critical_low' => const Pill('Below range', fg: Pal.warn, bg: Pal.warnBg),
    'within_range' ||
    'normal' => const Pill('In range', fg: Pal.ok, bg: Pal.okBg),
    _ => const Pill('No range', fg: Pal.mut, bg: Pal.line),
  };
}

class Ring extends StatelessWidget {
  const Ring({
    super.key,
    required this.value,
    required this.label,
    this.size = 54,
    this.color = Pal.pri,
  });
  final double value;
  final String label;
  final double size;
  final Color color;
  @override
  Widget build(BuildContext context) => SizedBox(
    width: size,
    height: size,
    child: Stack(
      alignment: Alignment.center,
      children: [
        SizedBox.expand(
          child: CircularProgressIndicator(
            value: value.clamp(0, 1),
            strokeWidth: 5,
            color: color,
            backgroundColor: Pal.line,
          ),
        ),
        Text(
          label,
          style: const TextStyle(fontSize: 11, fontWeight: FontWeight.w700),
        ),
      ],
    ),
  );
}

/// Position of [value] within a reference range, drawn as a bar with a marker.
class RangeBar extends StatelessWidget {
  const RangeBar({super.key, required this.value, this.low, this.high});
  final double value;
  final double? low, high;
  @override
  Widget build(BuildContext context) {
    if (low == null || high == null || high! <= low!)
      return const SizedBox(height: 6);
    final span = high! - low!;
    final pos = ((value - (low! - span * .5)) / (span * 2)).clamp(0.0, 1.0);
    final inRange = value >= low! && value <= high!;
    return LayoutBuilder(
      builder: (_, c) {
        return SizedBox(
          height: 8,
          child: Stack(
            clipBehavior: Clip.none,
            children: [
              Positioned.fill(
                top: 2,
                bottom: 2,
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: Pal.line,
                    borderRadius: BorderRadius.circular(4),
                  ),
                ),
              ),
              Positioned(
                left: c.maxWidth * .25,
                width: c.maxWidth * .5,
                top: 2,
                bottom: 2,
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: Pal.okBg,
                    borderRadius: BorderRadius.circular(4),
                  ),
                ),
              ),
              Positioned(
                left: c.maxWidth * pos - 3,
                top: 0,
                child: Container(
                  width: 6,
                  height: 8,
                  decoration: BoxDecoration(
                    color: inRange ? Pal.ok : Pal.warn,
                    borderRadius: BorderRadius.circular(3),
                  ),
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}

class Sparkline extends StatelessWidget {
  const Sparkline(
    this.values, {
    super.key,
    this.height = 28,
    this.color = Pal.pri,
  });
  final List<double> values;
  final double height;
  final Color color;
  @override
  Widget build(BuildContext context) => SizedBox(
    height: height,
    child: CustomPaint(
      painter: _SparkPainter(values, color),
      size: Size.infinite,
    ),
  );
}

class _SparkPainter extends CustomPainter {
  _SparkPainter(this.v, this.color);
  final List<double> v;
  final Color color;
  @override
  void paint(Canvas canvas, Size size) {
    if (v.length < 2) return;
    final lo = v.reduce((a, b) => a < b ? a : b),
        hi = v.reduce((a, b) => a > b ? a : b);
    final span = (hi - lo) == 0 ? 1 : hi - lo;
    final path = Path();
    for (var i = 0; i < v.length; i++) {
      final x = size.width * i / (v.length - 1);
      final y = size.height - (v[i] - lo) / span * (size.height - 4) - 2;
      i == 0 ? path.moveTo(x, y) : path.lineTo(x, y);
    }
    canvas.drawPath(
      path,
      Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.8
        ..strokeJoin = StrokeJoin.round,
    );
  }

  @override
  bool shouldRepaint(_SparkPainter o) => o.v != v;
}

class Empty extends StatelessWidget {
  const Empty(this.icon, this.title, {super.key, this.body, this.action});
  final IconData icon;
  final String title;
  final String? body;
  final Widget? action;
  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(28),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 34, color: Pal.mut),
          const SizedBox(height: 10),
          Text(
            title,
            textAlign: TextAlign.center,
            style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
          ),
          if (body != null) ...[
            const SizedBox(height: 4),
            Text(
              body!,
              textAlign: TextAlign.center,
              style: const TextStyle(fontSize: 11, color: Pal.mut),
            ),
          ],
          if (action != null) ...[const SizedBox(height: 14), action!],
        ],
      ),
    ),
  );
}

class ErrorView extends StatelessWidget {
  const ErrorView(this.error, {super.key, this.onRetry});
  final Object error;
  final VoidCallback? onRetry;
  @override
  Widget build(BuildContext context) => Empty(
    Icons.cloud_off_rounded,
    friendly(error),
    body: 'Check your connection and try again.',
    action: onRetry == null
        ? null
        : OutlinedButton(onPressed: onRetry, child: const Text('Retry')),
  );
}

/// Renders an AsyncValue with consistent loading and error states.
class AsyncBody<T> extends StatelessWidget {
  const AsyncBody(this.value, {super.key, required this.builder, this.onRetry});
  final AsyncValue<T> value;
  final Widget Function(T data) builder;
  final VoidCallback? onRetry;
  @override
  Widget build(BuildContext context) => value.when(
    data: builder,
    loading: () => const Skeleton(),
    error: (e, _) => ErrorView(e, onRetry: onRetry),
  );
}

/// Pulsing placeholder rows shown while content loads.
class Skeleton extends StatefulWidget {
  const Skeleton({super.key, this.rows = 4});
  final int rows;
  @override
  State<Skeleton> createState() => _SkeletonState();
}

class _SkeletonState extends State<Skeleton>
    with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 900),
  )..repeat(reverse: true);

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: _c,
    builder: (_, _) => Opacity(
      opacity: 0.35 + 0.4 * _c.value,
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (var i = 0; i < widget.rows; i++)
              Container(
                height: 56,
                margin: const EdgeInsets.only(bottom: 10),
                decoration: BoxDecoration(
                  color: Pal.card,
                  borderRadius: BorderRadius.circular(14),
                ),
              ),
          ],
        ),
      ),
    ),
  );
}

void toast(BuildContext c, String m) => ScaffoldMessenger.of(c)
  ..hideCurrentSnackBar()
  ..showSnackBar(SnackBar(content: Text(m)));

num? numOf(Object? v) => v is num ? v : (v is String ? num.tryParse(v) : null);
String fmtNum(num? n) => n == null
    ? '—'
    : (n == n.roundToDouble()
          ? n.toInt().toString()
          : n.toStringAsFixed(n.abs() < 10 ? 2 : 1));

/// True when a result sits outside its reference range. Results with no range are neither in nor out.
bool outOfRange(String? flag) => const {
  'above_range',
  'below_range',
  'high',
  'low',
  'critical_high',
  'critical_low',
  'abnormal',
}.contains(flag);

bool inRange(String? flag) => flag == 'within_range' || flag == 'normal';

String rangeText(Map? r, String unit) {
  final lo = numOf(r?['low']), hi = numOf(r?['high']);
  final u = unit.isEmpty ? '' : ' $unit';
  if (lo != null && hi != null) return '${fmtNum(lo)}–${fmtNum(hi)}$u';
  if (hi != null) return 'up to ${fmtNum(hi)}$u';
  if (lo != null) return 'at least ${fmtNum(lo)}$u';
  return '';
}

String shortDate(String? iso) {
  final d = DateTime.tryParse(iso ?? '');
  if (d == null) return '';
  const m = [
    'Jan',
    'Feb',
    'Mar',
    'Apr',
    'May',
    'Jun',
    'Jul',
    'Aug',
    'Sep',
    'Oct',
    'Nov',
    'Dec',
  ];
  return '${d.day} ${m[d.month - 1]} ${d.year}';
}
