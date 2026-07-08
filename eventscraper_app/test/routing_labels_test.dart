import 'package:flutter_test/flutter_test.dart';

import 'package:eventscraper_app/api/routing_api.dart';

void main() {
  group('WalkRoute.walkLabel', () {
    test('short walks show meters', () {
      const r = WalkRoute(
        distanceMeters: 450,
        durationSeconds: 360,
        geometry: {},
      );
      expect(r.walkLabel, '6 min walk · 450 m');
    });

    test('exactly 1000 m switches to km with one decimal', () {
      const r = WalkRoute(
        distanceMeters: 1000,
        durationSeconds: 720,
        geometry: {},
      );
      expect(r.walkLabel, '12 min walk · 1.0 km');
    });

    test('sub-minute duration rounds sensibly', () {
      const r = WalkRoute(
        distanceMeters: 40,
        durationSeconds: 29,
        geometry: {},
      );
      expect(r.walkLabel, '0 min walk · 40 m');
    });
  });

  group('WalkTour.totalLabel', () {
    test('multi-stop totals format like the single-leg label', () {
      const t = WalkTour(
        distanceMeters: 3250,
        durationSeconds: 2400,
        geometry: {},
        legSeconds: [1200, 1200],
      );
      expect(t.totalLabel, '40 min walking · 3.3 km total');
    });

    test('tiny tour stays in meters', () {
      const t = WalkTour(
        distanceMeters: 90,
        durationSeconds: 60,
        geometry: {},
        legSeconds: [60],
      );
      expect(t.totalLabel, '1 min walking · 90 m total');
    });
  });
}
