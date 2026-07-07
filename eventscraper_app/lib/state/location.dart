import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';

import '../api/event_api.dart';
import '../models/event.dart';
import 'providers.dart';

/// Raised when the device position can't be obtained; [message] is written
/// for direct display in a snackbar.
class LocationException implements Exception {
  const LocationException(this.message);
  final String message;

  @override
  String toString() => message;
}

class LocationState {
  final double? lat;
  final double? lon;
  final NearestCity? nearest;
  final bool locating;

  const LocationState({this.lat, this.lon, this.nearest, this.locating = false});

  bool get hasFix => lat != null && lon != null;
}

class LocationNotifier extends StateNotifier<LocationState> {
  LocationNotifier(this._api) : super(const LocationState());

  final EventApi _api;

  /// Gets the device position and reverse-geocodes it to the nearest
  /// supported city via the backend ([minEvents] asks for the nearest city
  /// that already has that many located events). Keeps the fix in state
  /// (for the map's position dot) and returns the match. Throws
  /// [LocationException] with a user-facing message when the position is
  /// unavailable.
  Future<NearestCity> locate({int? minEvents}) async {
    final pos = await _track(_devicePosition);
    final nearest = await _track(
      () => _api.reverseGeocode(
        pos.latitude,
        pos.longitude,
        minEvents: minEvents,
      ),
    );
    state = LocationState(
      lat: pos.latitude,
      lon: pos.longitude,
      nearest: nearest,
    );
    return nearest;
  }

  /// Position-only refresh — used by the map to center on the user without
  /// touching the backend or the city filter.
  Future<void> refreshFix() async {
    final pos = await _track(_devicePosition);
    state = LocationState(
      lat: pos.latitude,
      lon: pos.longitude,
      nearest: state.nearest,
    );
  }

  /// Runs [step] with the `locating` flag raised, lowering it on failure so
  /// the UI spinner never sticks.
  Future<T> _track<T>(Future<T> Function() step) async {
    state = LocationState(
      lat: state.lat,
      lon: state.lon,
      nearest: state.nearest,
      locating: true,
    );
    try {
      return await step();
    } catch (_) {
      state = LocationState(
        lat: state.lat,
        lon: state.lon,
        nearest: state.nearest,
      );
      rethrow;
    }
  }

  Future<Position> _devicePosition() async {
    if (!await Geolocator.isLocationServiceEnabled()) {
      throw const LocationException(
        'Location services are turned off on this device.',
      );
    }
    var permission = await Geolocator.checkPermission();
    if (permission == LocationPermission.denied) {
      permission = await Geolocator.requestPermission();
    }
    if (permission == LocationPermission.denied) {
      throw const LocationException('Location permission was denied.');
    }
    if (permission == LocationPermission.deniedForever) {
      throw const LocationException(
        'Location permission is blocked — enable it in system settings.',
      );
    }
    try {
      // City-level accuracy is all the feed needs; low keeps the fix fast
      // and battery-cheap.
      return await Geolocator.getCurrentPosition(
        locationSettings: const LocationSettings(
          accuracy: LocationAccuracy.low,
          timeLimit: Duration(seconds: 15),
        ),
      );
    } on LocationException {
      rethrow;
    } catch (_) {
      // Timeout or platform hiccup: an old fix is still good enough to pick
      // a city, so fall back to the last known position when there is one
      // (unavailable on web, where the lookup itself throws).
      Position? last;
      try {
        last = await Geolocator.getLastKnownPosition();
      } catch (_) {}
      if (last != null) return last;
      throw const LocationException(
        'Could not get a location fix. Try again.',
      );
    }
  }
}

final locationProvider = StateNotifierProvider<LocationNotifier, LocationState>(
  (ref) => LocationNotifier(ref.read(apiProvider)),
);
