import 'dart:math' as math;
import 'dart:ui';

/// CARTO's free "Dark Matter" vector style — the same dark basemap kepler.gl
/// ships with. Token-free; swap for an OpenFreeMap style if usage outgrows
/// CARTO's fair-use terms.
const mapStyleUrl =
    'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json';

/// Straight-line distance between two coordinates in meters.
double haversineMeters(double lat1, double lon1, double lat2, double lon2) {
  const earthRadius = 6371000.0;
  final dLat = _rad(lat2 - lat1);
  final dLon = _rad(lon2 - lon1);
  final a =
      math.sin(dLat / 2) * math.sin(dLat / 2) +
      math.cos(_rad(lat1)) *
          math.cos(_rad(lat2)) *
          math.sin(dLon / 2) *
          math.sin(dLon / 2);
  return 2 * earthRadius * math.atan2(math.sqrt(a), math.sqrt(1 - a));
}

double _rad(double deg) => deg * math.pi / 180;

/// `#rrggbb` form for MapLibre style expressions (which don't take alpha in
/// this notation — use `rgba(...)` strings where transparency is needed).
String hexColor(Color color) =>
    '#${(color.toARGB32() & 0xFFFFFF).toRadixString(16).padLeft(6, '0')}';
