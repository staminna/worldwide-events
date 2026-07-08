import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:eventscraper_app/api/event_api.dart';
import 'package:eventscraper_app/main.dart';
import 'package:eventscraper_app/models/event.dart';
import 'package:eventscraper_app/screens/agenda_screen.dart';
import 'package:eventscraper_app/state/providers.dart';
import 'package:eventscraper_app/widgets/event_card.dart';

/// A paid event far in the future — never matches Tonight, never Free.
final _paidFuture = Event(
  id: 'paid',
  source: EventSource.luma,
  sourceId: 'paid',
  title: 'Paid Future Gig',
  description: '',
  category: EventCategory.music,
  startsAt: DateTime(2030, 6, 10, 20),
  endsAt: null,
  venue: const Venue(name: 'Blue Note', lat: 38.72, lon: -9.14),
  city: 'Lisbon',
  country: 'PT',
  url: 'https://example.com/paid',
  imageUrl: '',
  price: const Price(min: 10, max: 20, currency: 'EUR', free: false),
  scrapedAt: DateTime(2030, 6, 1),
);

/// A free event at the very end of today — always inside the Tonight window
/// (now-2h .. 23:59:59) no matter when the test runs.
Event get _freeTonight {
  final now = DateTime.now();
  return Event(
    id: 'free',
    source: EventSource.viralagenda,
    sourceId: 'free',
    title: 'Free Tonight Show',
    description: '',
    category: EventCategory.arts,
    startsAt: DateTime(now.year, now.month, now.day, 23, 59, 58),
    endsAt: null,
    venue: const Venue(name: 'Praça', lat: 39.74, lon: -8.81),
    city: 'Leiria',
    country: 'PT',
    url: 'https://example.com/free',
    imageUrl: '',
    price: const Price(free: true),
    scrapedAt: DateTime(2026, 1, 1),
  );
}

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
    final events = [_paidFuture, _freeTonight];
    return EventList(
      events: events,
      total: events.length,
      cached: true,
      age: '1s',
      limit: limit,
      offset: offset,
    );
  }

  @override
  Future<Event> fetchEvent(String id) async =>
      id == 'paid' ? _paidFuture : _freeTonight;
}

