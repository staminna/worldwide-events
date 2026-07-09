import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter_compass/flutter_compass.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';
import 'package:intl/intl.dart';
import 'package:maplibre_gl/maplibre_gl.dart';

import '../api/routing_api.dart';
import '../models/event.dart';
import '../state/location.dart';
import '../util/geo.dart';
import '../util/map_pin.dart';
import '../util/nav_math.dart';
import '../widgets/category_style.dart';
import '../widgets/directions_buttons.dart';

/// Full-screen interactive map for a single event's venue, opened by tapping
/// the preview map on the detail screen. The venue is drawn as a teardrop pin
/// (a generated symbol so it tracks the map while panning). The location
/// button draws a walking polyline from the user to the pin with route info.
class VenueMapScreen extends ConsumerStatefulWidget {
  const VenueMapScreen({
    super.key,
    required this.event,
    required this.center,
    this.approxLabel,
  });

  final Event event;
  final LatLng center;

  /// Non-null when the point is a city centroid rather than a real venue —
  /// shown as an "Approximate" banner and hides turn-by-turn directions.
  final String? approxLabel;

  @override
  ConsumerState<VenueMapScreen> createState() => _VenueMapScreenState();
}

class _VenueMapScreenState extends ConsumerState<VenueMapScreen> {
  MapLibreMapController? _controller;
  bool _styleReady = false;

  // Route-from-me state: label shown in the bottom card once a route exists.
  bool _routing = false;
  String? _routeLabel;
  WalkRoute? _route;

  // Navigation mode: compass-up camera following a live position stream,
  // with a turn-by-turn banner driven by the route's OSRM steps.
  bool _navigating = false;
  int _stepIndex = 0;
  double? _heading; // smoothed camera bearing
  bool _compassAlive = false; // saw a non-null compass reading recently
  LatLng? _navFix; // latest streamed position
  StreamSubscription<CompassEvent>? _compassSub;
  StreamSubscription<Position>? _posSub;
  DateTime _lastCamAt = DateTime.fromMillisecondsSinceEpoch(0);
  double _lastCamHeading = 0;
  int _offRouteFixes = 0;
  DateTime _lastRerouteAt = DateTime.fromMillisecondsSinceEpoch(0);
  bool _rerouting = false;

