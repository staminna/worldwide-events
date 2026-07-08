import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:intl/intl.dart';
import 'package:maplibre_gl/maplibre_gl.dart';

import '../api/routing_api.dart';
import '../models/event.dart';
import '../state/location.dart';
import '../util/geo.dart';
import '../util/map_pin.dart';
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

    if (mounted) _styleReady = true;
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

      c.setGeoJsonSource('me', {
        'type': 'FeatureCollection',
        'features': [
          {
            'type': 'Feature',
            'properties': const <String, dynamic>{},
            'geometry': {
              'type': 'Point',
              'coordinates': [me.longitude, me.latitude],
            },
          },
        ],
      });

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
      setState(() => _routeLabel = label);
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
              child: Row(
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
                  // Location button: route from the user to the pin.
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
                              ],
                            ),
                          ],
                          if (isVenueLevel) ...[
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
