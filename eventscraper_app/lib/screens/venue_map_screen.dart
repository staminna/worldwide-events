import 'package:flutter/material.dart';
import 'package:intl/intl.dart';
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
              child: Material(
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
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
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
            ),
          ),
          ],
        ),
      ),
    );
  }
}
