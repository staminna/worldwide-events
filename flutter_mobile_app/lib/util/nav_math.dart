import 'dart:math' as math;

import 'geo.dart';

/// Initial great-circle bearing from point 1 to point 2, degrees 0..360.
double bearingDegrees(double lat1, double lon1, double lat2, double lon2) {
  final phi1 = _rad(lat1);
  final phi2 = _rad(lat2);
  final dLon = _rad(lon2 - lon1);
  final y = math.sin(dLon) * math.cos(phi2);
  final x =
      math.cos(phi1) * math.sin(phi2) -
      math.sin(phi1) * math.cos(phi2) * math.cos(dLon);
  return (math.atan2(y, x) * 180 / math.pi + 360) % 360;
}

/// Shortest signed arc from [from] to [to] in degrees, in -180..180.
double angularDeltaDegrees(double from, double to) =>
    ((to - from + 540) % 360) - 180;

/// Circular low-pass filter: moves [current] toward [target] by [alpha]
/// (0..1) along the shortest arc, so 359° → 1° travels 2°, not -358°.
/// Result normalized to 0..360.
double smoothHeading(double current, double target, double alpha) =>
    (current + alpha * angularDeltaDegrees(current, target) + 360) % 360;

/// Minimum distance in meters from a point to a polyline of GeoJSON
/// `[lon, lat]` pairs. Uses an equirectangular point-to-segment
/// approximation — plenty accurate at walking-route scale.
double distanceToPolylineMeters(
  double lat,
  double lon,
  List<List<double>> lonLatCoords,
) {
  if (lonLatCoords.isEmpty) return double.infinity;
  const earthRadius = 6371000.0;
  final cosLat = math.cos(_rad(lat));
  // Project everything to local meters around the query point.
  double px(List<double> c) => _rad(c[0] - lon) * cosLat * earthRadius;
  double py(List<double> c) => _rad(c[1] - lat) * earthRadius;

  var best = double.infinity;
  for (var i = 0; i < lonLatCoords.length - 1; i++) {
    final ax = px(lonLatCoords[i]), ay = py(lonLatCoords[i]);
    final bx = px(lonLatCoords[i + 1]), by = py(lonLatCoords[i + 1]);
    final dx = bx - ax, dy = by - ay;
    final lenSq = dx * dx + dy * dy;
    // The query point is the local origin, so project (0,0) onto AB.
    final t = lenSq == 0
        ? 0.0
        : ((-ax * dx - ay * dy) / lenSq).clamp(0.0, 1.0);
    final cx = ax + t * dx, cy = ay + t * dy;
    best = math.min(best, math.sqrt(cx * cx + cy * cy));
  }
  if (lonLatCoords.length == 1) {
    final ax = px(lonLatCoords[0]), ay = py(lonLatCoords[0]);
    best = math.sqrt(ax * ax + ay * ay);
  }
  return best;
}

/// Advances the current step index as maneuver points are passed: while the
/// fix is within [passRadiusMeters] of the current maneuver, move on to the
/// next one. GPS noise can make a walker "miss" the radius at a corner — the
/// off-route re-route is the recovery path for that, which keeps this simple.
int advanceStepIndex({
  required List<({double lat, double lon})> maneuvers,
  required int current,
  required double lat,
  required double lon,
  double passRadiusMeters = 18,
}) {
  var i = current;
  while (i < maneuvers.length - 1 &&
      haversineMeters(lat, lon, maneuvers[i].lat, maneuvers[i].lon) <
          passRadiusMeters) {
    i++;
  }
  return i;
}

double _rad(double deg) => deg * math.pi / 180;
