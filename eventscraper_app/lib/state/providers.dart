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
      category: category == _sentinel ? this.category : category as EventCategory?,
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

final filtersProvider =
    StateNotifierProvider<FiltersNotifier, Filters>((ref) => FiltersNotifier());

final eventsProvider = FutureProvider.autoDispose<EventList>((ref) async {
  final f = ref.watch(filtersProvider);
  return ref.read(apiProvider).fetchEvents(
        cityId: f.cityId,
        category: f.category,
        source: f.source,
        from: f.from,
        to: f.to,
        q: f.search,
      );
});

final eventByIdProvider =
    FutureProvider.autoDispose.family<Event, String>((ref, id) async {
  return ref.read(apiProvider).fetchEvent(id);
});
