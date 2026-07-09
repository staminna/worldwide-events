import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:intl/intl.dart';
import 'package:maplibre_gl/maplibre_gl.dart';
import 'package:url_launcher/url_launcher.dart';

import '../api/event_api.dart';
import '../models/event.dart';
import '../state/poster_color.dart';
import '../state/providers.dart';
import '../util/calendar.dart';
import '../util/geo.dart';
import '../widgets/category_style.dart';
import '../widgets/directions_buttons.dart';
import '../widgets/save_button.dart';
import 'image_viewer.dart';
import 'venue_map_screen.dart';

class EventDetailScreen extends ConsumerWidget {
  const EventDetailScreen({super.key, required this.eventId});

  final String eventId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final eventAsync = ref.watch(eventByIdProvider(eventId));
    return Scaffold(
      body: eventAsync.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Error: $e')),
        data: (event) => _EventDetailView(event: event),
      ),
    );
  }
}

class _EventDetailView extends ConsumerWidget {
  const _EventDetailView({required this.event});

  final Event event;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final citiesAsync = ref.watch(citiesProvider);
    final mapPoint = _resolveMapPoint(event, citiesAsync.valueOrNull);
    final isVenueLevel = event.venue.lat != 0 && event.venue.lon != 0;

    return Column(
      children: [
        _HeroHeader(event: event),
        Expanded(
          child: LayoutBuilder(
            builder: (context, constraints) {
              final wide = constraints.maxWidth >= 900;
              final details = _DetailsPane(event: event);
              final map = mapPoint == null
                  ? const _NoCoordsPane()
                  : _DetailMap(
                      center: mapPoint,
                      event: event,
                      label: isVenueLevel
                          ? null
                          : '${event.city}, ${event.country}',
                      zoom: isVenueLevel ? 15 : 11,
                    );
              if (wide) {
                return Row(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    SizedBox(width: 420, child: details),
                    Expanded(child: map),
                  ],
                );
              }
              return Column(
                children: [
                  details,
                  Expanded(child: map),
                ],
              );
            },
          ),
        ),
        SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                // Only when we have real venue coordinates — navigating to a
                // city centroid ("Approximate" cases) would mislead.
                if (isVenueLevel) ...[
                  DirectionsButtons(
                    lat: event.venue.lat,
                    lon: event.venue.lon,
                    label: event.venue.name.isEmpty
                        ? event.title
                        : event.venue.name,
                  ),
                  const SizedBox(height: 10),
                ],
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton.icon(
                        icon: const Icon(Icons.event_available_outlined),
                        label: const Text('Add to calendar'),
                        onPressed: () => addEventToCalendar(event),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: FilledButton.icon(
                        icon: const Icon(Icons.open_in_new),
                        label: const Text('Source site'),
                        onPressed: () async {
                          final uri = Uri.tryParse(event.url);
                          if (uri != null) {
                            await launchUrl(
                              uri,
                              mode: LaunchMode.externalApplication,
                            );
                          }
                        },
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }

  LatLng? _resolveMapPoint(Event event, List<City>? cities) {
    if (event.venue.lat != 0 && event.venue.lon != 0) {
      return LatLng(event.venue.lat, event.venue.lon);
    }
    if (cities == null) return null;
    final name = event.city.trim().toLowerCase();
    if (name.isEmpty) return null;
    for (final c in cities) {
      if (c.name.toLowerCase() == name) {
        return LatLng(c.lat, c.lon);
      }
    }
    return null;
  }
}

class _HeroHeader extends StatelessWidget {
  const _HeroHeader({required this.event});

  final Event event;

  @override
  Widget build(BuildContext context) {
    final hasImage = event.imageUrl.isNotEmpty;
    return Stack(
      children: [
        AspectRatio(
          aspectRatio: 21 / 9,
          child: hasImage
              ? Hero(
                  tag: 'event-image-${event.id}',
                  child: GestureDetector(
                    onTap: () => Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => ImageViewerScreen(
                          url: event.imageUrl,
                          heroTag: 'event-image-${event.id}',
                        ),
                      ),
                    ),
                    child: CachedNetworkImage(
                      imageUrl: proxiedImage(hiResImage(event.imageUrl)),
                      fit: BoxFit.cover,
                      // Fall back to the small thumbnail the feed already
                      // displays if the hi-res variant 404s on this CDN.
                      errorWidget: (_, __, ___) => CachedNetworkImage(
                        imageUrl: proxiedImage(event.imageUrl),
                        fit: BoxFit.cover,
                        errorWidget: (_, __, ___) => Container(
                          color: Theme.of(
                            context,
                          ).colorScheme.surfaceContainerHighest,
                        ),
                      ),
                    ),
                  ),
                )
              : Container(
                  color: Theme.of(context).colorScheme.surfaceContainerHighest,
                  child: const Icon(Icons.event, size: 56),
                ),
        ),
        // Subtle gradient so the back button stays legible.
        Positioned.fill(
          child: IgnorePointer(
            child: DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.center,
                  colors: [
                    Colors.black.withValues(alpha: 0.45),
                    Colors.transparent,
                  ],
                ),
              ),
            ),
          ),
        ),
        SafeArea(
          bottom: false,
          child: Padding(
            padding: const EdgeInsets.all(8),
            child: Row(
              children: [
                Material(
                  color: Colors.black.withValues(alpha: 0.35),
                  shape: const CircleBorder(),
                  child: IconButton(
                    color: Colors.white,
                    icon: const Icon(Icons.arrow_back),
                    onPressed: () => Navigator.of(context).maybePop(),
                  ),
                ),
                const Spacer(),
                SaveButton(event: event, onImagery: true),
              ],
            ),
          ),
        ),
        if (hasImage)
          Positioned(
            right: 12,
            bottom: 12,
            child: Material(
              color: Colors.black.withValues(alpha: 0.45),
              shape: const StadiumBorder(),
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 10,
                  vertical: 6,
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: const [
                    Icon(Icons.zoom_out_map, size: 14, color: Colors.white),
                    SizedBox(width: 4),
                    Text(
                      'Tap to expand',
                      style: TextStyle(color: Colors.white, fontSize: 11),
                    ),
                  ],
                ),
              ),
            ),
          ),
        // Poster-colored accent at the base of the hero, tying the header to
        // the artwork; falls back to the category color.
        Positioned(
          left: 0,
          right: 0,
          bottom: 0,
          child: Consumer(
            builder: (context, ref, _) {
              final cs = Theme.of(context).colorScheme;
              final color =
                  ref.watch(posterColorProvider(event.imageUrl)).valueOrNull ??
                  categoryColor(cs, event.category);
              return Container(height: 5, color: color);
            },
          ),
        ),
      ],
    );
  }
}

