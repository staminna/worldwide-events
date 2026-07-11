import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter_compass/flutter_compass.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';
import 'package:maplibre_gl/maplibre_gl.dart';

import '../api/routing_api.dart';
import '../models/chat.dart';
import '../state/location.dart';
import '../state/location_share.dart';
import '../util/geo.dart';
import '../util/map_pin.dart';
import '../util/nav_math.dart';
import '../widgets/directions_buttons.dart';

/// Full-screen turn-by-turn navigation to a friend sharing live location in
/// a chat group. Same engine as the venue navigator (position stream +
/// compass heading fusion, tilted follow camera, OSRM steps, off-route
/// recovery) with one fundamental difference: the destination MOVES — it
/// tracks the friend's latest [PeerFix] and re-routes when they relocate.
class FriendMapScreen extends ConsumerStatefulWidget {
  const FriendMapScreen({super.key, required this.peer});

  final PeerFix peer;

  @override
  ConsumerState<FriendMapScreen> createState() => _FriendMapScreenState();
}

class _FriendMapScreenState extends ConsumerState<FriendMapScreen> {
  MapLibreMapController? _controller;
  bool _styleReady = false;

  /// The live target. Starts from the tapped card's fix and follows
  /// peersProvider; kept at the last known position if they stop sharing.
  late PeerFix _peer = widget.peer;
  bool _stoppedSharing = false;

  bool _routing = false;
  String? _routeLabel;
  WalkRoute? _route;
  // Where the current route ends — re-route when the live peer drifts >30 m
  // from it.
  LatLng? _routeTarget;

  bool _navigating = false;
  int _stepIndex = 0;
  double? _heading;
  bool _compassAlive = false;
  LatLng? _navFix;
  StreamSubscription<CompassEvent>? _compassSub;
  StreamSubscription<Position>? _posSub;
  DateTime _lastCamAt = DateTime.fromMillisecondsSinceEpoch(0);
  double _lastCamHeading = 0;
  int _offRouteFixes = 0;
  DateTime _lastRerouteAt = DateTime.fromMillisecondsSinceEpoch(0);
  bool _rerouting = false;

  // Defer the platform view past the push transition (see venue_map_screen).
  bool _showMap = false;

  LatLng get _target => LatLng(_peer.lat, _peer.lon);

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
    final cs = Theme.of(context).colorScheme;
    final dpr = MediaQuery.devicePixelRatioOf(context);

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

    // The friend as a destination pin (tertiary — matches their map dot),
    // with their name underneath. The source moves as fixes arrive.
    final pin = await renderMapPin(cs.tertiary, dpr: dpr);
    await c.addImage('friend-pin', pin);
    await c.addSource('friend', const GeojsonSourceProperties(data: _emptyFc));
    await c.addSymbolLayer(
      'friend',
      'friend-pin-layer',
      const SymbolLayerProperties(
        iconImage: 'friend-pin',
        iconSize: 1,
        iconAnchor: 'bottom',
        iconAllowOverlap: true,
        iconIgnorePlacement: true,
        textField: ['get', 'name'],
        textFont: ['Montserrat Medium'],
        textSize: 12,
        textColor: '#ffffff',
        textHaloColor: 'rgba(0,0,0,0.7)',
        textHaloWidth: 1.2,
        textOffset: [0, 0.8],
        textAnchor: 'top',
      ),
    );
    _setFriendSource();

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

