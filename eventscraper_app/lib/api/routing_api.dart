import 'package:dio/dio.dart';

/// A pedestrian route between two points from the public OSRM foot profile.
class WalkRoute {
  const WalkRoute({
    required this.distanceMeters,
    required this.durationSeconds,
    required this.geometry,
  });

  final double distanceMeters;
  final double durationSeconds;

  /// GeoJSON LineString geometry ([lon, lat] pairs), ready to feed straight
  /// into a map source.
  final Map<String, dynamic> geometry;

  String get walkLabel {
    final mins = (durationSeconds / 60).round();
    final dist = distanceMeters >= 1000
        ? '${(distanceMeters / 1000).toStringAsFixed(1)} km'
        : '${distanceMeters.round()} m';
    return '$mins min walk · $dist';
  }
}

/// A multi-stop pedestrian tour through an ordered list of points — the
/// "event crawl". Distances/durations are the whole tour; [legSeconds] holds
/// one entry per hop between consecutive stops.
class WalkTour {
  const WalkTour({
    required this.distanceMeters,
    required this.durationSeconds,
    required this.geometry,
    required this.legSeconds,
  });

  final double distanceMeters;
  final double durationSeconds;

  /// GeoJSON LineString through every stop, ready for a map source.
  final Map<String, dynamic> geometry;
  final List<double> legSeconds;

  String get totalLabel {
    final mins = (durationSeconds / 60).round();
    final dist = distanceMeters >= 1000
        ? '${(distanceMeters / 1000).toStringAsFixed(1)} km'
        : '${distanceMeters.round()} m';
    return '$mins min walking · $dist total';
  }
}

// FOSSGIS's community OSRM instance with the pedestrian profile — free, no
// API key, fair-use. https://routing.openstreetmap.de/
final _dio = Dio(
  BaseOptions(
    connectTimeout: const Duration(seconds: 8),
    receiveTimeout: const Duration(seconds: 8),
    // Demo-server etiquette: identify the app instead of a generic client.
    headers: {'User-Agent': 'eventscraper_app (com.jorgenunes.eventscraper)'},
  ),
);

/// Fetches a walking route. Best-effort: returns null on any failure — the
/// route is decoration, callers must never surface an error for it.
Future<WalkRoute?> fetchWalkingRoute({
  required double fromLat,
  required double fromLon,
  required double toLat,
  required double toLon,
}) async {
  try {
    final res = await _dio.get<Map<String, dynamic>>(
      'https://routing.openstreetmap.de/routed-foot/route/v1/foot/'
      '$fromLon,$fromLat;$toLon,$toLat',
      queryParameters: {'overview': 'full', 'geometries': 'geojson'},
    );
    final data = res.data;
    if (data == null || data['code'] != 'Ok') return null;
    final routes = data['routes'] as List?;
    if (routes == null || routes.isEmpty) return null;
    final route = routes.first as Map<String, dynamic>;
    return WalkRoute(
      distanceMeters: (route['distance'] as num).toDouble(),
      durationSeconds: (route['duration'] as num).toDouble(),
      geometry: (route['geometry'] as Map).cast<String, dynamic>(),
    );
  } catch (_) {
    return null;
  }
}

/// Fetches a single walking route visiting [stops] in order (the event crawl).
/// Best-effort: returns null on any failure.
Future<WalkTour?> fetchWalkingTour(
  List<({double lat, double lon})> stops,
) async {
  if (stops.length < 2) return null;
  try {
    final coords = stops.map((s) => '${s.lon},${s.lat}').join(';');
    final res = await _dio.get<Map<String, dynamic>>(
      'https://routing.openstreetmap.de/routed-foot/route/v1/foot/$coords',
      queryParameters: {'overview': 'full', 'geometries': 'geojson'},
    );
    final data = res.data;
    if (data == null || data['code'] != 'Ok') return null;
    final routes = data['routes'] as List?;
    if (routes == null || routes.isEmpty) return null;
    final route = routes.first as Map<String, dynamic>;
    final legs = [
      for (final l in (route['legs'] as List? ?? const []))
        ((l as Map)['duration'] as num).toDouble(),
    ];
    return WalkTour(
      distanceMeters: (route['distance'] as num).toDouble(),
      durationSeconds: (route['duration'] as num).toDouble(),
      geometry: (route['geometry'] as Map).cast<String, dynamic>(),
      legSeconds: legs,
    );
  } catch (_) {
    return null;
  }
}
