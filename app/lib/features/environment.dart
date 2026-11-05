import 'dart:convert';
import 'dart:math' as math;

import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../core/hardware.dart';
import '../core/state.dart';
import '../core/theme.dart';
import '../core/widgets.dart';
import 'places.dart';

const _note = TextStyle(fontSize: 10.5, color: Pal.mut);

final _liveKeys = <String>{};

/// Opens instantly from the last reading saved on the phone, then refreshes in the background.
/// Pull to refresh always goes live.
final environmentProvider = FutureProvider.family<Json, Area>((ref, a) async {
  final key = 'env_${a.lat.toStringAsFixed(2)}_${a.lng.toStringAsFixed(2)}';
  final prefs = await SharedPreferences.getInstance();
  Future<Json> live() async {
    final d = await ref
        .read(apiProvider)
        .map('/environment/now?lat=${a.lat}&lng=${a.lng}');
    _liveKeys.add(key);
    await prefs.setString(
      key,
      jsonEncode({'at': DateTime.now().millisecondsSinceEpoch, 'd': d}),
    );
    return d;
  }

  final saved = prefs.getString(key);
  if (saved != null && !_liveKeys.contains(key)) {
    try {
      final c = jsonDecode(saved) as Map<String, dynamic>;
      if (DateTime.now().millisecondsSinceEpoch - (c['at'] as int) <
          6 * 3600 * 1000) {
        _liveKeys.add(key);
        live()
            .then((_) => ref.invalidateSelf())
            .catchError((_) => <String, dynamic>{});
        return (c['d'] as Map).cast<String, dynamic>();
      }
    } catch (_) {}
  }
  return live();
});

String _n(Object? v, [int dp = 0]) {
  final x = numOf(v);
  return x == null ? '—' : x.toStringAsFixed(dp);
}

Json _map(Object? v) => (v as Map?)?.cast<String, dynamic>() ?? {};
List<num?> _series(Object? v) =>
    ((v as List?) ?? []).map(numOf).toList(growable: false);

/// US EPA AQI band colours.
Color aqiColor(num? aqi) {
  if (aqi == null) return Pal.mut;
  if (aqi <= 50) return const Color(0xFF5FD18B);
  if (aqi <= 100) return const Color(0xFFF2D14A);
  if (aqi <= 150) return const Color(0xFFF2994A);
  if (aqi <= 200) return const Color(0xFFFF6B5E);
  if (aqi <= 300) return const Color(0xFFB06AE8);
  return const Color(0xFF9C2F4A);
}

Color _uvColor(num? uv) {
  if (uv == null) return Pal.mut;
  if (uv < 3) return const Color(0xFF5FD18B);
  if (uv < 6) return const Color(0xFFF2D14A);
  if (uv < 8) return const Color(0xFFF2994A);
  if (uv < 11) return const Color(0xFFFF6B5E);
  return const Color(0xFFB06AE8);
}

/// WMO weather interpretation codes, a published public standard.
(IconData, String) _wmo(num? code, {bool night = false}) {
  final c = code?.toInt();
  if (c == null) return (Icons.help_outline, 'Unknown');
  if (c == 0) {
    return night
        ? (Icons.nightlight_round, 'Clear')
        : (Icons.wb_sunny_rounded, 'Clear sky');
  }
  if (c <= 2) return (Icons.wb_cloudy_outlined, 'Partly cloudy');
  if (c == 3) return (Icons.cloud, 'Overcast');
  if (c == 45 || c == 48) return (Icons.foggy, 'Fog');
  if (c >= 51 && c <= 57) return (Icons.grain, 'Drizzle');
  if (c >= 61 && c <= 67) return (Icons.umbrella, 'Rain');
  if (c >= 71 && c <= 77) return (Icons.ac_unit, 'Snow');
  if (c >= 80 && c <= 82) return (Icons.shower, 'Rain showers');
  if (c == 85 || c == 86) return (Icons.ac_unit, 'Snow showers');
  if (c >= 95) return (Icons.thunderstorm, 'Thunderstorm');
  return (Icons.cloud, 'Cloudy');
}

