import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:flutter_map_marker_cluster/flutter_map_marker_cluster.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';
import 'package:latlong2/latlong.dart';

import '../models/event.dart';
import '../state/providers.dart';

class MapScreen extends ConsumerStatefulWidget {
  const MapScreen({super.key});

  @override
  ConsumerState<MapScreen> createState() => _MapScreenState();
}

class _MapScreenState extends ConsumerState<MapScreen> {
  final MapController _controller = MapController();
  Event? _selected;

  @override
  Widget build(BuildContext context) {
    final eventsAsync = ref.watch(eventsProvider);

    return eventsAsync.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (e, _) => Center(child: Text('Error: $e')),
      data: (list) {
        final placed = list.events
            .where((e) => e.venue.lat != 0 && e.venue.lon != 0)
            .toList();
        return Stack(
          children: [
            FlutterMap(
              mapController: _controller,
              options: MapOptions(
                initialCenter: _initialCenter(placed),
                initialZoom: placed.length > 50 ? 2.5 : 4,
                minZoom: 1,
                maxZoom: 18,
                onTap: (_, __) => setState(() => _selected = null),
              ),
              children: [
                TileLayer(
                  urlTemplate:
                      'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
                  userAgentPackageName: 'com.jorgenunes.eventscraper_app',
                  maxNativeZoom: 19,
                ),
                MarkerClusterLayerWidget(
                  options: MarkerClusterLayerOptions(
                    maxClusterRadius: 60,
                    size: const Size(46, 46),
                    alignment: Alignment.center,
                    padding: const EdgeInsets.all(40),
                    markers: [
                      for (final e in placed)
                        Marker(
                          point: LatLng(e.venue.lat, e.venue.lon),
                          width: 46,
                          height: 52,
                          // Anchor the teardrop's tip on the coordinate: with
                          // topCenter the whole marker sits above the point, so
                          // its bottom tip lands exactly on the location.
                          alignment: Alignment.topCenter,
                          child: _MapMarker(
                            event: e,
                            selected: _selected?.id == e.id,
                            onTap: () => setState(() => _selected = e),
                          ),
                        ),
                    ],
                    builder: (context, markers) =>
                        _ClusterBubble(count: markers.length),
                  ),
                ),
                const RichAttributionWidget(
                  attributions: [
                    TextSourceAttribution(
                      '© OpenStreetMap contributors',
                    ),
                  ],
                ),
              ],
            ),
            Positioned(
              top: 12,
              left: 12,
              right: 12,
              child: _MapTopBar(count: placed.length, total: list.events.length),
            ),
            if (_selected != null)
              Positioned(
                left: 12,
                right: 12,
                bottom: 12,
                child: _SelectedEventCard(
                  event: _selected!,
                  onClose: () => setState(() => _selected = null),
                  onOpen: () => context.push('/event/${_selected!.id}'),
                ),
              ),
          ],
        );
      },
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
}

class _MapMarker extends StatelessWidget {
  const _MapMarker({
    required this.event,
    required this.selected,
    required this.onTap,
  });

  final Event event;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final color = switch (event.category) {
      EventCategory.tech => cs.primary,
      EventCategory.music => cs.secondary,
      EventCategory.business => cs.tertiary,
      _ => cs.outline,
    };
    final glyph = switch (event.category) {
      EventCategory.tech => Icons.code,
      EventCategory.music => Icons.music_note,
      EventCategory.business => Icons.business_center,
      _ => Icons.place,
    };
    final scale = selected ? 1.18 : 1.0;
    // A modern teardrop pin: a colored map-pin glyph with a white outline and
    // a recessed white disc holding the category icon. Bottom-aligned so the
    // tip sits at the very bottom of the box (the Marker anchors that to the
    // coordinate).
    return GestureDetector(
      onTap: onTap,
      child: AnimatedScale(
        scale: scale,
        duration: const Duration(milliseconds: 150),
        curve: Curves.easeOutBack,
        child: SizedBox(
          width: 46,
          height: 52,
          child: Stack(
            clipBehavior: Clip.none,
            alignment: Alignment.bottomCenter,
            children: [
              // White halo behind, giving the pin a crisp edge on busy maps.
              const Align(
                alignment: Alignment.bottomCenter,
                child: Icon(Icons.location_on, size: 46, color: Colors.white),
              ),
              // The colored pin body with a soft drop shadow.
              Align(
                alignment: Alignment.bottomCenter,
                child: Icon(
                  Icons.location_on,
                  size: 42,
                  color: color,
                  shadows: const [
                    Shadow(
                      color: Color(0x59000000),
                      blurRadius: 5,
                      offset: Offset(0, 3),
                    ),
                  ],
                ),
              ),
              // Category glyph in a white disc set into the pin's head.
              Align(
                alignment: const Alignment(0, -0.34),
                child: Container(
                  width: 20,
                  height: 20,
                  alignment: Alignment.center,
                  decoration: const BoxDecoration(
                    shape: BoxShape.circle,
                    color: Colors.white,
                  ),
                  child: Icon(glyph, size: 12, color: color),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ClusterBubble extends StatelessWidget {
  const _ClusterBubble({required this.count});
  final int count;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    // Tier the bubble size + color so dense clusters read at a glance.
    final tier = count >= 100 ? 2 : (count >= 10 ? 1 : 0);
    final size = [40.0, 46.0, 56.0][tier];
    final color = [cs.primary, cs.secondary, cs.tertiary][tier];
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        color: color.withValues(alpha: 0.92),
        border: Border.all(color: Colors.white, width: 3),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.3),
            blurRadius: 8,
            offset: const Offset(0, 3),
          ),
        ],
      ),
      child: Text(
        '$count',
        style: TextStyle(
          color: Colors.white,
          fontWeight: FontWeight.w700,
          fontSize: tier == 2 ? 16 : 14,
        ),
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
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: cs.onSurfaceVariant,
                    ),
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
    required this.onClose,
    required this.onOpen,
  });

  final Event event;
  final VoidCallback onClose;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final fmt = DateFormat.MMMd().add_jm();
    return Card(
      elevation: 6,
      child: ListTile(
        title: Text(event.title,
            maxLines: 2, overflow: TextOverflow.ellipsis),
        subtitle: Text(
          '${fmt.format(event.startsAt.toLocal())} • ${event.city}',
        ),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            IconButton(
              icon: const Icon(Icons.open_in_new),
              onPressed: onOpen,
            ),
            IconButton(
              icon: const Icon(Icons.close),
              onPressed: onClose,
            ),
          ],
        ),
      ),
    );
  }
}