    if (!mounted) return;
    _styleReady = true;
    // Unlike the venue map (routes on demand), the user arrived here by
    // tapping "Navigate" — fetch the route right away.
    _routeFromMe();
  }

  void _setFriendSource() {
    _controller?.setGeoJsonSource('friend', {
      'type': 'FeatureCollection',
      'features': [
        {
          'type': 'Feature',
          'properties': {'name': _peer.name},
          'geometry': {
            'type': 'Point',
            'coordinates': [_peer.lon, _peer.lat],
          },
        },
      ],
    });
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

  /// Follows the live peer: move the pin, and while navigating re-route when
  /// they've drifted from where the current route ends. When they stop
  /// sharing, keep the last known position and say so.
  void _onPeersChanged(Map<String, PeerFix> peers) {
    final fresh = peers[widget.peer.userId];
    if (fresh == null) {
      if (!_stoppedSharing && mounted) setState(() => _stoppedSharing = true);
      return;
    }
    final moved = haversineMeters(fresh.lat, fresh.lon, _peer.lat, _peer.lon);
    final resumed = _stoppedSharing;
    if (moved < 1 && !resumed) return;
    setState(() {
      _peer = fresh;
      _stoppedSharing = false;
    });
    _setFriendSource();
    final target = _routeTarget;
    if (target != null &&
        haversineMeters(
              fresh.lat,
              fresh.lon,
              target.latitude,
              target.longitude,
            ) >
            30) {
      _rerouteTo(fresh);
    }
  }

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
        toLat: _peer.lat,
        toLon: _peer.lon,
      );
      final geometry =
          route?.geometry ??
          {
            'type': 'LineString',
            'coordinates': [
              [me.longitude, me.latitude],
              [_peer.lon, _peer.lat],
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
          '${(haversineMeters(me.latitude, me.longitude, _peer.lat, _peer.lon) / 1000).toStringAsFixed(1)} km straight line — walking route unavailable';
      if (!mounted) return;
      setState(() {
        _route = route;
        _routeTarget = _target;
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
    final minLat = math.min(me.latitude, _peer.lat);
    final maxLat = math.max(me.latitude, _peer.lat);
    final minLon = math.min(me.longitude, _peer.lon);
    final maxLon = math.max(me.longitude, _peer.lon);
    if (maxLat - minLat < 0.0005 && maxLon - minLon < 0.0005) {
      await _controller?.animateCamera(CameraUpdate.newLatLngZoom(_target, 16));
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

  bool get _canNavigate => !_navigating && (_route?.steps.isNotEmpty ?? false);

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

    if (!kIsWeb) {
      _compassSub = FlutterCompass.events?.listen(_onCompass);
    }
    _posSub =
        Geolocator.getPositionStream(
          locationSettings: const LocationSettings(
            accuracy: LocationAccuracy.best,
            distanceFilter: 3,
          ),
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
    await c.animateCamera(
      CameraUpdate.newCameraPosition(
        CameraPosition(target: _navFix ?? _target, zoom: 15.5),
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
    final fast = p.speed > 3.0;
    if ((!_compassAlive || fast) && p.heading >= 0 && p.speed > 0.5) {
      _fuseHeading(p.heading % 360);
    }
    // Arrival is proximity to the LIVE friend, not the route's endpoint —
    // the endpoint may be where they used to be.
    final toFriend = haversineMeters(
      p.latitude,
      p.longitude,
      _peer.lat,
      _peer.lon,
    );
    if (toFriend < 25) {
      _stopNav();
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('You found ${_peer.name} 🎉')),
      );
      return;
    }
    _advanceSteps(p);
    _maybeReroute(p);
    _pushCamera(force: true);
    if (mounted && _navigating) setState(() {});
  }

  void _fuseHeading(double raw) {
    _heading = _heading == null ? raw : smoothHeading(_heading!, raw, 0.25);
    _pushCamera();
  }

  static const _camThrottle = Duration(milliseconds: 180);

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
  }

  /// Off-route recovery, identical to the venue navigator. Target-moved
  /// re-routes come from [_onPeersChanged] instead; both honor the same
  /// 15-second minimum between fetches via [_rerouteTo].
  Future<void> _maybeReroute(Position p) async {
    final route = _route;
    if (route == null) return;
    final coords = [
      for (final c in (route.geometry['coordinates'] as List? ?? const []))
        [(c[0] as num).toDouble(), (c[1] as num).toDouble()],
    ];
    final off = distanceToPolylineMeters(p.latitude, p.longitude, coords) > 40;
    _offRouteFixes = off ? _offRouteFixes + 1 : 0;
    if (_offRouteFixes < 2) return;
    await _rerouteTo(_peer);
  }

  /// Re-fetches the route from the latest known own position to [to],
  /// throttled to one fetch per 15 s and one in flight.
  Future<void> _rerouteTo(PeerFix to) async {
    if (_rerouting ||
        DateTime.now().difference(_lastRerouteAt) <
            const Duration(seconds: 15)) {
      return;
    }
    final from = _navFix ??
        (ref.read(locationProvider).hasFix
            ? LatLng(
                ref.read(locationProvider).lat!,
                ref.read(locationProvider).lon!,
              )
            : null);
    if (from == null) return;
    _rerouting = true;
    _lastRerouteAt = DateTime.now();
    final r = await fetchWalkingRoute(
      fromLat: from.latitude,
      fromLon: from.longitude,
      toLat: to.lat,
      toLon: to.lon,
    );
    _rerouting = false;
    if (r == null || !mounted) return;
    _controller?.setGeoJsonSource('walk-route', {
      'type': 'FeatureCollection',
      'features': [
        {'type': 'Feature', 'properties': const {}, 'geometry': r.geometry},
      ],
    });
    setState(() {
      _route = r;
      _routeTarget = LatLng(to.lat, to.lon);
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

  String _freshness() {
    final secs = DateTime.now().difference(_peer.at).inSeconds;
    if (secs < 45) return 'live';
    if (secs < 120) return 'updated ${secs}s ago';
    return 'updated ${(secs / 60).round()} min ago';
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(peersProvider, (_, next) => _onPeersChanged(next));
    final cs = Theme.of(context).colorScheme;
    final loc = ref.watch(locationProvider);
    final distance = loc.hasFix
        ? haversineMeters(loc.lat!, loc.lon!, _peer.lat, _peer.lon)
        : null;
    final distanceLabel = distance == null
        ? null
        : distance < 1000
        ? '${distance.round()} m away'
        : '${(distance / 1000).toStringAsFixed(1)} km away';

    return Scaffold(
      body: SizedBox.expand(
        child: Stack(
          children: [
            Positioned.fill(
              child: _showMap
                  ? MapLibreMap(
                      styleString: mapStyleUrl,
                      initialCameraPosition: CameraPosition(
                        target: _target,
                        zoom: 14,
                      ),
                      minMaxZoomPreference: const MinMaxZoomPreference(2, 19),
                      compassEnabled: false,
                      onMapCreated: (c) => _controller = c,
                      onStyleLoadedCallback: _onStyleLoaded,
                    )
                  : ColoredBox(
                      color: cs.surfaceContainerHighest,
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
                        if (_stoppedSharing) ...[
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
                                  '${_peer.name} stopped sharing — heading to their last position',
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
                    if (!_navigating) ...[
                      FloatingActionButton.small(
                        heroTag: 'friend-locate',
                        tooltip: 'Walking route from my location',
                        onPressed: _routing ? null : _routeFromMe,
                        child: _routing
                            ? const SizedBox(
                                width: 18,
                                height: 18,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                ),
                              )
                            : const Icon(Icons.my_location),
                      ),
                      const SizedBox(height: 8),
                    ],
                    Material(
                      elevation: 4,
                      borderRadius: BorderRadius.circular(16),
                      color: cs.surface,
                      child: Padding(
                        padding: const EdgeInsets.all(12),
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                CircleAvatar(
                                  radius: 16,
                                  backgroundColor: cs.tertiaryContainer,
                                  child: Text(
                                    _peer.name.isEmpty
                                        ? '?'
                                        : _peer.name.characters.first
                                              .toUpperCase(),
                                    style: TextStyle(
                                      color: cs.onTertiaryContainer,
                                      fontWeight: FontWeight.w600,
                                      fontSize: 14,
                                    ),
                                  ),
                                ),
                                const SizedBox(width: 10),
                                Expanded(
                                  child: Column(
                                    crossAxisAlignment:
                                        CrossAxisAlignment.start,
                                    children: [
                                      Text(
                                        _peer.name,
                                        maxLines: 1,
                                        overflow: TextOverflow.ellipsis,
                                        style: Theme.of(context)
                                            .textTheme
                                            .titleSmall
                                            ?.copyWith(
                                              fontWeight: FontWeight.w600,
                                            ),
                                      ),
                                      Text(
                                        [
                                          ?distanceLabel,
                                          _stoppedSharing
                                              ? 'stopped sharing'
                                              : _freshness(),
                                        ].join(' • '),
                                        style: Theme.of(context)
                                            .textTheme
                                            .bodySmall
                                            ?.copyWith(
                                              color: cs.onSurfaceVariant,
                                            ),
                                      ),
                                    ],
                                  ),
                                ),
                              ],
                            ),
                            if (_routeLabel != null) ...[
                              const SizedBox(height: 6),
                              Row(
                                children: [
                                  Icon(
                                    Icons.directions_walk,
                                    size: 16,
                                    color: cs.primary,
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
                                            color: cs.primary,
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
                            if (!_navigating) ...[
                              const SizedBox(height: 10),
                              DirectionsButtons(
                                lat: _peer.lat,
                                lon: _peer.lon,
                                label: _peer.name,
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
