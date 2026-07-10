import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:eventscraper_app/models/event.dart';
import 'package:eventscraper_app/state/saved_events.dart';

Event _event(String id, {DateTime? startsAt}) => Event(
  id: id,
  source: EventSource.luma,
  sourceId: id,
  title: 'Event $id',
  description: '',
  category: EventCategory.music,
  startsAt: startsAt ?? DateTime.utc(2030, 6, 10, 20),
  endsAt: null,
  venue: const Venue(name: 'Venue', lat: 38.72, lon: -9.14),
  city: 'Lisbon',
  country: 'PT',
  url: '',
  imageUrl: '',
  price: null,
  scrapedAt: DateTime.utc(2030, 6, 1),
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('SavedEventsNotifier', () {
    test('toggle saves newest first, second toggle removes', () async {
      SharedPreferences.setMockInitialValues({});
      final n = SavedEventsNotifier();
      await pumpEventQueue();

      await n.toggle(_event('a'));
      await n.toggle(_event('b'));
      expect(n.state.map((e) => e.id), ['b', 'a']); // newest first
      expect(n.isSaved('a'), isTrue);

      await n.toggle(_event('a')); // toggle off
      expect(n.state.map((e) => e.id), ['b']);
      expect(n.isSaved('a'), isFalse);
    });

    test('remove drops by id and is a no-op for unknown ids', () async {
      SharedPreferences.setMockInitialValues({});
      final n = SavedEventsNotifier();
      await pumpEventQueue();

      await n.toggle(_event('a'));
      await n.remove('does-not-exist');
      expect(n.state.length, 1);
      await n.remove('a');
      expect(n.state, isEmpty);
    });

    test('saved events persist and reload in a fresh notifier', () async {
      SharedPreferences.setMockInitialValues({});
      final first = SavedEventsNotifier();
      await pumpEventQueue();
      await first.toggle(_event('a'));

      // Same mock store, brand-new notifier — simulates an app restart.
      final second = SavedEventsNotifier();
      await pumpEventQueue();
      expect(second.state.map((e) => e.id), ['a']);
      expect(second.state.first.title, 'Event a');
    });

    test('a corrupt store starts clean instead of crashing', () async {
      SharedPreferences.setMockInitialValues({
        'saved_events_v1': 'this is {not] json',
      });
      final n = SavedEventsNotifier();
      await pumpEventQueue();
      expect(n.state, isEmpty);

      // And the store recovers on the next save.
      await n.toggle(_event('a'));
      final prefs = await SharedPreferences.getInstance();
      final decoded =
          jsonDecode(prefs.getString('saved_events_v1')!) as List<dynamic>;
      expect(decoded, hasLength(1));
    });

    test('a store with a valid list of the wrong shape starts clean', () async {
      SharedPreferences.setMockInitialValues({
        'saved_events_v1': '[{"nonsense": true}]',
      });
      final n = SavedEventsNotifier();
      await pumpEventQueue();
      expect(n.state, isEmpty); // missing required fields → caught, not thrown
    });
  });
}