class _DetailsPane extends ConsumerWidget {
  const _DetailsPane({required this.event});

  final Event event;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final cs = Theme.of(context).colorScheme;
    // Venues scraped without an address get one lazily: the backend
    // reverse-geocodes the coordinates (cached, and persisted back into the
    // event). Silent while loading or on failure.
    var address = event.venue.address;
    if (address.isEmpty && event.venue.lat != 0 && event.venue.lon != 0) {
      address = ref
              .watch(
                venueAddressProvider((
                  lat: event.venue.lat,
                  lon: event.venue.lon,
                  id: event.id,
                )),
              )
              .valueOrNull ??
          '';
    }
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(20, 16, 20, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            event.title,
            style: Theme.of(
              context,
            ).textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              Chip(label: Text(categoryLabel(event.category))),
              if (event.price != null)
                Chip(
                  label: Text(
                    event.price!.free
                        ? 'Free'
                        : '${event.price!.min.toStringAsFixed(0)}–${event.price!.max.toStringAsFixed(0)} ${event.price!.currency}',
                  ),
                ),
            ],
          ),
          const SizedBox(height: 16),
          _IconRow(
            icon: Icons.schedule,
            text: DateFormat.yMMMMEEEEd().add_jm().format(
              event.startsAt.toLocal(),
            ),
          ),
          const SizedBox(height: 8),
          _IconRow(
            icon: Icons.place_outlined,
            text: [
              event.venue.name,
              address,
              '${event.city}, ${event.country}',
            ].where((s) => s.isNotEmpty).join(' • '),
          ),
          if (event.description.isNotEmpty) ...[
            const SizedBox(height: 20),
            Text(
              event.description,
              style: Theme.of(context).textTheme.bodyLarge?.copyWith(
                color: cs.onSurface,
                height: 1.45,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _IconRow extends StatelessWidget {
  const _IconRow({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(
          icon,
          size: 20,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Text(text, style: Theme.of(context).textTheme.bodyLarge),
        ),
      ],
    );
  }
}

/// A static, non-interactive map preview with a teardrop pin on the venue.
/// Tapping anywhere opens the full-screen interactive [VenueMapScreen].
class _DetailMap extends StatefulWidget {
  const _DetailMap({
    required this.center,
    required this.zoom,
    required this.event,
    this.label,
  });

  final LatLng center;
  final double zoom;
  final Event event;
  final String? label;

  @override
  State<_DetailMap> createState() => _DetailMapState();
}

class _DetailMapState extends State<_DetailMap> {
  // While the full-screen map is open we drop this preview's live map: two
  // simultaneous MapLibre GL surfaces corrupt each other's sizing (the second
  // renders as a thin strip). One map on screen at a time.
  bool _expanded = false;

  Future<void> _openFullscreen() async {
    setState(() => _expanded = true);
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => VenueMapScreen(
          event: widget.event,
          center: widget.center,
          approxLabel: widget.label,
        ),
      ),
    );
    if (mounted) setState(() => _expanded = false);
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(20),
        child: Stack(
          children: [
            Positioned.fill(
              child: _expanded
                  ? Container(color: cs.surfaceContainerHighest)
                  : MapLibreMap(
                      styleString: mapStyleUrl,
                      initialCameraPosition: CameraPosition(
                        target: widget.center,
                        zoom: widget.zoom,
                      ),
                      // Static preview — the full-screen map handles interaction.
                      scrollGesturesEnabled: false,
                      zoomGesturesEnabled: false,
                      rotateGesturesEnabled: false,
                      tiltGesturesEnabled: false,
                      dragEnabled: false,
                      compassEnabled: false,
                    ),
            ),
            // Teardrop pin, tip anchored on the venue at map centre.
            IgnorePointer(
              child: Center(
                child: Transform.translate(
                  offset: const Offset(0, -21),
                  child: Icon(
                    Icons.location_on,
                    size: 46,
                    color: categoryColor(cs, widget.event.category),
                    shadows: const [
                      Shadow(
                        color: Colors.black54,
                        blurRadius: 6,
                        offset: Offset(0, 2),
                      ),
                    ],
                  ),
                ),
              ),
            ),
            if (widget.label != null)
              Positioned(
                left: 12,
                top: 12,
                child: _MapChip(
                  icon: Icons.location_city,
                  text: 'Approximate • ${widget.label}',
                ),
              ),
            const Positioned(
              right: 12,
              bottom: 12,
              child: _MapChip(icon: Icons.zoom_out_map, text: 'Tap to expand'),
            ),
            // Whole preview is one big tap target → open the full map.
            Positioned.fill(
              child: GestureDetector(
                behavior: HitTestBehavior.opaque,
                onTap: _expanded ? null : _openFullscreen,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _MapChip extends StatelessWidget {
  const _MapChip({required this.icon, required this.text});
  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.black.withValues(alpha: 0.55),
      shape: const StadiumBorder(),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 14, color: Colors.white),
            const SizedBox(width: 6),
            Text(
              text,
              style: const TextStyle(color: Colors.white, fontSize: 12),
            ),
          ],
        ),
      ),
    );
  }
}

class _NoCoordsPane extends StatelessWidget {
  const _NoCoordsPane();

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.location_off, size: 40, color: cs.onSurfaceVariant),
            const SizedBox(height: 12),
            Text(
              'No location coordinates for this event.',
              style: Theme.of(
                context,
              ).textTheme.bodyMedium?.copyWith(color: cs.onSurfaceVariant),
            ),
          ],
        ),
      ),
    );
  }
}
