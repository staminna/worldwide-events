import 'dart:ui';

import 'package:flutter_test/flutter_test.dart';

import 'package:eventscraper_app/util/geo.dart';

void main() {
  group('haversineMeters', () {
    test('identical points are zero meters apart', () {
      expect(haversineMeters(38.72, -9.14, 38.72, -9.14), 0);
    });

    test('Lisbon to Porto is roughly 274 km', () {
      final d = haversineMeters(38.7223, -9.1393, 41.1579, -8.6291);
      expect(d, closeTo(274000, 5000));
    });

    test('one degree of longitude at the equator is ~111.2 km', () {
      final d = haversineMeters(0, 0, 0, 1);
      expect(d, closeTo(111195, 200));
    });

    test('is symmetric', () {
      final ab = haversineMeters(38.72, -9.14, 41.16, -8.63);
      final ba = haversineMeters(41.16, -8.63, 38.72, -9.14);
      expect(ab, closeTo(ba, 0.0001));
    });

    test('antipodal points are half the Earth circumference apart', () {
      final d = haversineMeters(0, 0, 0, 180);
      expect(d, closeTo(20015000, 10000)); // π * 6371 km
    });
  });

  group('hexColor', () {
    test('formats channels as lowercase #rrggbb', () {
      expect(hexColor(const Color(0xFF123456)), '#123456');
      expect(hexColor(const Color(0xFFFF0000)), '#ff0000');
    });

    test('pads small values with leading zeros', () {
      expect(hexColor(const Color(0xFF0000FF)), '#0000ff');
      expect(hexColor(const Color(0xFF000000)), '#000000');
    });

    test('alpha is dropped, not encoded', () {
      // MapLibre's #rrggbb notation takes no alpha — a translucent color
      // must still serialize to 6 hex digits.
      expect(hexColor(const Color(0x80FF0000)), '#ff0000');
      expect(hexColor(const Color(0x00FFFFFF)), '#ffffff');
    });
  });
}