  // Build the map only after the push transition settles. Creating a MapLibre
  // platform view mid-transition makes it latch a wrong (partial) surface
  // size and render as a thin strip — this defers it to full screen size.
  bool _showMap = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      await Future<void>.delayed(const Duration(milliseconds: 350));
      if (mounted) setState(() => _showMap = true);
    });
  }

  @override
  void dispose() {
    _compassSub?.cancel();
    _posSub?.cancel();
    super.dispose();
  }

  static const _emptyFc = {'type': 'FeatureCollection', 'features': []};

  Future<void> _onStyleLoaded() async {
    final c = _controller;
    if (c == null || !mounted) return;
    // Captured before the awaits below — context must not cross async gaps.
    final pinColor = categoryColor(
      Theme.of(context).colorScheme,
      widget.event.category,
    );
    final dpr = MediaQuery.devicePixelRatioOf(context);

    // Walk route under everything so the pin and me-dot draw on top of it.
    await c.addSource(
      'walk-route',
      const GeojsonSourceProperties(data: _emptyFc),
    );
    await c.addLineLayer(
      'walk-route',
      'walk-route-line',
      const LineLayerProperties(
        lineColor: '#4dd0e1',
        lineWidth: 4.5,
        lineOpacity: 0.9,
        lineJoin: 'round',
        lineCap: 'round',
      ),
      enableInteraction: false,
    );

    final pin = await renderMapPin(pinColor, dpr: dpr);
    await c.addImage('venue-pin', pin);
    await c.addSource(
      'venue',
      GeojsonSourceProperties(
        data: {
          'type': 'FeatureCollection',
          'features': [
            {
              'type': 'Feature',
              'properties': const <String, dynamic>{},
              'geometry': {
                'type': 'Point',
                'coordinates': [
                  widget.center.longitude,
                  widget.center.latitude,
                ],
              },
            },
          ],
        },
      ),
    );
    await c.addSymbolLayer(
      'venue',
      'venue-pin-layer',
      const SymbolLayerProperties(
        iconImage: 'venue-pin',
        iconSize: 1,
        iconAnchor: 'bottom',
        iconAllowOverlap: true,
        iconIgnorePlacement: true,
      ),
    );

    // The classic blue "you are here" dot, hidden until a fix is drawn.
    await c.addSource('me', const GeojsonSourceProperties(data: _emptyFc));
    await c.addCircleLayer(
      'me',
      'me-halo',
      const CircleLayerProperties(circleRadius: 10, circleColor: '#ffffff'),
      enableInteraction: false,
    );
    await c.addCircleLayer(
      'me',
      'me-dot',
      const CircleLayerProperties(circleRadius: 6.5, circleColor: '#1A73E8'),
      enableInteraction: false,
    );

    // Nav-mode puck: a heading arrow that replaces the dot while navigating.
    // Rotation is data-driven from the me-feature's 'bearing' property and
    // map-aligned, so it stays true regardless of the camera's own bearing.
    await c.addImage('nav-arrow', await renderNavArrow(dpr: dpr));
    await c.addSymbolLayer(
      'me',
      'me-arrow',
      const SymbolLayerProperties(
        iconImage: 'nav-arrow',
        iconSize: 1,
        iconAllowOverlap: true,
        iconIgnorePlacement: true,
        iconRotationAlignment: 'map',
        iconRotate: ['get', 'bearing'],
      ),
      enableInteraction: false,
    );
    await c.setLayerVisibility('me-arrow', false);

    if (mounted) _styleReady = true;
  }

  void _setMeSource(LatLng p, {double bearing = 0}) {
    _controller?.setGeoJsonSource('me', {
      'type': 'FeatureCollection',
      'features': [
        {
          'type': 'Feature',
          'properties': {'bearing': bearing},
          'geometry': {
            'type': 'Point',
            'coordinates': [p.longitude, p.latitude],
          },
        },
      ],
    });
  }

  /// Location-button tap: get a fix, draw the walking polyline from the user
  /// to the pin, surface the walk time/distance, and frame both points.
  /// Falls back to a straight line when OSRM has no route.
  Future<void> _routeFromMe() async {
    final c = _controller;
    if (c == null || !_styleReady || _routing) return;
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _routing = true);
    try {
      if (!ref.read(locationProvider).hasFix) {
        await ref.read(locationProvider.notifier).refreshFix();
      }
      final loc = ref.read(locationProvider);
      if (!loc.hasFix) {
        throw const LocationException('Could not get a location fix.');
      }
      final me = LatLng(loc.lat!, loc.lon!);
      _setMeSource(me);

      final route = await fetchWalkingRoute(
        fromLat: me.latitude,
        fromLon: me.longitude,
        toLat: widget.center.latitude,
        toLon: widget.center.longitude,
      );
      final geometry =
          route?.geometry ??
          {
            'type': 'LineString',
            'coordinates': [
              [me.longitude, me.latitude],
              [widget.center.longitude, widget.center.latitude],
            ],
          };
      c.setGeoJsonSource('walk-route', {
        'type': 'FeatureCollection',
        'features': [
          {'type': 'Feature', 'properties': const {}, 'geometry': geometry},
        ],
      });

      final label =
          route?.walkLabel ??
          '${(haversineMeters(me.latitude, me.longitude, widget.center.latitude, widget.center.longitude) / 1000).toStringAsFixed(1)} km straight line — walking route unavailable';
      if (!mounted) return;
      setState(() {
        _route = route;
        _routeLabel = label;
        _stepIndex = 0;
      });
      await _fitBoth(me);
    } catch (e) {
      messenger.showSnackBar(
        SnackBar(
          content: Text(
            e is LocationException ? e.message : 'Location lookup failed: $e',
          ),
        ),
      );
    } finally {
      if (mounted) setState(() => _routing = false);
    }
  }

  Future<void> _fitBoth(LatLng me) async {
    final minLat = math.min(me.latitude, widget.center.latitude);
    final maxLat = math.max(me.latitude, widget.center.latitude);
    final minLon = math.min(me.longitude, widget.center.longitude);
    final maxLon = math.max(me.longitude, widget.center.longitude);
    // Practically the same point → just centre instead of a degenerate fit.
    if (maxLat - minLat < 0.0005 && maxLon - minLon < 0.0005) {
      await _controller?.animateCamera(
        CameraUpdate.newLatLngZoom(widget.center, 16),
      );
      return;
    }
    await _controller?.animateCamera(
      CameraUpdate.newLatLngBounds(
        LatLngBounds(
          southwest: LatLng(minLat, minLon),
          northeast: LatLng(maxLat, maxLon),
        ),
        left: 60,
        right: 60,
        top: 120,
        bottom: 300,
      ),
    );
  }

  bool get _canNavigate =>
      !_navigating &&
      widget.approxLabel == null &&
      (_route?.steps.isNotEmpty ?? false);

  /// Start-button tap: swap the dot for the heading arrow, subscribe to the
  /// compass and the position stream, and tilt the camera into follow mode.
  Future<void> _startNav() async {
    final c = _controller;
    final route = _route;
    final loc = ref.read(locationProvider);
    if (c == null || route == null || route.steps.isEmpty || !loc.hasFix) {
      return;
    }
    _navFix = LatLng(loc.lat!, loc.lon!);
    setState(() {
      _navigating = true;
      _stepIndex = 0;
      _offRouteFixes = 0;
    });
    await c.setLayerVisibility('me-halo', false);
    await c.setLayerVisibility('me-dot', false);
    await c.setLayerVisibility('me-arrow', true);
    _setMeSource(_navFix!, bearing: _heading ?? 0);

    // Fused platform compass (rotation vector / CLHeading); no web support.
    if (!kIsWeb) {
      _compassSub = FlutterCompass.events?.listen(_onCompass);
    }
    _posSub =
        Geolocator.getPositionStream(
          locationSettings: const LocationSettings(
            accuracy: LocationAccuracy.best,
            distanceFilter: 3,
          ),
          // Errors are swallowed: nav keeps working from the last good fix.
        ).listen(_onNavPosition, onError: (_) {});

    await c.animateCamera(
      CameraUpdate.newCameraPosition(
        CameraPosition(
          target: _navFix!,
          zoom: 17.5,
          tilt: 45,
          bearing: _heading ?? 0,
        ),
      ),
    );
  }

  Future<void> _stopNav() async {
    _compassSub?.cancel();
    _compassSub = null;
    _posSub?.cancel();
    _posSub = null;
    if (mounted) setState(() => _navigating = false);
    final c = _controller;
    if (c == null) return;
    await c.setLayerVisibility('me-arrow', false);
    await c.setLayerVisibility('me-halo', true);
    await c.setLayerVisibility('me-dot', true);
    // Level the camera (bearing/tilt back to 0) before re-framing the route.
    await c.animateCamera(
      CameraUpdate.newCameraPosition(
        CameraPosition(target: _navFix ?? widget.center, zoom: 15.5),
      ),
    );
    if (_navFix != null) await _fitBoth(_navFix!);
  }

  void _onCompass(CompassEvent e) {
    if (!mounted || !_navigating) return;
    final h = e.heading;
    if (h == null) {
      _compassAlive = false;
      return;
    }
    _compassAlive = true;
    _fuseHeading((h + 360) % 360);
  }

  void _onNavPosition(Position p) {
    if (!mounted || !_navigating) return;
    _navFix = LatLng(p.latitude, p.longitude);
    _setMeSource(_navFix!, bearing: _heading ?? 0);
    // GPS course beats a swinging compass once genuinely moving, and it is
    // the only heading source where the compass is unavailable (web).
    final fast = p.speed > 3.0;
    if ((!_compassAlive || fast) && p.heading >= 0 && p.speed > 0.5) {
      _fuseHeading(p.heading % 360);
    }
    _advanceSteps(p);
    _maybeReroute(p);
    _pushCamera(force: true);
    if (mounted && _navigating) setState(() {}); // banner distance countdown
  }

  void _fuseHeading(double raw) {
    _heading = _heading == null ? raw : smoothHeading(_heading!, raw, 0.25);
    _pushCamera();
  }

  static const _camThrottle = Duration(milliseconds: 180);

  /// Throttled + dead-banded camera follow: the compass fires at 10-50 Hz,
  /// the platform channel should see ~5 updates/s at most.
  void _pushCamera({bool force = false}) {
    final c = _controller;
    if (c == null || !_navigating || _navFix == null) return;
    final h = _heading ?? _lastCamHeading;
    final now = DateTime.now();
    if (now.difference(_lastCamAt) < _camThrottle) return;
    if (!force && angularDeltaDegrees(_lastCamHeading, h).abs() < 2) return;
    _lastCamAt = now;
    _lastCamHeading = h;
    c.animateCamera(
      CameraUpdate.newCameraPosition(
        CameraPosition(target: _navFix!, zoom: 17.5, tilt: 45, bearing: h),
      ),
      duration: const Duration(milliseconds: 200),
    );
  }

  void _advanceSteps(Position p) {
    final steps = _route?.steps ?? const <RouteStep>[];
    if (steps.isEmpty) return;
    final i = advanceStepIndex(
      maneuvers: [for (final s in steps) (lat: s.lat, lon: s.lon)],
      current: _stepIndex,
      lat: p.latitude,
      lon: p.longitude,
    );
    if (i != _stepIndex) setState(() => _stepIndex = i);
    final step = steps[i];
    final dist = haversineMeters(p.latitude, p.longitude, step.lat, step.lon);
    if (step.maneuverType == 'arrive' && dist < 20) {
      _stopNav();
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('You have arrived.')));
    }
  }

  /// Minimal off-route recovery: two consecutive fixes >40 m from the line
  /// (and >=15 s since the last fetch) silently re-route from where we are.
  Future<void> _maybeReroute(Position p) async {
    final route = _route;
    if (route == null) return;
    final coords = [
      for (final c in (route.geometry['coordinates'] as List? ?? const []))
        [(c[0] as num).toDouble(), (c[1] as num).toDouble()],
    ];
    final off = distanceToPolylineMeters(p.latitude, p.longitude, coords) > 40;
    _offRouteFixes = off ? _offRouteFixes + 1 : 0;
    if (_offRouteFixes < 2 ||
        _rerouting ||
        DateTime.now().difference(_lastRerouteAt) <
            const Duration(seconds: 15)) {
      return;
    }
    _rerouting = true;
    _lastRerouteAt = DateTime.now();
    final r = await fetchWalkingRoute(
      fromLat: p.latitude,
      fromLon: p.longitude,
      toLat: widget.center.latitude,
      toLon: widget.center.longitude,
    );
    _rerouting = false;
    if (r == null || !mounted || !_navigating) return; // best-effort
    _controller?.setGeoJsonSource('walk-route', {
      'type': 'FeatureCollection',
      'features': [
        {'type': 'Feature', 'properties': const {}, 'geometry': r.geometry},
      ],
    });
    setState(() {
      _route = r;
      _routeLabel = r.walkLabel;
      _stepIndex = 0;
      _offRouteFixes = 0;
    });
  }

  Widget _navBanner(BuildContext context) {
    final steps = _route!.steps;
    final step = steps[math.min(_stepIndex, steps.length - 1)];
    final fix = _navFix;
    final dist = fix == null
        ? null
        : haversineMeters(fix.latitude, fix.longitude, step.lat, step.lon);
    final instruction = step.instruction;
    final text = (dist == null || dist < 25)
        ? instruction
        : 'In ${(dist / 10).round() * 10} m, '
              '${instruction[0].toLowerCase()}${instruction.substring(1)}';
    return Material(
      color: Colors.black.withValues(alpha: 0.75),
      borderRadius: BorderRadius.circular(14),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        child: Row(
          children: [
            Icon(_maneuverIcon(step), color: Colors.white, size: 30),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                text,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 15,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            IconButton(
              color: Colors.white,
              icon: const Icon(Icons.close),
              tooltip: 'Stop navigation',
              onPressed: _stopNav,
            ),
          ],
        ),
      ),
    );
  }

  static IconData _maneuverIcon(RouteStep step) {
    if (step.maneuverType == 'arrive') return Icons.flag;
    if (step.maneuverType == 'depart') return Icons.navigation;
    switch (step.maneuverModifier) {
      case 'left':
        return Icons.turn_left;
      case 'right':
        return Icons.turn_right;
      case 'slight left':
        return Icons.turn_slight_left;
      case 'slight right':
        return Icons.turn_slight_right;
      case 'sharp left':
        return Icons.turn_sharp_left;
      case 'sharp right':
        return Icons.turn_sharp_right;
      case 'uturn':
        return Icons.u_turn_left;
      default:
        return Icons.straight;
    }
  }

  @override
  Widget build(BuildContext context) {
    final isVenueLevel =
        widget.approxLabel == null &&
        widget.event.venue.lat != 0 &&
        widget.event.venue.lon != 0;
    return Scaffold(
      // SizedBox.expand forces the Stack to fill the screen — the Scaffold
      // body passes loose constraints, so a bare Stack would otherwise shrink
      // to its small non-positioned child and the map would render as a strip.
      body: SizedBox.expand(
        child: Stack(
          children: [
            Positioned.fill(
            child: _showMap
                ? MapLibreMap(
                    styleString: mapStyleUrl,
                    initialCameraPosition: CameraPosition(
                      target: widget.center,
                      zoom: isVenueLevel ? 15.5 : 11.5,
                    ),
                    minMaxZoomPreference: const MinMaxZoomPreference(2, 19),
                    compassEnabled: false,
                    onMapCreated: (c) => _controller = c,
                    onStyleLoadedCallback: _onStyleLoaded,
                  )
                : ColoredBox(
                    color: Theme.of(context).colorScheme.surfaceContainerHighest,
                    child: const Center(child: CircularProgressIndicator()),
                  ),
          ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.all(8),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Material(
                        color: Colors.black.withValues(alpha: 0.4),
                        shape: const CircleBorder(),
                        child: IconButton(
                          color: Colors.white,
                          icon: const Icon(Icons.arrow_back),
                          onPressed: () => Navigator.of(context).maybePop(),
                        ),
                      ),
                      if (widget.approxLabel != null) ...[
                        const SizedBox(width: 8),
                        Flexible(
                          child: Material(
                            color: Colors.black.withValues(alpha: 0.55),
                            shape: const StadiumBorder(),
                            child: Padding(
                              padding: const EdgeInsets.symmetric(
                                horizontal: 12,
                                vertical: 6,
                              ),
                              child: Text(
                                'Approximate • ${widget.approxLabel}',
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(
                                  color: Colors.white,
                                  fontSize: 12,
                                ),
                              ),
                            ),
                          ),
                        ),
                      ],
                    ],
                  ),
                  if (_navigating && _route != null) ...[
                    const SizedBox(height: 8),
                    _navBanner(context),
                  ],
                ],
              ),
            ),
          ),
          Positioned(
            left: 12,
            right: 12,
            bottom: 12,
            child: SafeArea(
              top: false,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                mainAxisSize: MainAxisSize.min,
                children: [
                  // Location button: route from the user to the pin. Hidden
                  // in nav mode, where the camera already follows the user.
                  if (!_navigating) ...[
                    FloatingActionButton.small(
                      heroTag: 'venue-locate',
                      tooltip: 'Walking route from my location',
                      onPressed: _routing ? null : _routeFromMe,
                      child: _routing
                          ? const SizedBox(
                              width: 18,
                              height: 18,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Icon(Icons.my_location),
                    ),
                    const SizedBox(height: 8),
                  ],
                  Material(
                    elevation: 4,
                    borderRadius: BorderRadius.circular(16),
                    color: Theme.of(context).colorScheme.surface,
                    child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          // The pin's event, so the map is self-explanatory.
                          Text(
                            widget.event.title,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.titleSmall
                                ?.copyWith(fontWeight: FontWeight.w600),
                          ),
                          const SizedBox(height: 4),
                          Text(
                            [
                              categoryLabel(widget.event.category),
                              DateFormat.MMMEd().add_jm().format(
                                widget.event.startsAt.toLocal(),
                              ),
                              if (widget.event.venue.name.isNotEmpty)
                                widget.event.venue.name,
                            ].join(' • '),
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: Theme.of(context).textTheme.bodySmall
                                ?.copyWith(
                                  color: Theme.of(
                                    context,
                                  ).colorScheme.onSurfaceVariant,
                                ),
                          ),
                          if (_routeLabel != null) ...[
                            const SizedBox(height: 6),
                            Row(
                              children: [
                                Icon(
                                  Icons.directions_walk,
                                  size: 16,
                                  color: Theme.of(context).colorScheme.primary,
                                ),
                                const SizedBox(width: 6),
                                Expanded(
                                  child: Text(
                                    _routeLabel!,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: Theme.of(context)
                                        .textTheme
                                        .bodySmall
                                        ?.copyWith(
                                          color: Theme.of(
                                            context,
                                          ).colorScheme.primary,
                                          fontWeight: FontWeight.w600,
                                        ),
                                  ),
                                ),
                                if (_canNavigate) ...[
                                  const SizedBox(width: 8),
                                  FilledButton.icon(
                                    style: FilledButton.styleFrom(
                                      visualDensity: VisualDensity.compact,
                                    ),
                                    onPressed: _startNav,
                                    icon: const Icon(
                                      Icons.navigation,
                                      size: 16,
                                    ),
                                    label: const Text('Start'),
                                  ),
                                ],
                              ],
                            ),
                          ],
                          if (isVenueLevel && !_navigating) ...[
                            const SizedBox(height: 10),
                            DirectionsButtons(
                              lat: widget.event.venue.lat,
                              lon: widget.event.venue.lon,
                              label: widget.event.venue.name.isEmpty
                                  ? widget.event.title
                                  : widget.event.venue.name,
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
          ],
        ),
      ),
    );
  }
}