String _compass(num? deg) {
  if (deg == null) return '';
  const p = ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'];
  return p[((deg % 360) / 45).round() % 8];
}

String _hhmm(Object? iso) {
  final s = '$iso';
  return s.length >= 16 ? s.substring(11, 16) : '—';
}

String _hour(Object? iso) {
  final h = int.tryParse(_hhmm(iso).substring(0, 2));
  if (h == null) return '';
  return '${h % 12 == 0 ? 12 : h % 12}${h < 12 ? 'a' : 'p'}';
}

/// WHO 2021 24-hour air quality guideline levels (µg/m³), used only as a reference marker.
const _who = {
  'pm2_5': 15.0,
  'pm10': 45.0,
  'nitrogen_dioxide': 25.0,
  'sulphur_dioxide': 40.0,
};

/// Live air quality and weather for the user's approximate area.
class EnvironmentScreen extends ConsumerWidget {
  const EnvironmentScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) =>
      NeedsArea('Environmental Health', (area) {
        return RefreshIndicator(
          onRefresh: () async {
            ref.invalidate(environmentProvider(area));
            await ref.read(environmentProvider(area).future);
          },
          child: AsyncBody<Json>(
            ref.watch(environmentProvider(area)),
            onRetry: () => ref.invalidate(environmentProvider(area)),
            builder: (d) {
              final air = _map(_map(d['air'])['current']);
              final airHourly = _map(_map(d['air'])['hourly']);
              final wx = _map(_map(d['weather'])['current']);
              final daily = _map(_map(d['weather'])['daily']);
              final wxHourly = _map(_map(d['weather'])['hourly']);
              final station = (d['station'] as Map?)?.cast<String, dynamic>();
              final hasAir = air.isNotEmpty, hasWx = wx.isNotEmpty;
              return ListView(
                physics: const AlwaysScrollableScrollPhysics(),
                padding: const EdgeInsets.all(14),
                children: [
                  if (hasWx) _WeatherHero(wx: wx, daily: daily),
                  if (hasAir) _AirHero(d: d, air: air),
                  if (hasAir && _series(airHourly['us_aqi']).length > 1)
                    _AqiForecast(
                      times: (airHourly['time'] as List?) ?? [],
                      aqi: _series(airHourly['us_aqi']),
                    ),
                  if (hasAir) _Pollutants(air: air),
                  if (hasWx && _series(wxHourly['temperature_2m']).length > 1)
                    _HourlyTemp(h: wxHourly),
                  if (hasWx) _Details(wx: wx, daily: daily, d: d, air: air),
                  if (daily['sunrise'] is List && daily['sunset'] is List)
                    _SunCard(daily: daily),
                  if (station != null) _Station(station),
                  if (!hasAir && !hasWx)
                    const Empty(Icons.cloud_off, 'No data for this area'),
                  const Padding(
                    padding: EdgeInsets.all(8),
                    child: Text(
                      'Source: Open-Meteo, for your approximate area. AQI follows the US EPA scale, UV follows the WHO scale and pollutant markers use WHO 2021 guideline levels.',
                      style: _note,
                    ),
                  ),
                ],
              );
            },
          ),
        );
      });
}

