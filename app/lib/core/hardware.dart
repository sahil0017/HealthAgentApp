import 'dart:typed_data';

import 'package:flutter/foundation.dart'
    show TargetPlatform, defaultTargetPlatform;

import 'package:geocoding/geocoding.dart';
import 'package:geolocator/geolocator.dart';
import 'package:health/health.dart';
import 'package:image_picker/image_picker.dart';
import 'package:local_auth/local_auth.dart';
import 'package:pdf/widgets.dart' as pw;

/// Coarse area only: coordinates are rounded on the phone to a 0.05 degree grid (about 5 km).
class Area {
  const Area({
    required this.lat,
    required this.lng,
    this.city,
    this.region,
    this.countryCode,
    this.country,
  });
  final double lat;
  final double lng;
  final String? city;
  final String? region;
  final String? countryCode;
  final String? country;

  @override
  bool operator ==(Object o) => o is Area && o.lat == lat && o.lng == lng;
  @override
  int get hashCode => Object.hash(lat, lng);

  String get label =>
      [city, region].whereType<String>().where((s) => s.isNotEmpty).join(', ');
}

double roundCoord(double v) => (v / 0.05).round() * 0.05;

abstract class LocationSource {
  /// Returns null when permission is denied or location is unavailable.
  Future<Area?> currentArea();
}

class PhoneLocation implements LocationSource {
  @override
  Future<Area?> currentArea() async {
    if (!await Geolocator.isLocationServiceEnabled()) return null;
    var perm = await Geolocator.checkPermission();
    if (perm == LocationPermission.denied)
      perm = await Geolocator.requestPermission();
    if (perm == LocationPermission.denied ||
        perm == LocationPermission.deniedForever)
      return null;
    final p = await Geolocator.getCurrentPosition(
      locationSettings: const LocationSettings(accuracy: LocationAccuracy.low),
    );
    final lat = roundCoord(p.latitude), lng = roundCoord(p.longitude);
    try {
      final marks = await Geocoding().placemarkFromCoordinates(lat, lng);
      final m = marks.isEmpty ? null : marks.first;
      return Area(
        lat: lat,
        lng: lng,
        city: m?.locality,
        region: m?.administrativeArea,
        countryCode: m?.isoCountryCode,
        country: m?.country,
      );
    } catch (_) {
      return Area(lat: lat, lng: lng);
    }
  }
}

class HealthSample {
  const HealthSample(
    this.metric,
    this.value,
    this.unit,
    this.at,
    this.externalId,
  );
  final String metric;
  final double value;
  final String unit;
  final DateTime at;
  final String externalId;
}

abstract class HealthSource {
  String get sourceName;
  Future<bool> authorize();
  Future<List<HealthSample>> read({int days = 7});
}

/// Maps the platform health type to the backend metric key and its canonical unit.
const _types = <HealthDataType, (String, String)>{
  HealthDataType.STEPS: ('steps', 'count'),
  HealthDataType.HEART_RATE: ('heart_rate', 'bpm'),
  HealthDataType.RESTING_HEART_RATE: ('resting_heart_rate', 'bpm'),
  HealthDataType.BLOOD_OXYGEN: ('oxygen_saturation', '%'),
  HealthDataType.WEIGHT: ('body_weight', 'kg'),
  HealthDataType.BLOOD_GLUCOSE: ('blood_glucose', 'mg/dL'),
  HealthDataType.BODY_TEMPERATURE: ('body_temperature', '°C'),
};

class PhoneHealth implements HealthSource {
  final _h = Health();
  @override
  String get sourceName =>
      defaultTargetPlatform == TargetPlatform.iOS ? 'healthkit' : 'import';

  @override
  Future<bool> authorize() async {
    await _h.configure();
    final t = _types.keys.toList();
    return _h.requestAuthorization(
      t,
      permissions: t.map((_) => HealthDataAccess.READ).toList(),
    );
  }

  @override
  Future<List<HealthSample>> read({int days = 7}) async {
    final end = DateTime.now();
    final pts = await _h.getHealthDataFromTypes(
      types: _types.keys.toList(),
      startTime: end.subtract(Duration(days: days)),
      endTime: end,
    );
    final out = <HealthSample>[];
    for (final p in _h.removeDuplicates(pts)) {
      final meta = _types[p.type];
      final v = p.value;
      if (meta == null || v is! NumericHealthValue) continue;
      var n = v.numericValue.toDouble();
      // HealthKit/Health Connect report oxygen saturation as a 0-1 fraction on some versions.
      if (meta.$1 == 'oxygen_saturation' && n <= 1) n *= 100;
      out.add(HealthSample(meta.$1, n, meta.$2, p.dateFrom, p.uuid));
    }
    return out;
  }
}

abstract class Scanner {
  /// Captures one or more pages and returns a single PDF, or null if cancelled.
  Future<({Uint8List bytes, String name})?> scan();
}

class CameraScanner implements Scanner {
  @override
  Future<({Uint8List bytes, String name})?> scan() async {
    final f = await ImagePicker().pickImage(
      source: ImageSource.camera,
      imageQuality: 85,
      maxWidth: 2000,
    );
    if (f == null) return null;
    final pages = [await f.readAsBytes()];
    final doc = pw.Document();
    for (final b in pages) {
      doc.addPage(
        pw.Page(
          build: (_) => pw.Center(
            child: pw.Image(pw.MemoryImage(b), fit: pw.BoxFit.contain),
          ),
        ),
      );
    }
    return (
      bytes: Uint8List.fromList(await doc.save()),
      name: 'scan-${DateTime.now().millisecondsSinceEpoch}.pdf',
    );
  }
}

abstract class Biometric {
  Future<bool> available();
  Future<bool> confirm(String reason);
}

class PhoneBiometric implements Biometric {
  final _a = LocalAuthentication();
  @override
  Future<bool> available() async =>
      await _a.canCheckBiometrics || await _a.isDeviceSupported();
  @override
  Future<bool> confirm(String reason) =>
      _a.authenticate(localizedReason: reason);
}
