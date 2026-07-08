import 'dart:math' as math;

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';
import 'package:maplibre_gl/maplibre_gl.dart';

import '../api/event_api.dart';
import '../api/routing_api.dart';
import '../models/event.dart';
import '../state/location.dart';
import '../state/providers.dart';
import '../util/geo.dart';
import '../widgets/category_style.dart';
import '../widgets/directions_buttons.dart';
import '../widgets/location_search_field.dart';

/// Beyond this straight-line distance we don't fetch a walking route — the
/// deep-link buttons cover the "too far to walk" case.
const _walkRouteMaxMeters = 2500.0;

/// Zoom level where the heatmap hands over to clusters/dots.
const _heatToPinsZoom = 9.0;

class MapScreen extends ConsumerStatefulWidget {
  const MapScreen({super.key});

  @override
  ConsumerState<MapScreen> createState() => _MapScreenState();
}

class _MapScreenState extends ConsumerState<MapScreen> {
  MapLibreMapController? _controller;
  bool _styleReady = false;
  Event? _selected;
  WalkRoute? _walkRoute;
  List<Event> _placed = const [];
  // Display coordinates parallel to _placed: events sharing a coordinate (a
  // city centroid) are fanned out into a ring so each is visible/tappable.
  List<LatLng> _placedPoints = const [];
  // Identity of the feed.events list we last derived _placed from, so rebuilds
  // triggered by camera/GPS/fullscreen ticks don't re-scan the whole feed.
  List<Event>? _lastSyncedEvents;

  @override
  void dispose() {
    // Leaving the map must not strand the app in immersive mode.
    if (ref.read(mapFullscreenProvider)) {
      SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    }
    super.dispose();
  }

