import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';
import 'package:maplibre_gl/maplibre_gl.dart';

import '../api/routing_api.dart';
import '../models/event.dart';
import '../util/geo.dart';

/// Chains the given events (already ordered) into a single walking itinerary:
/// numbered stops + the OSRM foot route between them, with per-leg and total
/// walk times.
class CrawlScreen extends StatefulWidget {
  const CrawlScreen({super.key, required this.stops});

  final List<Event> stops;

  @override
  State<CrawlScreen> createState() => _CrawlScreenState();
}

class _CrawlScreenState extends State<CrawlScreen> {
  MapLibreMapController? _controller;
  bool _styleReady = false;
  WalkTour? _tour;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _loadTour();
  }

  Future<void> _loadTour() async {
    final tour = await fetchWalkingTour([
      for (final e in widget.stops) (lat: e.venue.lat, lon: e.venue.lon),
    ]);
    if (!mounted) return;
    setState(() {
      _tour = tour;
      _loading = false;
    });
    _drawRoute();
  }

  Future<void> _onStyleLoaded() async {
    final c = _controller;
    if (c == null || !mounted) return;
    final cs = Theme.of(context).colorScheme;

    // Route first so the numbered stops draw on top of the line.
    await c.addSource(
      'crawl-route',
      const GeojsonSourceProperties(
        data: {'type': 'FeatureCollection', 'features': []},
      ),
    );
    await c.addLineLayer(
      'crawl-route',
      'crawl-line',
      const LineLayerProperties(
        lineColor: '#4dd0e1',
        lineWidth: 4.5,
        lineOpacity: 0.9,
        lineJoin: 'round',
        lineCap: 'round',
      ),
      enableInteraction: false,
    );

    await c.addSource(
      'crawl-stops',
      GeojsonSourceProperties(data: _stopsFc()),
    );
    await c.addCircleLayer(
      'crawl-stops',
      'crawl-stop-dot',
      CircleLayerProperties(
        circleRadius: 15,
        circleColor: hexColor(cs.primary),
        circleStrokeColor: '#ffffff',
        circleStrokeWidth: 2,
      ),
    );
    await c.addSymbolLayer(
      'crawl-stops',
      'crawl-stop-num',
      const SymbolLayerProperties(
        textField: ['get', 'n'],
        textFont: ['Montserrat Medium'],
        textSize: 13,
        textColor: '#ffffff',
        textAllowOverlap: true,
        textIgnorePlacement: true,
      ),
    );

    _styleReady = true;
    _fitBounds();
    _drawRoute();
  }

  Map<String, dynamic> _stopsFc() => {
    'type': 'FeatureCollection',
    'features': [
      for (var i = 0; i < widget.stops.length; i++)
        {
          'type': 'Feature',
          'properties': {'n': '${i + 1}'},
          'geometry': {
            'type': 'Point',
            'coordinates': [widget.stops[i].venue.lon, widget.stops[i].venue.lat],
          },
        },
    ],
  };

  void _drawRoute() {
    final tour = _tour;
    if (!_styleReady || tour == null) return;
    _controller?.setGeoJsonSource('crawl-route', {
      'type': 'FeatureCollection',
      'features': [
        {'type': 'Feature', 'properties': const {}, 'geometry': tour.geometry},
      ],
    });
  }

  void _fitBounds() {
    if (widget.stops.isEmpty) return;
    var minLat = 90.0, maxLat = -90.0, minLon = 180.0, maxLon = -180.0;
    for (final e in widget.stops) {
      minLat = math.min(minLat, e.venue.lat);
      maxLat = math.max(maxLat, e.venue.lat);
      minLon = math.min(minLon, e.venue.lon);
      maxLon = math.max(maxLon, e.venue.lon);
    }
    // All stops on ~one point (city-centroid data) → just centre on it.
    if (maxLat - minLat < 0.002 && maxLon - minLon < 0.002) {
      _controller?.animateCamera(
        CameraUpdate.newLatLngZoom(
          LatLng((minLat + maxLat) / 2, (minLon + maxLon) / 2),
          14,
        ),
      );
      return;
    }
    _controller?.animateCamera(
      CameraUpdate.newLatLngBounds(
        LatLngBounds(
          southwest: LatLng(minLat, minLon),
          northeast: LatLng(maxLat, maxLon),
        ),
        left: 48,
        right: 48,
        top: 96,
        bottom: 280,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Stack(
        children: [
          Positioned.fill(
            child: MapLibreMap(
              styleString: mapStyleUrl,
              initialCameraPosition: CameraPosition(
                target: LatLng(
                  widget.stops.first.venue.lat,
                  widget.stops.first.venue.lon,
                ),
                zoom: 13,
              ),
              compassEnabled: false,
              onMapCreated: (c) => _controller = c,
              onStyleLoadedCallback: _onStyleLoaded,
            ),
          ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.all(8),
              child: Material(
                color: Colors.black.withValues(alpha: 0.4),
                shape: const CircleBorder(),
                child: IconButton(
                  color: Colors.white,
                  icon: const Icon(Icons.arrow_back),
                  onPressed: () => Navigator.of(context).maybePop(),
                ),
              ),
            ),
          ),
          Align(
            alignment: Alignment.bottomCenter,
            child: _CrawlPanel(
              stops: widget.stops,
              tour: _tour,
              loading: _loading,
            ),
          ),
        ],
      ),
    );
  }
}