Future<void> _boot(WidgetTester tester) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(_FakeApi())],
      child: const EventScraperApp(),
    ),
  );
  await tester.pump(); // first frame
  await tester.pump(); // feed future resolves
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('quick chips: Free narrows the grid client-side', (tester) async {
    await _boot(tester);
    expect(find.text('Paid Future Gig'), findsOneWidget);
    expect(find.text('Free Tonight Show'), findsOneWidget);
    expect(find.textContaining('2 events'), findsOneWidget);

    await tester.tap(find.widgetWithText(FilterChip, 'Free'));
    await tester.pump();
    expect(find.text('Paid Future Gig'), findsNothing);
    expect(find.text('Free Tonight Show'), findsOneWidget);
    expect(find.textContaining('1 of 2'), findsOneWidget);

    // Toggle back off restores everything.
    await tester.tap(find.widgetWithText(FilterChip, 'Free'));
    await tester.pump();
    expect(find.text('Paid Future Gig'), findsOneWidget);
  });

  testWidgets('quick chips: Tonight keeps only today\'s events', (tester) async {
    await _boot(tester);
    await tester.tap(find.widgetWithText(FilterChip, 'Tonight'));
    await tester.pump();
    expect(find.text('Free Tonight Show'), findsOneWidget);
    expect(find.text('Paid Future Gig'), findsNothing);
  });

  testWidgets('quick chips: Free + Tonight compose; both off restores',
      (tester) async {
    await _boot(tester);
    await tester.tap(find.widgetWithText(FilterChip, 'Free'));
    await tester.tap(find.widgetWithText(FilterChip, 'Tonight'));
    await tester.pump();
    expect(find.text('Free Tonight Show'), findsOneWidget);
    expect(find.text('Paid Future Gig'), findsNothing);

    await tester.tap(find.widgetWithText(FilterChip, 'Free'));
    await tester.tap(find.widgetWithText(FilterChip, 'Tonight'));
    await tester.pump();
    expect(find.text('Paid Future Gig'), findsOneWidget);
  });

  testWidgets('save → agenda → remove full flow', (tester) async {
    await _boot(tester);

    // Save the first card via its image-overlay bookmark (NOT the appbar one).
    await tester.tap(
      find.descendant(
        of: find.widgetWithText(EventCard, 'Paid Future Gig'),
        matching: find.byIcon(Icons.bookmark_border),
      ),
    );
    await tester.pump();
    expect(find.text('Saved to your agenda'), findsOneWidget);
    // Appbar badge shows the count.
    expect(find.text('1'), findsWidgets);
    await tester.pump(const Duration(seconds: 3)); // let the snackbar expire

    // Open the agenda.
    await tester.tap(find.byTooltip('My agenda'));
    await tester.pumpAndSettle();
    expect(find.byType(AgendaScreen), findsOneWidget);
    expect(find.text('My agenda · 1'), findsOneWidget);
    expect(find.text('Paid Future Gig'), findsOneWidget);
    // Only one saved venue → the crawl planner must be disabled.
    expect(
      find.byTooltip('Save 2+ events with a location to plan a crawl'),
      findsOneWidget,
    );

    // Remove it → empty state.
    await tester.tap(find.byTooltip('Remove from agenda'));
    await tester.pump();
    expect(find.text('No saved events yet'), findsOneWidget);

    // Back home: the badge is gone and the card bookmark is hollow again.
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(find.byType(AgendaScreen), findsNothing);
    expect(
      find.descendant(
        of: find.widgetWithText(EventCard, 'Paid Future Gig'),
        matching: find.byIcon(Icons.bookmark_border),
      ),
      findsOneWidget,
    );
  });

  testWidgets('saving two located events enables the crawl planner',
      (tester) async {
    await _boot(tester);
    for (final title in ['Paid Future Gig', 'Free Tonight Show']) {
      await tester.tap(
        find.descendant(
          of: find.widgetWithText(EventCard, title),
          matching: find.byIcon(Icons.bookmark_border),
        ),
      );
      await tester.pump();
    }
    await tester.pump(const Duration(seconds: 3)); // snackbars

    await tester.tap(find.byTooltip('My agenda'));
    await tester.pumpAndSettle();
    expect(find.text('My agenda · 2'), findsOneWidget);
    // Two located stops → the crawl action is live.
    expect(find.byTooltip('Plan a walking crawl'), findsOneWidget);

    // Leave the app at '/' for the next test.
    await tester.pageBack();
    await tester.pumpAndSettle();
  });

  testWidgets('NL search applies parsed filters and reports them',
      (tester) async {
    await _boot(tester);

    await tester.enterText(find.byType(TextField), 'free music tonight');
    await tester.testTextInput.receiveAction(TextInputAction.search);
    await tester.pump();

    // The parse is confirmed to the user...
    expect(find.text('Applied: Music • Free • tonight'), findsOneWidget);

    // Applying the category rebuilds the feed notifier (autoDispose), which
    // briefly shows the loading state — pump until the refetch resolves and
    // the chip bar is back in the tree.
    await tester.pump();
    await tester.pump();

    // ...the chips reflect the parsed state...
    expect(
      tester.widget<FilterChip>(find.widgetWithText(FilterChip, 'Free')).selected,
      isTrue,
    );
    expect(
      tester
          .widget<FilterChip>(find.widgetWithText(FilterChip, 'Tonight'))
          .selected,
      isTrue,
    );
    // ...and the recognised words were consumed out of the text box.
    expect(tester.widget<TextField>(find.byType(TextField)).controller!.text,
        isEmpty);

    // Feed refetches with the category filter; client chips then narrow it.
    await tester.pump();
    await tester.pump();
    expect(find.text('Free Tonight Show'), findsOneWidget);
    expect(find.text('Paid Future Gig'), findsNothing);

    await tester.pump(const Duration(seconds: 4)); // expire the snackbar
  });

  testWidgets('plain-text search does not touch the chips', (tester) async {
    await _boot(tester);
    await tester.enterText(find.byType(TextField), 'springsteen');
    await tester.testTextInput.receiveAction(TextInputAction.search);
    // setSearch rebuilds the feed notifier — pump until the refetch resolves
    // so the chip bar is back in the tree before asserting on it.
    await tester.pump();
    await tester.pump();
    await tester.pump();

    expect(find.textContaining('Applied:'), findsNothing);
    expect(
      tester.widget<FilterChip>(find.widgetWithText(FilterChip, 'Free')).selected,
      isFalse,
    );
    // The query stays in the box as a plain full-text search.
    expect(tester.widget<TextField>(find.byType(TextField)).controller!.text,
        'springsteen');
    await tester.pump(const Duration(seconds: 1)); // flush search debounce
  });
}
