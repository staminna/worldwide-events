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

  group('RouteStep.instruction', () {
    RouteStep step(String type, String modifier, String name) => RouteStep(
      distanceMeters: 100,
      name: name,
      maneuverType: type,
      maneuverModifier: modifier,
      lat: 0,
      lon: 0,
    );

    test('turn with modifier and street name', () {
      expect(step('turn', 'left', 'Rua X').instruction, 'Turn left onto Rua X');
    });

    test('slight turns keep the OSRM modifier wording', () {
      expect(step('turn', 'slight right', '').instruction, 'Turn slight right');
    });

    test('arrive and depart', () {
      expect(step('arrive', '', '').instruction, 'You have arrived');
      expect(step('depart', '', 'Rua Y').instruction, 'Head out on Rua Y');
    });

    test('straight and unnamed default to continue', () {
      expect(step('continue', 'straight', '').instruction, 'Continue');
      expect(step('turn', 'straight', 'Rua Z').instruction, 'Continue on Rua Z');
    });

    test('WalkRoute without steps still constructs const with empty steps', () {
      const r = WalkRoute(distanceMeters: 1, durationSeconds: 1, geometry: {});
      expect(r.steps, isEmpty);
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
