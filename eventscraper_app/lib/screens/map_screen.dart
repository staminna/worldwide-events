import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
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
                MarkerLayer(
                  markers: [
                    for (final e in placed)
                      Marker(
                        point: LatLng(e.venue.lat, e.venue.lon),
                        width: 38,
                        height: 38,
                        child: _MapMarker(
                          event: e,
                          selected: _selected?.id == e.id,
                          onTap: () => setState(() => _selected = e),
                        ),
                      ),
                  ],
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
    final scale = selected ? 1.15 : 1.0;
    return GestureDetector(
      onTap: onTap,
      child: AnimatedScale(
        scale: scale,
        duration: const Duration(milliseconds: 120),
        child: Container(
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            color: color,
            border: Border.all(color: Colors.white, width: 2.5),
            boxShadow: [
              BoxShadow(
                color: Colors.black.withValues(alpha: 0.35),
                blurRadius: 6,
                offset: const Offset(0, 2),
              ),
            ],
          ),
          child: Icon(
            switch (event.category) {
              EventCategory.tech => Icons.code,
              EventCategory.music => Icons.music_note,
              EventCategory.business => Icons.business_center,
              _ => Icons.place,
            },
            color: Colors.white,
            size: 18,
          ),
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
