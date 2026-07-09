import 'package:flutter_test/flutter_test.dart';

import 'package:eventscraper_app/util/nav_math.dart';

void main() {
  group('bearingDegrees', () {
    test('due north is 0', () {
      expect(bearingDegrees(39.0, -8.0, 40.0, -8.0), closeTo(0, 0.01));
    });

    test('due east on the equator is 90', () {
      expect(bearingDegrees(0, 0, 0, 1), closeTo(90, 0.01));
    });

    test('due south is 180', () {
      expect(bearingDegrees(40.0, -8.0, 39.0, -8.0), closeTo(180, 0.01));
    });

    test('Lisbon to Porto points roughly north', () {
      final b = bearingDegrees(38.7223, -9.1393, 41.1579, -8.6291);
      expect(b, greaterThan(0));
      expect(b, lessThan(30));
    });
  });

  group('angularDeltaDegrees', () {
    test('wraps across north going clockwise', () {
      expect(angularDeltaDegrees(350, 10), 20);
    });

    test('wraps across north going counter-clockwise', () {
      expect(angularDeltaDegrees(10, 350), -20);
    });

    test('identical headings give 0', () {
      expect(angularDeltaDegrees(123, 123), 0);
    });
  });

  group('smoothHeading', () {
    test('moves halfway along the short arc across north', () {
      // 359 toward 1 with alpha 0.5 → 0, never the long way (~180).
      expect(smoothHeading(359, 1, 0.5), closeTo(0, 0.01));
    });

    test('plain interpolation away from the wrap point', () {
      expect(smoothHeading(90, 100, 0.25), closeTo(92.5, 0.01));
    });

    test('alpha 1 lands exactly on the target', () {
      expect(smoothHeading(200, 40, 1), closeTo(40, 0.01));
    });
  });

  group('distanceToPolylineMeters', () {
    // A ~west-east segment through Pombal at lat 39.9155.
    final line = [
      [-8.6300, 39.9155],
      [-8.6200, 39.9155],
    ];

    test('point on the line is ~0', () {
      expect(distanceToPolylineMeters(39.9155, -8.6250, line), lessThan(1));
    });

    test('offset north of the line is the perpendicular distance', () {
      // 0.001° of latitude ≈ 111 m.
      expect(
        distanceToPolylineMeters(39.9165, -8.6250, line),
        closeTo(111, 3),
      );
    });

    test('beyond the segment end clamps to the endpoint', () {
      final atEnd = distanceToPolylineMeters(39.9155, -8.6190, line);
      // 0.001° of longitude at lat 39.9 ≈ 85 m.
      expect(atEnd, closeTo(85, 3));
    });

    test('empty polyline is infinitely far', () {
      expect(distanceToPolylineMeters(39.9, -8.6, []), double.infinity);
    });
  });

  group('advanceStepIndex', () {
    final maneuvers = [
      (lat: 39.9155, lon: -8.6291),
      (lat: 39.9160, lon: -8.6280),
      (lat: 39.9170, lon: -8.6270),
    ];

    test('far from the current maneuver stays put', () {
      expect(
        advanceStepIndex(
          maneuvers: maneuvers,
          current: 0,
          lat: 39.9100,
          lon: -8.6400,
        ),
        0,
      );
    });

    test('within the pass radius advances one', () {
      expect(
        advanceStepIndex(
          maneuvers: maneuvers,
          current: 0,
          lat: 39.9155,
          lon: -8.6291,
        ),
        1,
      );
    });

    test('two coincident maneuvers are skipped together', () {
      final stacked = [
        (lat: 39.9155, lon: -8.6291),
        (lat: 39.9155, lon: -8.6291),
        (lat: 39.9170, lon: -8.6270),
      ];
      expect(
        advanceStepIndex(
          maneuvers: stacked,
          current: 0,
          lat: 39.9155,
          lon: -8.6291,
        ),
        2,
      );
    });

    test('never advances past the last maneuver', () {
      expect(
        advanceStepIndex(
          maneuvers: maneuvers,
          current: 2,
          lat: 39.9170,
          lon: -8.6270,
        ),
        2,
      );
    });
  });
}