class _CrawlPanel extends StatelessWidget {
  const _CrawlPanel({
    required this.stops,
    required this.tour,
    required this.loading,
  });

  final List<Event> stops;
  final WalkTour? tour;
  final bool loading;

  String _legLabel(double seconds) {
    final mins = (seconds / 60).round();
    return mins <= 0 ? 'same spot' : '$mins min walk';
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final fmt = DateFormat.MMMEd().add_jm();
    return Container(
      margin: const EdgeInsets.all(12),
      constraints: BoxConstraints(
        maxHeight: MediaQuery.sizeOf(context).height * 0.42,
      ),
      decoration: BoxDecoration(
        color: cs.surface,
        borderRadius: BorderRadius.circular(20),
        boxShadow: const [
          BoxShadow(color: Colors.black38, blurRadius: 12, offset: Offset(0, 4)),
        ],
      ),
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 14, 16, 8),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(Icons.directions_walk, color: cs.primary),
                  const SizedBox(width: 8),
                  Text(
                    'Your crawl · ${stops.length} stops',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ],
              ),
              const SizedBox(height: 2),
              if (loading)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 6),
                  child: Row(
                    children: [
                      const SizedBox(
                        width: 14,
                        height: 14,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                      const SizedBox(width: 8),
                      Text(
                        'Routing…',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ],
                  ),
                )
              else
                Text(
                  tour?.totalLabel ?? 'Route unavailable — showing stops only',
                  style: Theme.of(
                    context,
                  ).textTheme.bodyMedium?.copyWith(color: cs.onSurfaceVariant),
                ),
              const Divider(height: 16),
              Flexible(
                child: ListView.builder(
                  shrinkWrap: true,
                  itemCount: stops.length,
                  itemBuilder: (context, i) {
                    final e = stops[i];
                    final legs = tour?.legSeconds ?? const [];
                    final showLeg = i > 0 && i - 1 < legs.length;
                    return Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (showLeg)
                          Padding(
                            padding: const EdgeInsets.only(left: 11, bottom: 4),
                            child: Row(
                              children: [
                                Icon(
                                  Icons.more_vert,
                                  size: 14,
                                  color: cs.outline,
                                ),
                                const SizedBox(width: 8),
                                Text(
                                  _legLabel(legs[i - 1]),
                                  style: Theme.of(context).textTheme.labelSmall
                                      ?.copyWith(color: cs.onSurfaceVariant),
                                ),
                              ],
                            ),
                          ),
                        Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            CircleAvatar(
                              radius: 12,
                              backgroundColor: cs.primary,
                              child: Text(
                                '${i + 1}',
                                style: TextStyle(
                                  color: cs.onPrimary,
                                  fontSize: 12,
                                  fontWeight: FontWeight.w600,
                                ),
                              ),
                            ),
                            const SizedBox(width: 10),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    e.title,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: const TextStyle(
                                      fontWeight: FontWeight.w600,
                                    ),
                                  ),
                                  Text(
                                    '${fmt.format(e.startsAt.toLocal())}'
                                    '${e.venue.name.isNotEmpty ? ' • ${e.venue.name}' : ''}',
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: Theme.of(context).textTheme.bodySmall
                                        ?.copyWith(color: cs.onSurfaceVariant),
                                  ),
                                ],
                              ),
                            ),
                          ],
                        ),
                        const SizedBox(height: 8),
                      ],
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
