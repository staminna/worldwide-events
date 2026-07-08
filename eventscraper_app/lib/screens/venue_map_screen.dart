import 'package:flutter/material.dart';
import 'package:maplibre_gl/maplibre_gl.dart';

import '../models/event.dart';
import '../util/geo.dart';
import '../util/map_pin.dart';
import '../widgets/category_style.dart';
import '../widgets/directions_buttons.dart';

/// Full-screen interactive map for a single event's venue, opened by tapping
/// the preview map on the detail screen. The venue is drawn as a teardrop pin
/// (a generated symbol so it tracks the map while panning).
class VenueMapScreen extends StatefulWidget {
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
  State<VenueMapScreen> createState() => _VenueMapScreenState();
}

class _VenueMapScreenState extends State<VenueMapScreen> {
  MapLibreMapController? _controller;

  Future<void> _onStyleLoaded() async {
    final c = _controller;
    if (c == null || !mounted) return;
    final pin = await renderMapPin(
      categoryColor(Theme.of(context).colorScheme, widget.event.category),
      dpr: MediaQuery.devicePixelRatioOf(context),
    );
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
  }

  @override
  Widget build(BuildContext context) {
    final isVenueLevel =
        widget.approxLabel == null &&
        widget.event.venue.lat != 0 &&
        widget.event.venue.lon != 0;
    return Scaffold(
      body: Stack(
        children: [
          Positioned.fill(
            child: MapLibreMap(
              styleString: mapStyleUrl,
              initialCameraPosition: CameraPosition(
                target: widget.center,
                zoom: isVenueLevel ? 15.5 : 11.5,
              ),
              minMaxZoomPreference: const MinMaxZoomPreference(2, 19),
              compassEnabled: false,
              onMapCreated: (c) => _controller = c,
              onStyleLoadedCallback: _onStyleLoaded,
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
          if (isVenueLevel)
            Positioned(
              left: 12,
              right: 12,
              bottom: 12,
              child: SafeArea(
                top: false,
                child: Material(
                  elevation: 4,
                  borderRadius: BorderRadius.circular(16),
                  color: Theme.of(context).colorScheme.surface,
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: DirectionsButtons(
                      lat: widget.event.venue.lat,
                      lon: widget.event.venue.lon,
                      label: widget.event.venue.name.isEmpty
                          ? widget.event.title
                          : widget.event.venue.name,
                    ),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}
