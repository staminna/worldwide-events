import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:eventscraper_app/api/event_api.dart';
import 'package:eventscraper_app/main.dart';
import 'package:eventscraper_app/models/event.dart';
import 'package:eventscraper_app/state/providers.dart';

/// Regression tests for the startup feed gate: the feed must fetch exactly
/// once on boot, with the city already resolved — never the old flicker of
/// a throwaway global fetch replaced by a city fetch.
class _CountingApi extends EventApi {
  final fetches = <String?>[]; // cityId of each /events call, in order

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
    fetches.add(cityId);
    return EventList(
      events: const [],
      total: 0,
      cached: true,
      age: '1s',
      limit: limit,
      offset: offset,
    );
  }
}

Future<void> _boot(WidgetTester tester, _CountingApi api) async {
  // Fail geolocator fast — unmocked channel futures never complete, which
  // is not what a device does and would hold the gate open forever.
  tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
    const MethodChannel('flutter.baseflow.com/geolocator'),
    (_) async => throw MissingPluginException(),
  );
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: const EventScraperApp(),
    ),
  );
  await tester.pump(); // gate opens; feed starts fetching
  await tester.pump(); // feed future resolves
  await tester.pump(); // settle any trailing rebuild
}

void main() {
  testWidgets('fresh install: locate fails → exactly one global fetch', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final api = _CountingApi();
    await _boot(tester, api);
    expect(api.fetches, [null]);
  });

  testWidgets('saved city: exactly one fetch, already city-scoped', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({lastCityPrefKey: 'porto'});
    final api = _CountingApi();
    await _boot(tester, api);
    expect(api.fetches, ['porto']);
  });
}