  Future<void> _goToMyLocation() async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(locationProvider.notifier).refreshFix();
      final loc = ref.read(locationProvider);
      if (loc.hasFix) {
        await _controller?.animateCamera(
          CameraUpdate.newLatLngZoom(LatLng(loc.lat!, loc.lon!), 12),
        );
      }
    } catch (e) {
      messenger.showSnackBar(
        SnackBar(
          content: Text(
            e is LocationException ? e.message : 'Location lookup failed: $e',
          ),
        ),
      );
    }
  }

  void _setFullscreen(bool on) {
    ref.read(mapFullscreenProvider.notifier).state = on;
    SystemChrome.setEnabledSystemUIMode(
      on ? SystemUiMode.immersiveSticky : SystemUiMode.edgeToEdge,
    );
  }

  Future<void> _flyToSearch(LocationResult r) async {
    await _controller?.animateCamera(
      CameraUpdate.newLatLngZoom(LatLng(r.lat, r.lon), 13),
    );
  }

  @override
  Widget build(BuildContext context) {
    final feed = ref.watch(eventFeedProvider);
    final loc = ref.watch(locationProvider);
    final fullscreen = ref.watch(mapFullscreenProvider);
    ref.listen(locationProvider, (_, next) => _syncMySource(next));

    if (feed.loading && feed.events.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }
    if (feed.error != null && feed.events.isEmpty) {
      return Center(child: Text('Error: ${feed.error}'));
    }
    // Only re-derive placed events (and re-push the map sources) when the feed
    // instance actually changes — build() runs on every camera move, GPS fix,
    // and fullscreen toggle, and the old code re-scanned every event each time.
    if (!identical(feed.events, _lastSyncedEvents)) {
      _lastSyncedEvents = feed.events;
      _placed = feed.events
          .where((e) => e.venue.lat != 0 && e.venue.lon != 0)
          .toList();
      _placedPoints = _spread(_placed);
      _syncEventSources();
    }

    return Stack(
      children: [
        MapLibreMap(
          styleString: mapStyleUrl,
          initialCameraPosition: CameraPosition(
            target: _initialCenter(_placed),
            zoom: _placed.length > 50 ? 2.5 : 4,
          ),
          minMaxZoomPreference: const MinMaxZoomPreference(1, 18),
          trackCameraPosition: true,
          compassEnabled: false,
          // The location fix comes from LocationState; we draw our own dot
          // instead of requesting the platform puck (extra permissions flow).
          myLocationEnabled: false,
          onMapCreated: (c) {
            _controller = c;
            // Taps on interactive layers arrive here, NOT via onMapClick —
            // the plugin splits the two (see featureTapsTriggersMapClick).
            c.onFeatureTapped.add(_onFeatureTap);
          },
          onStyleLoadedCallback: _onStyleLoaded,
          onMapClick: _onMapClick,
        ),
        Positioned(
          right: 12,
          // Lift the button clear of the selected-event card when one shows.
          bottom: _selected == null ? 16 : 196,
          child: FloatingActionButton.small(
            heroTag: 'map-locate',
            tooltip: 'My location',
            onPressed: loc.locating ? null : _goToMyLocation,
            child: loc.locating
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.my_location),
          ),
        ),
        // Top controls: search + count, hidden in immersive fullscreen.
        if (!fullscreen)
          Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: SafeArea(
              bottom: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(12, 12, 12, 0),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Expanded(
                          child: Material(
                            elevation: 3,
                            borderRadius: BorderRadius.circular(12),
                            child: LocationSearchField(
                              hintText: 'Search the map…',
                              onSelected: _flyToSearch,
                            ),
                          ),
                        ),
                        const SizedBox(width: 8),
                        _RoundMapButton(
                          icon: Icons.fullscreen,
                          tooltip: 'Full screen',
                          onTap: () => _setFullscreen(true),
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    _MapTopBar(
                      count: _placed.length,
                      total: feed.events.length,
                    ),
                  ],
                ),
              ),
            ),
          ),
        // Exit-fullscreen affordance while immersive.
        if (fullscreen)
          Positioned(
            top: 0,
            right: 0,
            child: SafeArea(
              bottom: false,
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: _RoundMapButton(
                  icon: Icons.fullscreen_exit,
                  tooltip: 'Exit full screen',
                  onTap: () => _setFullscreen(false),
                ),
              ),
            ),
          ),
        if (_selected != null)
          Positioned(
            left: 12,
            right: 12,
            bottom: 12,
            child: _SelectedEventCard(
              event: _selected!,
              walkRoute: _walkRoute,
              onClose: _clearSelection,
              onOpen: () => context.push('/event/${_selected!.id}'),
            ),
          ),
      ],
    );
  }

  LatLng _initialCenter(List<Event> events) {
    if (events.isEmpty) return const LatLng(20, 0);
    double lat = 0, lon = 0;
    for (final e in events) {
      lat += e.venue.lat;
      lon += e.venue.lon;
    }
    return LatLng(lat / events.length, lon / events.length);
  }

  // --- Style: sources & layers -------------------------------------------

  static Map<String, dynamic> _fc(List<Map<String, dynamic>> features) => {
    'type': 'FeatureCollection',
    'features': features,
  };

  static Map<String, dynamic> _pointFeature(
    double lat,
    double lon, {
    Map<String, dynamic> properties = const {},
    // The top-level feature id is what onFeatureTapped reports back. It must
    // be numeric: MapLibre GL (web especially) only round-trips numeric
    // GeoJSON ids through queryRenderedFeatures, so we use the placed-event
    // index and resolve the Event from it on tap.
    int? id,
  }) => {
    'type': 'Feature',
    'id': ?id,
    'properties': properties,
    'geometry': {
      'type': 'Point',
      'coordinates': [lon, lat],
    },
  };

  Future<void> _onStyleLoaded() async {
    final c = _controller;
    if (c == null || !mounted) return;
    final cs = Theme.of(context).colorScheme;

    // Clustered source for pins, plus an unclustered twin for the heatmap
    // (clustering collapses points, which would flatten the heat density).
    await c.addSource(
      'events',
      const GeojsonSourceProperties(
        data: {'type': 'FeatureCollection', 'features': []},
        cluster: true,
        clusterRadius: 50,
        clusterMaxZoom: 14,
      ),
    );
    for (final id in ['events-heat', 'route', 'selected', 'me']) {
      await c.addSource(
        id,
        const GeojsonSourceProperties(
          data: {'type': 'FeatureCollection', 'features': []},
        ),
      );
    }

    // Kepler-style handover: the heatmap fades out exactly as the
    // cluster/dot layers become visible — all native zoom expressions, no
    // Dart-side zoom listener.
    await c.addHeatmapLayer(
      'events-heat',
      'heat',
      const HeatmapLayerProperties(
        heatmapOpacity: [
          'interpolate',
          ['linear'],
          ['zoom'],
          _heatToPinsZoom - 2,
          0.9,
          _heatToPinsZoom,
          0.0,
        ],
        heatmapRadius: [
          'interpolate',
          ['linear'],
          ['zoom'],
          0,
          8,
          _heatToPinsZoom,
          26,
        ],
        heatmapIntensity: [
          'interpolate',
          ['linear'],
          ['zoom'],
          0,
          0.6,
          _heatToPinsZoom,
          1.3,
        ],
        heatmapColor: [
          'interpolate',
          ['linear'],
          ['heatmap-density'],
          0,
          'rgba(0,0,0,0)',
          0.2,
          '#2c1e5c',
          0.4,
          '#7b2f9e',
          0.6,
          '#d63a83',
          0.8,
          '#f8a45c',
          1,
          '#fdeca6',
        ],
      ),
      maxzoom: _heatToPinsZoom + 1,
    );

    await c.addLineLayer(
      'route',
      'route-line',
      const LineLayerProperties(
        lineColor: '#4dd0e1',
        lineWidth: 4.5,
        lineOpacity: 0.9,
        lineJoin: 'round',
        lineCap: 'round',
      ),
      enableInteraction: false,
    );

    // Same 3 tiers the old _ClusterBubble used (<10, <100, 100+).
    await c.addCircleLayer(
      'events',
      'clusters',
      CircleLayerProperties(
        circleColor: [
          'step',
          ['get', 'point_count'],
          hexColor(cs.primary),
          10,
          hexColor(cs.secondary),
          100,
          hexColor(cs.tertiary),
        ],
        circleRadius: [
          'step',
          ['get', 'point_count'],
          16,
          10,
          19,
          100,
          25,
        ],
        circleOpacity: 0.92,
        circleStrokeColor: '#ffffff',
        circleStrokeWidth: 2,
      ),
      minzoom: _heatToPinsZoom,
      filter: ['has', 'point_count'],
    );
    await c.addSymbolLayer(
      'events',
      'cluster-count',
      const SymbolLayerProperties(
        textField: ['get', 'point_count_abbreviated'],
        // Verified present in the CARTO Dark Matter glyph set.
        textFont: ['Montserrat Medium'],
        textSize: 13,
        textColor: '#ffffff',
        textAllowOverlap: true,
        textIgnorePlacement: true,
      ),
      minzoom: _heatToPinsZoom,
      filter: ['has', 'point_count'],
    );

    // Kepler-style dots, colored by category (single source of truth stays
    // categoryColor in category_style.dart).
    await c.addCircleLayer(
      'events',
      'event-dots',
      CircleLayerProperties(
        circleColor: _categoryColorExpression(cs),
        circleRadius: [
          'interpolate',
          ['linear'],
          ['zoom'],
          _heatToPinsZoom,
          5,
          16,
          10,
        ],
        circleStrokeColor: '#ffffff',
        circleStrokeWidth: 1.5,
      ),
      minzoom: _heatToPinsZoom,
      filter: [
        '!',
        ['has', 'point_count'],
      ],
    );

    // Decorative layers opt out of hit-testing so taps on them fall through
    // to the event dots (or clear the selection) instead of being swallowed.
    await c.addCircleLayer(
      'selected',
      'selected-ring',
      const CircleLayerProperties(
        circleRadius: 13,
        circleColor: 'rgba(255,255,255,0.15)',
        circleStrokeColor: '#ffffff',
        circleStrokeWidth: 2.5,
      ),
      enableInteraction: false,
    );

    // The classic blue "you are here" dot with a white ring.
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

    if (!mounted) return;
    _styleReady = true;
    _syncEventSources();
    _syncMySource(ref.read(locationProvider));
  }

  /// `['match', category, <name>, <hex>, ..., fallback]` built from the enum
  /// so new categories can't silently fall out of sync with the feed chips.
  List<Object> _categoryColorExpression(ColorScheme cs) {
    final expr = <Object>[
      'match',
      ['get', 'category'],
    ];
    for (final cat in EventCategory.values) {
      if (cat == EventCategory.unknown) continue;
      expr
        ..add(cat.name)
        ..add(hexColor(categoryColor(cs, cat)));
    }
    expr.add(hexColor(categoryColor(cs, EventCategory.unknown)));
    return expr;
  }

  // --- Data sync -----------------------------------------------------------

  /// Events without a real venue coordinate fall back to their city centroid,
  /// so many pile onto one point. Fan each such group out into a ring around
  /// the shared point so every event is individually visible and tappable —
  /// they still cluster when zoomed out. Deterministic (no random jitter).
  List<LatLng> _spread(List<Event> events) {
    const earthR = 6371000.0;
    final groups = <String, List<int>>{};
    for (var i = 0; i < events.length; i++) {
      final v = events[i].venue;
      final key = '${v.lat.toStringAsFixed(4)},${v.lon.toStringAsFixed(4)}';
      (groups[key] ??= []).add(i);
    }
    final points = List<LatLng>.filled(events.length, const LatLng(0, 0));
    for (final idxs in groups.values) {
      final n = idxs.length;
      if (n == 1) {
        final v = events[idxs.first].venue;
        points[idxs.first] = LatLng(v.lat, v.lon);
        continue;
      }
      // Ring radius grows with the count so dense centroids stay legible.
      final radiusMeters = 90.0 * math.sqrt(n.toDouble());
      for (var k = 0; k < n; k++) {
        final v = events[idxs[k]].venue;
        final angle = 2 * math.pi * k / n;
        final dLat = radiusMeters * math.sin(angle) / earthR * 180 / math.pi;
        final dLon =
            radiusMeters *
            math.cos(angle) /
            (earthR * math.cos(v.lat * math.pi / 180)) *
            180 /
            math.pi;
        points[idxs[k]] = LatLng(v.lat + dLat, v.lon + dLon);
      }
    }
    return points;
  }

  /// The display (fanned-out) point for an event, matched by id.
  LatLng _pointFor(Event e) {
    final i = _placed.indexWhere((p) => p.id == e.id);
    return (i >= 0 && i < _placedPoints.length)
        ? _placedPoints[i]
        : LatLng(e.venue.lat, e.venue.lon);
  }

  void _syncEventSources() {
    final c = _controller;
    if (c == null || !_styleReady) return;
    // Callers are already gated on feed-identity change (build) or one-shot
    // style load, so this pushes only when the data genuinely changed.
    final fc = _fc([
      for (var i = 0; i < _placed.length; i++)
        _pointFeature(
          _placedPoints[i].latitude,
          _placedPoints[i].longitude,
          id: i,
          properties: {
            'category': _placed[i].category.name,
            'title': _placed[i].title,
          },
        ),
    ]);
    c.setGeoJsonSource('events', fc);
    c.setGeoJsonSource('events-heat', fc);
  }

  void _syncMySource(LocationState loc) {
    final c = _controller;
    if (c == null || !_styleReady) return;
    c.setGeoJsonSource(
      'me',
      _fc([if (loc.hasFix) _pointFeature(loc.lat!, loc.lon!)]),
    );
  }

  // --- Selection & routing -------------------------------------------------

  /// Fires only for taps that hit no interactive feature — the plugin routes
  /// feature hits to [_onFeatureTap] instead.
  void _onMapClick(math.Point<double> point, LatLng latLng) {
    _clearSelection();
  }

  void _onFeatureTap(
    math.Point<double> point,
    LatLng latLng,
    String id,
    String layerId,
    Annotation? annotation,
  ) {
    if (!mounted) return;
    if (layerId == 'clusters' || layerId == 'cluster-count') {
      // Zoom in to break the cluster apart. The spread fix means co-located
      // events separate into individual, tappable pins as you zoom.
      final c = _controller;
      if (c == null) return;
      final zoom = c.cameraPosition?.zoom ?? _heatToPinsZoom;
      c.animateCamera(
        CameraUpdate.newLatLngZoom(latLng, math.min(zoom + 2.5, 18)),
      );
      return;
    }
    if (layerId != 'event-dots') return;
    final idx = int.tryParse(id);
    if (idx == null || idx < 0 || idx >= _placed.length) return;
    _select(_placed[idx], _placedPoints[idx]);
  }

  void _select(Event e, [LatLng? at]) {
    final point = at ?? _pointFor(e);
    setState(() {
      _selected = e;
      _walkRoute = null;
    });
    final c = _controller;
    c?.setGeoJsonSource(
      'selected',
      _fc([_pointFeature(point.latitude, point.longitude)]),
    );
    c?.setGeoJsonSource('route', _fc([]));
    _maybeFetchRoute(e);
  }

  void _clearSelection() {
    if (_selected == null) return;
    setState(() {
      _selected = null;
      _walkRoute = null;
    });
    _controller?.setGeoJsonSource('selected', _fc([]));
    _controller?.setGeoJsonSource('route', _fc([]));
  }

  Future<void> _maybeFetchRoute(Event e) async {
    final loc = ref.read(locationProvider);
    if (!loc.hasFix) return;
    final meters = haversineMeters(
      loc.lat!,
      loc.lon!,
      e.venue.lat,
      e.venue.lon,
    );
    if (meters > _walkRouteMaxMeters) return;
    final route = await fetchWalkingRoute(
      fromLat: loc.lat!,
      fromLon: loc.lon!,
      toLat: e.venue.lat,
      toLon: e.venue.lon,
    );
    // Guard against a stale response landing after the user moved on.
    if (!mounted || route == null || _selected?.id != e.id) return;
    _controller?.setGeoJsonSource(
      'route',
      _fc([
        {'type': 'Feature', 'properties': {}, 'geometry': route.geometry},
      ]),
    );
    setState(() => _walkRoute = route);
  }
}

