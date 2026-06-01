import 'package:dio/dio.dart';

import '../models/event.dart';

const String kApiBase = String.fromEnvironment('API_BASE',
    defaultValue: 'http://localhost:8080');

/// Wraps an upstream image URL with the backend's CORS-friendly proxy.
/// Empty input returns empty. Already-proxied URLs are returned untouched.
String proxiedImage(String url) {
  if (url.isEmpty) return '';
  if (url.startsWith('$kApiBase/img?')) return url;
  return '$kApiBase/img?u=${Uri.encodeQueryComponent(url)}';
}

class EventApi {
  EventApi({String? baseUrl})
      : _dio = Dio(BaseOptions(
          baseUrl: baseUrl ?? kApiBase,
          connectTimeout: const Duration(seconds: 8),
          receiveTimeout: const Duration(seconds: 30),
          responseType: ResponseType.json,
        ));

  final Dio _dio;

  Future<List<City>> fetchCities() async {
    final res = await _dio.get('/cities');
    final data = res.data as Map<String, dynamic>;
    final list = (data['data'] as List).cast<Map<String, dynamic>>();
    return list.map(City.fromJson).toList();
  }

  Future<List<SourceInfo>> fetchSources() async {
    final res = await _dio.get('/sources');
    final data = res.data as Map<String, dynamic>;
    final list = (data['data'] as List).cast<Map<String, dynamic>>();
    return list.map(SourceInfo.fromJson).toList();
  }

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
    String fmt(DateTime d) =>
        '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';
    final params = <String, dynamic>{
      if (cityId != null && cityId.isNotEmpty) 'city': cityId,
      if (category != null && category != EventCategory.unknown)
        'category': category.name,
      if (source != null && source != EventSource.unknown) 'source': source.name,
      if (from != null) 'from': fmt(from),
      if (to != null) 'to': fmt(to),
      if (q != null && q.isNotEmpty) 'q': q,
      'limit': limit,
      'offset': offset,
    };
    final res = await _dio.get('/events', queryParameters: params);
    final body = res.data as Map<String, dynamic>;
    final events = (body['data'] as List)
        .cast<Map<String, dynamic>>()
        .map(Event.fromJson)
        .toList();
    final meta = body['meta'] as Map<String, dynamic>? ?? const {};
    return EventList(
      events: events,
      total: meta['total'] as int? ?? events.length,
      cached: meta['cached'] as bool? ?? false,
      age: meta['age'] as String? ?? '',
      limit: meta['limit'] as int? ?? limit,
      offset: meta['offset'] as int? ?? offset,
    );
  }

  Future<Event> fetchEvent(String id) async {
    final res = await _dio.get('/events/$id');
    final body = res.data as Map<String, dynamic>;
    return Event.fromJson(body['data'] as Map<String, dynamic>);
  }
}
