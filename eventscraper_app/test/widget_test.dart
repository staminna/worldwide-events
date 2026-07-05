import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:eventscraper_app/api/event_api.dart';
import 'package:eventscraper_app/main.dart';
import 'package:eventscraper_app/models/event.dart';
import 'package:eventscraper_app/state/providers.dart';

final _event = Event(
  id: 'e1',
  source: EventSource.luma,
  sourceId: 'e1',
  title: 'Jazz Night',
  description: 'Smooth jazz downtown.',
  category: EventCategory.music,
  startsAt: DateTime(2030, 6, 10, 20),
  endsAt: null,
  venue: const Venue(name: 'Blue Note', lat: 38.72, lon: -9.14),
  city: 'Lisbon',
  country: 'PT',
  url: 'https://example.com/e1',
  imageUrl: '', // keep empty so no network image is fetched in tests
  price: null,
  scrapedAt: DateTime(2030, 6, 1),
);

/// Canned API so widget tests stay off the network (real requests leave
/// pending timeout timers that fail the test harness).
class _FakeApi extends EventApi {
  @override
  Future<List<City>> fetchCities() async => const [];

  @override
  Future<List<SourceInfo>> fetchSources() async => const [];

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
    return EventList(
      events: [_event],
      total: 1,
      cached: true,
      age: '1s',
      limit: limit,
      offset: offset,
    );
  }

  @override
  Future<Event> fetchEvent(String id) async => _event;
}

void main() {
  testWidgets('app boots and shows the event feed', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(_FakeApi())],
        child: const EventScraperApp(),
      ),
    );
    await tester.pump(); // first frame
    await tester.pump(); // feed future resolves

    // Home shell navigation.
    expect(find.text('Feed'), findsOneWidget);
    expect(find.text('Map'), findsOneWidget);
    // The fetched event is rendered in the grid.
    expect(find.text('Jazz Night'), findsOneWidget);
    // Feed is presented in default date order.
    expect(find.textContaining('Sorted by date'), findsOneWidget);
  });
}
