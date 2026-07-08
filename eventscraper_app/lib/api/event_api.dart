import 'package:dio/dio.dart';

import '../models/event.dart';

// Production backend by default; override for local development with
// --dart-define=API_BASE=http://localhost:8080
const String kApiBase = String.fromEnvironment('API_BASE',
    defaultValue: 'https://api.iamjorgenunes.com/eventscraper');

/// Wraps an upstream image URL with the backend's CORS-friendly proxy.
/// Empty input returns empty. Already-proxied URLs are returned untouched.
String proxiedImage(String url) {
  if (url.isEmpty) return '';
  if (url.startsWith('$kApiBase/img?')) return url;
  return '$kApiBase/img?u=${Uri.encodeQueryComponent(url)}';
}

/// Rewrites known CDN URLs to request a higher-resolution variant.
/// Used on detail screens where the small thumbnail picked up by the scraper
/// looks blurry when stretched to full width.
String hiResImage(String url, {int width = 1600}) {
  if (url.isEmpty) return url;
  final lower = url.toLowerCase();

  // Eventbrite: img.evbuc.com / cdn.evbuc.com use ?w=&h= sizing params,
  // capped by the upstream original. Bumping w/h gets the full-res render
  // (they ignore values larger than the source). Strip the rect crop too,
  // which forces a small aspect-ratio box.
  if (lower.contains('evbuc.com') || lower.contains('eventbrite')) {
    final uri = Uri.tryParse(url);
    if (uri == null) return url;
    final qp = Map<String, String>.from(uri.queryParameters);
    qp.remove('h');
    qp.remove('rect');
    qp['w'] = width.toString();
    qp['auto'] = qp['auto'] ?? 'format,compress';
    qp['q'] = '85';
    return uri.replace(queryParameters: qp).toString();
  }

  // Luma: images.lumacdn.com uses Cloudflare image-resizing in the path,
  // e.g. /cdn-cgi/image/format=auto,fit=cover,dpr=2,quality=75,width=400/...
  // Rewrite width=N in that segment.
  if (lower.contains('lumacdn.com') || lower.contains('cdn.lu.ma')) {
    final widthRe = RegExp(r'(?<=[,/])width=\d+');
    if (widthRe.hasMatch(url)) {
      return url.replaceFirst(widthRe, 'width=$width');
    }
  }

  // Songkick avatars: sk-static uses size prefixes (avatar/large_avatar/huge_avatar).
  if (lower.contains('sk-static')) {
    return url
        .replaceAll('/large_avatar', '/huge_avatar')
        .replaceAll('/medium_avatar', '/huge_avatar');
  }

  // viralagenda: promoter images under /events/ext/ are served either as the
  // full-size original (no suffix) or a downscaled `-r` copy (~640-750px wide).
  // When we hold the `-r` copy, drop the suffix to fetch the original, which is
  // frequently much larger (e.g. 1440x1800). If a given event's original isn't
  // public the request 404s and the caller falls back to the stored URL. The
  // hashed `/events/<hash>-large.jpg` form has no larger variant, so it's left
  // untouched.
  if (lower.contains('viralagenda.com')) {
    return url.replaceFirstMapped(
      RegExp(r'-r(\.(?:jpe?g|png))$', caseSensitive: false),
      (m) => m[1]!,
    );
  }

  return url;
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

  /// Resolves a coordinate to the nearest supported city (reverse geocoding
  /// against the backend's city catalog). With [minEvents], the backend
  /// walks cities outward and returns the first whose feed already holds
  /// that many located events, so an empty city doesn't win on distance.
  Future<NearestCity> reverseGeocode(
    double lat,
    double lon, {
    int? minEvents,
  }) async {
    final res = await _dio.get(
      '/geo/reverse',
      queryParameters: {'lat': lat, 'lon': lon, 'min_events': ?minEvents},
    );
    final body = res.data as Map<String, dynamic>;
    return NearestCity.fromJson(body['data'] as Map<String, dynamic>);
  }

  /// Fetches a street address for a venue coordinate (backend proxies and
  /// caches Nominatim). Passing [eventId] lets the backend persist the
  /// address into the stored event. Returns '' when nothing is known —
  /// callers treat the address as optional decoration.
  Future<String> fetchVenueAddress({
    required double lat,
    required double lon,
    String? eventId,
  }) async {
    try {
      final res = await _dio.get(
        '/geo/address',
        queryParameters: {
          'lat': lat,
          'lon': lon,
          if (eventId != null && eventId.isNotEmpty) 'event': eventId,
        },
      );
      final body = res.data as Map<String, dynamic>;
      final data = body['data'] as Map<String, dynamic>? ?? const {};
      return data['address'] as String? ?? '';
    } catch (_) {
      return '';
    }
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

  /// Creates a user-authored event (stored server-side under the "manual"
  /// source). [cityId] must be a catalog city so the event is reachable by
  /// the feed's city filter. Returns the stored event.
  Future<Event> createEvent({
    required String title,
    String description = '',
    required EventCategory category,
    required DateTime startsAt,
    DateTime? endsAt,
    required String cityId,
    String venueName = '',
    String address = '',
    double? lat,
    double? lon,
    String imageUrl = '',
  }) async {
    final res = await _dio.post(
      '/events',
      data: {
        'title': title,
        'description': description,
        'category': category.name,
        'startsAt': startsAt.toUtc().toIso8601String(),
        if (endsAt != null) 'endsAt': endsAt.toUtc().toIso8601String(),
        'cityId': cityId,
        'venueName': venueName,
        'address': address,
        'lat': ?lat,
        'lon': ?lon,
        'imageUrl': imageUrl,
      },
    );
    final body = res.data as Map<String, dynamic>;
    return Event.fromJson(body['data'] as Map<String, dynamic>);
  }

  /// Uploads a cover image (POST /upload, multipart field "file") and returns
  /// its absolute URL. The backend answers with a relative `/uploads/...`
  /// path so the URL works behind any reverse-proxy prefix — we absolutize it
  /// against this client's own base.
  Future<String> uploadImage(List<int> bytes, String filename) async {
    final res = await _dio.post(
      '/upload',
      data: FormData.fromMap({
        'file': MultipartFile.fromBytes(bytes, filename: filename),
      }),
    );
    final body = res.data as Map<String, dynamic>;
    final url = (body['data'] as Map<String, dynamic>)['url'] as String;
    if (url.startsWith('http')) return url;
    final base = _dio.options.baseUrl;
    return '${base.endsWith('/') ? base.substring(0, base.length - 1) : base}$url';
  }

  /// Forward-geocodes a free-text query to candidate places (backend proxies
  /// and rate-limits Nominatim). Returns an empty list on any failure —
  /// callers treat it as "no matches".
  Future<List<LocationResult>> searchLocation(String query) async {
    try {
      final res = await _dio.get(
        '/geo/search',
        queryParameters: {'q': query},
      );
      final body = res.data as Map<String, dynamic>;
      final list = (body['data'] as List? ?? const [])
          .cast<Map<String, dynamic>>();
      return list.map(LocationResult.fromJson).toList();
    } catch (_) {
      return [];
    }
  }
}
