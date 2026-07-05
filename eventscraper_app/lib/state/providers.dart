import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/event_api.dart';
import '../models/event.dart';

final apiProvider = Provider<EventApi>((ref) => EventApi());

final citiesProvider = FutureProvider<List<City>>((ref) async {
  return ref.read(apiProvider).fetchCities();
});

final sourcesProvider = FutureProvider<List<SourceInfo>>((ref) async {
  return ref.read(apiProvider).fetchSources();
});

class Filters {
  final String? cityId;
  final EventCategory? category;
  final EventSource? source;
  final DateTime? from;
  final DateTime? to;
  final String search;

  const Filters({
    this.cityId,
    this.category,
    this.source,
    this.from,
    this.to,
    this.search = '',
  });

  Filters copyWith({
    Object? cityId = _sentinel,
    Object? category = _sentinel,
    Object? source = _sentinel,
    Object? from = _sentinel,
    Object? to = _sentinel,
    String? search,
  }) {
    return Filters(
      cityId: cityId == _sentinel ? this.cityId : cityId as String?,
      category: category == _sentinel
          ? this.category
          : category as EventCategory?,
      source: source == _sentinel ? this.source : source as EventSource?,
      from: from == _sentinel ? this.from : from as DateTime?,
      to: to == _sentinel ? this.to : to as DateTime?,
      search: search ?? this.search,
    );
  }

  static const Object _sentinel = Object();
}

class FiltersNotifier extends StateNotifier<Filters> {
  FiltersNotifier() : super(const Filters());

  void setCity(String? id) => state = state.copyWith(cityId: id);
  void setCategory(EventCategory? c) => state = state.copyWith(category: c);
  void setSource(EventSource? s) => state = state.copyWith(source: s);
  void setRange(DateTime? from, DateTime? to) =>
      state = state.copyWith(from: from, to: to);
  void setSearch(String q) => state = state.copyWith(search: q);
  void clear() => state = const Filters();
}

final filtersProvider = StateNotifierProvider<FiltersNotifier, Filters>(
  (ref) => FiltersNotifier(),
);

/// Accumulated, paged view over /events for the current filters.
class EventFeed {
  final List<Event> events;
  final int total;
  final bool loading; // first page / refresh in flight
  final bool loadingMore; // next page in flight
  final Object? error;

  const EventFeed({
    this.events = const [],
    this.total = 0,
    this.loading = true,
    this.loadingMore = false,
    this.error,
  });

  bool get hasMore => events.length < total;

  EventFeed copyWith({
    List<Event>? events,
    int? total,
    bool? loading,
    bool? loadingMore,
    Object? error = _noError,
  }) {
    return EventFeed(
      events: events ?? this.events,
      total: total ?? this.total,
      loading: loading ?? this.loading,
      loadingMore: loadingMore ?? this.loadingMore,
      error: identical(error, _noError) ? this.error : error,
    );
  }

  static const Object _noError = Object();
}

class EventFeedNotifier extends StateNotifier<EventFeed> {
  EventFeedNotifier(this._api, this._filters) : super(const EventFeed()) {
    refresh();
  }

  final EventApi _api;
  final Filters _filters;
  static const _pageSize = 50;

  Future<void> refresh() async {
    state = state.copyWith(loading: true, error: null);
    try {
      final page = await _fetch(0);
      if (!mounted) return;
      state = EventFeed(events: page.events, total: page.total, loading: false);
    } catch (e) {
      if (!mounted) return;
      state = state.copyWith(loading: false, error: e);
    }
  }

  Future<void> loadMore() async {
    if (state.loading || state.loadingMore || !state.hasMore) return;
    state = state.copyWith(loadingMore: true);
    try {
      final page = await _fetch(state.events.length);
      if (!mounted) return;
      // The backend keeps scraping between page loads, so guard against an
      // event sliding across a page boundary and appearing twice.
      final known = state.events.map((e) => e.id).toSet();
      final fresh = page.events.where((e) => !known.contains(e.id)).toList();
      state = state.copyWith(
        events: [...state.events, ...fresh],
        total: page.total,
        loadingMore: false,
      );
    } catch (_) {
      if (!mounted) return;
      state = state.copyWith(loadingMore: false);
    }
  }

  Future<EventList> _fetch(int offset) => _api.fetchEvents(
    cityId: _filters.cityId,
    category: _filters.category,
    source: _filters.source,
    from: _filters.from,
    to: _filters.to,
    q: _filters.search,
    limit: _pageSize,
    offset: offset,
  );
}

/// Rebuilt whenever filters change, which resets paging to the first page.
final eventFeedProvider =
    StateNotifierProvider.autoDispose<EventFeedNotifier, EventFeed>((ref) {
      final filters = ref.watch(filtersProvider);
      return EventFeedNotifier(ref.read(apiProvider), filters);
    });

final eventByIdProvider = FutureProvider.autoDispose.family<Event, String>((
  ref,
  id,
) async {
  return ref.read(apiProvider).fetchEvent(id);
});
