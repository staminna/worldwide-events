import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:eventscraper_app/api/event_api.dart';
import 'package:eventscraper_app/models/event.dart';
import 'package:eventscraper_app/state/follows.dart';

Event _event(String id, {required DateTime scrapedAt}) => Event(
  id: id,
  source: EventSource.viralagenda,
  sourceId: id,
  title: 'Event $id',
  description: '',
  category: EventCategory.music,
  startsAt: DateTime.utc(2030, 6, 10, 20),
  endsAt: null,
  venue: const Venue(),
  city: 'Leiria',
  country: 'PT',
  url: '',
  imageUrl: '',
  price: null,
  scrapedAt: scrapedAt,
);

/// Canned API: returns [canned] for music-category queries, empty otherwise.
class _FakeApi extends EventApi {
  _FakeApi(this.canned);
  final List<Event> canned;
  int calls = 0;

  @override
  Future<EventList> fetchEvents({
    String? cityId,
    EventCategory? category,
    EventSource? source,
    DateTime? from,
    DateTime? to,
    String? q,
    int limit = 50,
    int offset = 0,
  }) async {
    calls++;
    final events = category == EventCategory.music ? canned : <Event>[];
    return EventList(
      events: events,
      total: events.length,
      cached: false,
      age: '',
      limit: limit,
      offset: offset,
    );
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('FollowsNotifier', () {
    test('toggle follows and unfollows, persisted across notifiers', () async {
      SharedPreferences.setMockInitialValues({});
      final n = FollowsNotifier();
      await pumpEventQueue();

      await n.toggle(FollowType.category, 'music', 'Music');
      expect(n.isFollowing(FollowType.category, 'music'), isTrue);
      // Same value under a different type is a distinct follow.
      expect(n.isFollowing(FollowType.source, 'music'), isFalse);

      final reloaded = FollowsNotifier();
      await pumpEventQueue();
      expect(reloaded.isFollowing(FollowType.category, 'music'), isTrue);

      await n.toggle(FollowType.category, 'music', 'Music');
      expect(n.isFollowing(FollowType.category, 'music'), isFalse);
    });
  });

  group('checkFollowsAndNotify', () {
    final followTime = DateTime.utc(2030, 1, 1);
    final follow = Follow(
      type: FollowType.category,
      value: 'music',
      label: 'Music',
      createdAt: followTime,
    );

    test('does nothing with no follows (no API calls)', () async {
      SharedPreferences.setMockInitialValues({});
      final api = _FakeApi([]);
      await checkFollowsAndNotify(api, const []);
      expect(api.calls, 0);
    });

    test('only events scraped AFTER the follow are announced', () async {
      SharedPreferences.setMockInitialValues({});
      final api = _FakeApi([
        _event('old', scrapedAt: DateTime.utc(2029, 12, 1)), // pre-follow
        _event('new', scrapedAt: DateTime.utc(2030, 2, 1)), // post-follow
      ]);
      await checkFollowsAndNotify(api, [follow]);

      final prefs = await SharedPreferences.getInstance();
      final notified = prefs.getStringList('notified_event_ids_v1') ?? [];
      expect(notified, ['new']); // back-catalog must not be announced
    });

    test('an event is never announced twice', () async {
      SharedPreferences.setMockInitialValues({
        'notified_event_ids_v1': ['new'],
      });
      final api = _FakeApi([
        _event('new', scrapedAt: DateTime.utc(2030, 2, 1)),
      ]);
      await checkFollowsAndNotify(api, [follow]);

      final prefs = await SharedPreferences.getInstance();
      // Nothing fresh → the stored list is not rewritten and stays length 1.
      expect(prefs.getStringList('notified_event_ids_v1'), ['new']);
    });

    test('an API error on one follow does not break the check', () async {
      SharedPreferences.setMockInitialValues({});
      final api = _ThrowingApi();
      // Must complete without throwing.
      await checkFollowsAndNotify(api, [follow]);
      expect(true, isTrue);
    });
  });
}

class _ThrowingApi extends EventApi {
  @override
  Future<EventList> fetchEvents({
    String? cityId,
    EventCategory? category,
    EventSource? source,
    DateTime? from,
    DateTime? to,
    String? q,
    int limit = 50,
    int offset = 0,
  }) async {
    throw Exception('network down');
  }
}