class _WeatherHero extends StatelessWidget {
  const _WeatherHero({required this.wx, required this.daily});
  final Json wx, daily;
  @override
  Widget build(BuildContext context) {
    final night = numOf(wx['is_day']) == 0;
    final (icon, label) = _wmo(numOf(wx['weather_code']), night: night);
    final hi = _series(daily['temperature_2m_max']).firstOrNull;
    final lo = _series(daily['temperature_2m_min']).firstOrNull;
    return SCard(
      padding: const EdgeInsets.all(16),
      child: Row(
        children: [
          Icon(icon, size: 52, color: night ? Pal.pri : Pal.warn),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${_n(wx['temperature_2m'])}°C',
                  style: const TextStyle(
                    fontSize: 38,
                    fontWeight: FontWeight.w700,
                    height: 1.05,
                  ),
                ),
                Text(label, style: const TextStyle(fontSize: 13)),
                Text(
                  'Feels like ${_n(wx['apparent_temperature'])}°C'
                  '${hi != null && lo != null ? '  ·  H ${_n(hi)}°  L ${_n(lo)}°' : ''}',
                  style: _note,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _AirHero extends StatelessWidget {
  const _AirHero({required this.d, required this.air});
  final Json d, air;
  @override
  Widget build(BuildContext context) {
    final aqi = numOf(air['us_aqi']);
    final col = aqiColor(aqi);
    return SCard(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              SizedBox(
                width: 84,
                height: 84,
                child: Stack(
                  alignment: Alignment.center,
                  children: [
                    SizedBox.expand(
                      child: CircularProgressIndicator(
                        value: ((aqi ?? 0) / 300).clamp(0.02, 1).toDouble(),
                        strokeWidth: 8,
                        color: col,
                        backgroundColor: Pal.line,
                        strokeCap: StrokeCap.round,
                      ),
                    ),
                    Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          _n(aqi),
                          style: const TextStyle(
                            fontSize: 26,
                            fontWeight: FontWeight.w800,
                            height: 1,
                          ),
                        ),
                        const Text('US AQI', style: _note),
                      ],
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 16),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('Air quality', style: _note),
                    const SizedBox(height: 4),
                    Text(
                      '${d['air_category'] ?? '—'}',
                      style: TextStyle(
                        fontSize: 17,
                        fontWeight: FontWeight.w700,
                        color: col,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          _AqiScale(aqi: aqi),
          const SizedBox(height: 10),
          Text(
            '${d['air_advice'] ?? ''}',
            style: const TextStyle(fontSize: 12),
          ),
        ],
      ),
    );
  }
}

/// A band-coloured 0–300 scale with a marker at the current AQI.
class _AqiScale extends StatelessWidget {
  const _AqiScale({required this.aqi});
  final num? aqi;
  @override
  Widget build(BuildContext context) {
    const stops = [50, 100, 150, 200, 300];
    return LayoutBuilder(
      builder: (_, c) {
        final w = c.maxWidth;
        final x = ((aqi ?? 0) / 300).clamp(0, 1) * w;
        return SizedBox(
          height: 14,
          child: Stack(
            clipBehavior: Clip.none,
            children: [
              Row(
                children: [
                  for (var i = 0; i < stops.length; i++)
                    Expanded(
                      flex: stops[i] - (i == 0 ? 0 : stops[i - 1]),
                      child: Container(
                        height: 6,
                        margin: const EdgeInsets.only(top: 4, right: 1.5),
                        decoration: BoxDecoration(
                          color: aqiColor(stops[i] - 1),
                          borderRadius: BorderRadius.circular(3),
                        ),
                      ),
                    ),
                ],
              ),
              Positioned(
                left: x.toDouble() - 5,
                top: 0,
                child: Container(
                  width: 10,
                  height: 14,
                  decoration: BoxDecoration(
                    color: Pal.ink,
                    borderRadius: BorderRadius.circular(3),
                    border: Border.all(color: Pal.bg, width: 1.5),
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

class _AqiForecast extends StatelessWidget {
  const _AqiForecast({required this.times, required this.aqi});
  final List times;
  final List<num?> aqi;
  @override
  Widget build(BuildContext context) {
    final vals = aqi.map((v) => (v ?? 0).toDouble()).toList();
    final best = vals.indexOf(vals.reduce(math.min));
    final worst = vals.indexOf(vals.reduce(math.max));
    String at(int i) => i < times.length ? _hour(times[i]) : '';
    return SCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Next 24 hours · air quality',
            style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 12),
          SizedBox(
            height: 120,
            child: BarChart(
              BarChartData(
                maxY: math.max(100, vals.reduce(math.max) * 1.15),
                gridData: FlGridData(
                  drawVerticalLine: false,
                  horizontalInterval: 50,
                  getDrawingHorizontalLine: (_) =>
                      const FlLine(color: Pal.line, strokeWidth: 1),
                ),
                borderData: FlBorderData(show: false),
                titlesData: FlTitlesData(
                  topTitles: const AxisTitles(),
                  rightTitles: const AxisTitles(),
                  leftTitles: AxisTitles(
                    sideTitles: SideTitles(
                      showTitles: true,
                      reservedSize: 28,
                      interval: 50,
                      getTitlesWidget: (v, m) =>
                          Text(v.toInt().toString(), style: _note),
                    ),
                  ),
                  bottomTitles: AxisTitles(
                    sideTitles: SideTitles(
                      showTitles: true,
                      interval: 6,
                      reservedSize: 18,
                      getTitlesWidget: (v, m) => v.toInt() % 6 == 0
                          ? Text(at(v.toInt()), style: _note)
                          : const SizedBox(),
                    ),
                  ),
                ),
                barTouchData: BarTouchData(
                  touchTooltipData: BarTouchTooltipData(
                    getTooltipItem: (g, gi, r, ri) => BarTooltipItem(
                      '${at(gi)}  AQI ${r.toY.round()}',
                      const TextStyle(fontSize: 11, color: Pal.ink),
                    ),
                  ),
                ),
                barGroups: [
                  for (var i = 0; i < vals.length; i++)
                    BarChartGroupData(
                      x: i,
                      barRods: [
                        BarChartRodData(
                          toY: vals[i],
                          width: 6,
                          color: aqiColor(vals[i]),
                          borderRadius: BorderRadius.circular(2),
                        ),
                      ],
                    ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'Cleanest around ${at(best)} (AQI ${vals[best].round()}) · '
            'worst around ${at(worst)} (AQI ${vals[worst].round()})',
            style: _note,
          ),
        ],
      ),
    );
  }
}

class _Pollutants extends StatelessWidget {
  const _Pollutants({required this.air});
  final Json air;
  @override
  Widget build(BuildContext context) {
    const rows = [
      ('pm2_5', 'PM2.5', 'fine particles'),
      ('pm10', 'PM10', 'coarse particles'),
      ('nitrogen_dioxide', 'NO₂', 'traffic'),
      ('sulphur_dioxide', 'SO₂', 'industry'),
      ('ozone', 'Ozone', 'smog'),
    ];
    final shown = rows.where((r) => numOf(air[r.$1]) != null).toList();
    if (shown.isEmpty) return const SizedBox();
    return SCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Pollutants (µg/m³)',
            style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 10),
          for (final r in shown)
            _PollutantBar(
              label: r.$2,
              hint: r.$3,
              value: numOf(air[r.$1])!.toDouble(),
              guide: _who[r.$1],
            ),
          const Text('│ marks the WHO 24-hour guideline', style: _note),
        ],
      ),
    );
  }
}

class _PollutantBar extends StatelessWidget {
  const _PollutantBar({
    required this.label,
    required this.hint,
    required this.value,
    this.guide,
  });
  final String label, hint;
  final double value;
  final double? guide;
  @override
  Widget build(BuildContext context) {
    final over = guide != null && value > guide!;
    final scale = guide == null ? math.max(value, 1) * 1.3 : guide! * 3;
    final col = guide == null
        ? Pal.pri
        : value <= guide!
        ? Pal.ok
        : value <= guide! * 2
        ? Pal.warn
        : Pal.bad;
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                label,
                style: const TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(width: 6),
              Text(hint, style: _note),
              const Spacer(),
              Text(
                _n(value, value < 10 ? 1 : 0),
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w700,
                  color: over ? col : Pal.ink,
                ),
              ),
              if (guide != null)
                Text(
                  '  ${(value / guide!).toStringAsFixed(1)}× WHO',
                  style: _note,
                ),
            ],
          ),
          const SizedBox(height: 4),
          LayoutBuilder(
            builder: (_, c) => Stack(
              clipBehavior: Clip.none,
              children: [
                Container(
                  height: 6,
                  decoration: BoxDecoration(
                    color: Pal.line,
                    borderRadius: BorderRadius.circular(3),
                  ),
                ),
                Container(
                  height: 6,
                  width: c.maxWidth * (value / scale).clamp(0.02, 1).toDouble(),
                  decoration: BoxDecoration(
                    color: col,
                    borderRadius: BorderRadius.circular(3),
                  ),
                ),
                if (guide != null)
                  Positioned(
                    left: c.maxWidth * (guide! / scale),
                    top: -3,
                    child: Container(width: 2, height: 12, color: Pal.ink),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _HourlyTemp extends StatelessWidget {
  const _HourlyTemp({required this.h});
  final Json h;
  @override
  Widget build(BuildContext context) {
    final temps = _series(h['temperature_2m'])
        .map((v) => (v ?? 0).toDouble())
        .toList();
    final rain = _series(h['precipitation_probability']);
    final times = (h['time'] as List?) ?? [];
    final codes = _series(h['weather_code']);
    final lo = temps.reduce(math.min), hi = temps.reduce(math.max);
    final maxRain = rain.whereType<num>().fold<num>(0, math.max);
    return SCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Next 24 hours · temperature',
            style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 12),
          SizedBox(
            height: 120,
            child: LineChart(
              LineChartData(
                minY: lo - 2,
                maxY: hi + 2,
                gridData: FlGridData(
                  drawVerticalLine: false,
                  getDrawingHorizontalLine: (_) =>
                      const FlLine(color: Pal.line, strokeWidth: 1),
                ),
                borderData: FlBorderData(show: false),
                titlesData: FlTitlesData(
                  topTitles: const AxisTitles(),
                  rightTitles: const AxisTitles(),
                  leftTitles: AxisTitles(
                    sideTitles: SideTitles(
                      showTitles: true,
                      reservedSize: 28,
                      getTitlesWidget: (v, m) => v == m.min || v == m.max
                          ? const SizedBox()
                          : Text('${v.round()}°', style: _note),
                    ),
                  ),
                  bottomTitles: AxisTitles(
                    sideTitles: SideTitles(
                      showTitles: true,
                      interval: 6,
                      reservedSize: 18,
                      getTitlesWidget: (v, m) =>
                          v.toInt() % 6 == 0 && v.toInt() < times.length
                          ? Text(_hour(times[v.toInt()]), style: _note)
                          : const SizedBox(),
                    ),
                  ),
                ),
                lineTouchData: LineTouchData(
                  touchTooltipData: LineTouchTooltipData(
                    getTooltipItems: (spots) => [
                      for (final s in spots)
                        LineTooltipItem(
                          '${s.x.toInt() < times.length ? _hour(times[s.x.toInt()]) : ''}  ${s.y.round()}°C',
                          const TextStyle(fontSize: 11, color: Pal.ink),
                        ),
                    ],
                  ),
                ),
                lineBarsData: [
                  LineChartBarData(
                    spots: [
                      for (var i = 0; i < temps.length; i++)
                        FlSpot(i.toDouble(), temps[i]),
                    ],
                    isCurved: true,
                    color: Pal.warn,
                    barWidth: 2.5,
                    dotData: const FlDotData(show: false),
                    belowBarData: BarAreaData(
                      show: true,
                      color: Pal.warn.withValues(alpha: 0.12),
                    ),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 10),
          const Text('Chance of rain', style: _note),
          const SizedBox(height: 4),
          SizedBox(
            height: 46,
            child: ListView.builder(
              scrollDirection: Axis.horizontal,
              itemCount: math.min(times.length, rain.length),
              itemBuilder: (_, i) {
                final p = (rain[i] ?? 0).toDouble();
                return SizedBox(
                  width: 30,
                  child: Column(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      Container(
                        width: 12,
                        height: 3 + 22 * (p / 100),
                        decoration: BoxDecoration(
                          color: Pal.pri.withValues(
                            alpha: 0.35 + 0.65 * p / 100,
                          ),
                          borderRadius: BorderRadius.circular(3),
                        ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        _hour(times[i]),
                        style: const TextStyle(fontSize: 8.5, color: Pal.mut),
                      ),
                    ],
                  ),
                );
              },
            ),
          ),
          Text(
            maxRain >= 40
                ? 'Rain likely at some point today (up to ${maxRain.round()}%).'
                : 'Rain unlikely over the next 24 hours.',
            style: _note,
          ),
          if (codes.isNotEmpty) const SizedBox(),
        ],
      ),
    );
  }
}

class _Details extends StatelessWidget {
  const _Details({
    required this.wx,
    required this.daily,
    required this.d,
    required this.air,
  });
  final Json wx, daily, d, air;
  @override
  Widget build(BuildContext context) {
    final uv =
        numOf(air['uv_index']) ?? _series(daily['uv_index_max']).firstOrNull;
    final tiles = <(IconData, String, String, String?, Color?)>[
      (
        Icons.water_drop_outlined,
        'Humidity',
        '${_n(wx['relative_humidity_2m'])}%',
        null,
        null,
      ),
      (
        Icons.air,
        'Wind',
        '${_n(wx['wind_speed_10m'])} km/h ${_compass(numOf(wx['wind_direction_10m']))}',
        wx['wind_gusts_10m'] != null
            ? 'Gusts ${_n(wx['wind_gusts_10m'])}'
            : null,
        null,
      ),
      (
        Icons.wb_sunny_outlined,
        'UV index',
        _n(uv, 1),
        d['uv_category'] as String?,
        _uvColor(uv),
      ),
      (Icons.speed, 'Pressure', '${_n(wx['pressure_msl'])} hPa', null, null),
      (
        Icons.cloud_outlined,
        'Cloud cover',
        '${_n(wx['cloud_cover'])}%',
        null,
        null,
      ),
      (
        Icons.umbrella_outlined,
        'Rain now',
        '${_n(wx['precipitation'], 1)} mm',
        null,
        null,
      ),
    ];
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: GridView.count(
        crossAxisCount: 2,
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        mainAxisSpacing: 8,
        crossAxisSpacing: 8,
        childAspectRatio: 2.2,
        children: [
          for (final t in tiles)
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              decoration: BoxDecoration(
                color: Pal.card,
                borderRadius: BorderRadius.circular(14),
                border: Border.all(color: Pal.line),
              ),
              child: Row(
                children: [
                  Icon(t.$1, size: 22, color: t.$5 ?? Pal.pri),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      mainAxisAlignment: MainAxisAlignment.center,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(t.$2, style: _note),
                        Text(
                          t.$3,
                          style: const TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        if (t.$4 != null)
                          Text(
                            t.$4!,
                            style: TextStyle(
                              fontSize: 10,
                              color: t.$5 ?? Pal.mut,
                            ),
                          ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _SunCard extends StatelessWidget {
  const _SunCard({required this.daily});
  final Json daily;
  @override
  Widget build(BuildContext context) {
    final rise = _hhmm((daily['sunrise'] as List).first);
    final set = _hhmm((daily['sunset'] as List).first);
    int mins(String s) {
      final p = s.split(':');
      return p.length == 2
          ? (int.tryParse(p[0]) ?? 0) * 60 + (int.tryParse(p[1]) ?? 0)
          : 0;
    }

    final now = DateTime.now();
    final nowM = now.hour * 60 + now.minute;
    final r = mins(rise), s = mins(set);
    final frac = s > r ? ((nowM - r) / (s - r)).clamp(0, 1).toDouble() : 0.0;
    final len = s - r;
    return SCard(
      child: Row(
        children: [
          const Icon(Icons.wb_twilight, color: Pal.warn),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    Text('Sunrise $rise', style: const TextStyle(fontSize: 12)),
                    Text('Sunset $set', style: const TextStyle(fontSize: 12)),
                  ],
                ),
                const SizedBox(height: 6),
                ClipRRect(
                  borderRadius: BorderRadius.circular(3),
                  child: LinearProgressIndicator(
                    value: frac,
                    minHeight: 6,
                    color: Pal.warn,
                    backgroundColor: Pal.line,
                  ),
                ),
                if (len > 0)
                  Padding(
                    padding: const EdgeInsets.only(top: 4),
                    child: Text(
                      'Daylight ${len ~/ 60}h ${len % 60}m',
                      style: _note,
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Station extends StatelessWidget {
  const _Station(this.station);
  final Json station;
  @override
  Widget build(BuildContext context) => SCard(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'Nearest station: ${station['name']}',
          style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
        ),
        for (final r in ((station['readings'] as List?) ?? []).cast<Json>())
          Text(
            '${r['parameter']}: ${_n(r['value'], 1)} ${r['unit']}',
            style: _note,
          ),
        const Text('Source: OpenAQ', style: _note),
      ],
    ),
  );
}