/// A circular surface-colored icon button matching the map overlay chrome.
class _RoundMapButton extends StatelessWidget {
  const _RoundMapButton({
    required this.icon,
    required this.tooltip,
    required this.onTap,
  });

  final IconData icon;
  final String tooltip;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Material(
      elevation: 3,
      shape: const CircleBorder(),
      color: cs.surface,
      child: IconButton(
        tooltip: tooltip,
        icon: Icon(icon, color: cs.onSurface),
        onPressed: onTap,
      ),
    );
  }
}

class _MapTopBar extends StatelessWidget {
  const _MapTopBar({required this.count, required this.total});
  final int count;
  final int total;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final hidden = total - count;
    return Material(
      elevation: 4,
      borderRadius: BorderRadius.circular(28),
      color: cs.surface,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        child: Row(
          children: [
            Icon(Icons.public, size: 18, color: cs.primary),
            const SizedBox(width: 8),
            Text(
              '$count events on the map',
              style: Theme.of(context).textTheme.titleSmall,
            ),
            if (hidden > 0) ...[
              const SizedBox(width: 8),
              Text(
                '($hidden without coordinates)',
                style: Theme.of(
                  context,
                ).textTheme.bodySmall?.copyWith(color: cs.onSurfaceVariant),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _SelectedEventCard extends StatelessWidget {
  const _SelectedEventCard({
    required this.event,
    required this.walkRoute,
    required this.onClose,
    required this.onOpen,
  });

  final Event event;
  final WalkRoute? walkRoute;
  final VoidCallback onClose;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final fmt = DateFormat.MMMd().add_jm();
    final accent = categoryColor(cs, event.category);
    final where = event.venue.name.isNotEmpty
        ? '${event.venue.name} • ${event.city}'
        : event.city;
    return Card(
      elevation: 6,
      clipBehavior: Clip.antiAlias,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(height: 4, color: accent),
          ListTile(
            leading: ClipRRect(
              borderRadius: BorderRadius.circular(8),
              child: SizedBox(
                width: 52,
                height: 52,
                child: event.imageUrl.isNotEmpty
                    ? CachedNetworkImage(
                        imageUrl: proxiedImage(event.imageUrl),
                        fit: BoxFit.cover,
                        memCacheWidth: 120,
                        errorWidget: (_, _, _) =>
                            Container(color: cs.surfaceContainerHighest),
                        placeholder: (_, _) =>
                            Container(color: cs.surfaceContainerHighest),
                      )
                    : Container(
                        color: cs.surfaceContainerHighest,
                        child: Icon(Icons.event, size: 22, color: cs.outline),
                      ),
              ),
            ),
            title: Text(
              event.title,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontWeight: FontWeight.w600),
            ),
            subtitle: Text(
              '${categoryLabel(event.category)} • '
              '${fmt.format(event.startsAt.toLocal())}\n$where'
              '${walkRoute != null ? '  🚶 ${walkRoute!.walkLabel}' : ''}',
            ),
            isThreeLine: true,
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                IconButton(
                  icon: const Icon(Icons.open_in_new),
                  onPressed: onOpen,
                ),
                IconButton(icon: const Icon(Icons.close), onPressed: onClose),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 10),
            child: DirectionsButtons(
              lat: event.venue.lat,
              lon: event.venue.lon,
              label: event.venue.name.isEmpty ? event.title : event.venue.name,
              compact: true,
            ),
          ),
        ],
      ),
    );
  }
}
