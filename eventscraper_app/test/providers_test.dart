import 'package:flutter_test/flutter_test.dart';

import 'package:eventscraper_app/models/event.dart';
import 'package:eventscraper_app/state/providers.dart';

void main() {
  group('upcomingWeekend', () {
    test('midweek picks the coming Saturday', () {
      final w = upcomingWeekend(DateTime(2026, 7, 8)); // Wednesday
      expect(w.from, DateTime(2026, 7, 11)); // Saturday
      expect(w.to, DateTime(2026, 7, 12, 23, 59, 59)); // Sunday end of day
    });

    test('Monday and Friday both land on the same coming Saturday', () {
      expect(upcomingWeekend(DateTime(2026, 7, 6)).from, DateTime(2026, 7, 11));
      expect(
        upcomingWeekend(DateTime(2026, 7, 10)).from,
        DateTime(2026, 7, 11),
      );
    });

    test('on a Saturday the weekend is today', () {
      final w = upcomingWeekend(DateTime(2026, 7, 11, 18, 30));
      expect(w.from, DateTime(2026, 7, 11)); // date part only
      expect(w.to, DateTime(2026, 7, 12, 23, 59, 59));
    });

    test('on a Sunday the CURRENT weekend is kept, not next week', () {
      final w = upcomingWeekend(DateTime(2026, 7, 12, 11));
      expect(w.from, DateTime(2026, 7, 11)); // yesterday's Saturday
      expect(w.to, DateTime(2026, 7, 12, 23, 59, 59)); // still today
    });

    test('weekend range crosses a month boundary correctly', () {
      final w = upcomingWeekend(DateTime(2026, 7, 31)); // Friday, July 31
      expect(w.from, DateTime(2026, 8, 1)); // Saturday, August 1
      expect(w.to, DateTime(2026, 8, 2, 23, 59, 59));
    });
  });

  group('QuickFiltersNotifier', () {
    test('toggles are independent and radius is preserved', () {
      final n = QuickFiltersNotifier();
      expect(n.state.freeOnly, isFalse);
      expect(n.state.tonight, isFalse);
      expect(n.state.nearMe, isFalse);
      expect(n.state.radiusKm, 10);

      n.toggleFree();
      n.toggleTonight();
      n.setNearMe(true);
      n.setRadius(25);
      expect(n.state.freeOnly, isTrue);
      expect(n.state.tonight, isTrue);
      expect(n.state.nearMe, isTrue);
      expect(n.state.radiusKm, 25);

      // Toggling one off leaves the others alone.
      n.toggleFree();
      expect(n.state.freeOnly, isFalse);
      expect(n.state.tonight, isTrue);
      expect(n.state.nearMe, isTrue);
      expect(n.state.radiusKm, 25);
    });
  });

  group('Filters copyWith sentinel', () {
    test('setting a field to null actually clears it', () {
      final n = FiltersNotifier();
      n.setCity('lisbon');
      n.setCategory(EventCategory.music);
      expect(n.state.cityId, 'lisbon');

      n.setCity(null);
      expect(n.state.cityId, isNull); // null must clear, not "keep previous"
      expect(n.state.category, EventCategory.music); // untouched field kept
    });

    test('clearing the date range only clears the date range', () {
      final n = FiltersNotifier();
      n.setCity('porto');
      n.setRange(DateTime(2026, 7, 11), DateTime(2026, 7, 12));
      n.setRange(null, null);
      expect(n.state.from, isNull);
      expect(n.state.to, isNull);
      expect(n.state.cityId, 'porto');
    });

    test('clear() resets everything', () {
      final n = FiltersNotifier();
      n.setCity('porto');
      n.setSearch('jazz');
      n.clear();
      expect(n.state.cityId, isNull);
      expect(n.state.search, isEmpty);
    });
  });
}
