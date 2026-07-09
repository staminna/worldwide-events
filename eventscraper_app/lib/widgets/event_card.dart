import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:intl/intl.dart';

import '../api/event_api.dart';
import '../models/event.dart';
import '../state/poster_color.dart';
import 'category_style.dart';
import 'save_button.dart';

class EventCard extends StatelessWidget {
  const EventCard({super.key, required this.event, required this.onTap});

  final Event event;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final fmt = DateFormat.MMMd().add_jm();
    return Card(
      clipBehavior: Clip.antiAlias,
      margin: EdgeInsets.zero,
      child: InkWell(
        onTap: onTap,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          mainAxisSize: MainAxisSize.min,
          children: [
            AspectRatio(
              aspectRatio: 16 / 9,
              child: Stack(
                fit: StackFit.expand,
                children: [
                  event.imageUrl.isNotEmpty
                      // Decode the thumbnail at its displayed physical size
                      // rather than the source's full resolution — hi-res
                      // images otherwise decode into memory at native size,
                      // causing scroll jank and memory pressure across a grid.
                      ? LayoutBuilder(
                          builder: (context, constraints) {
                            final cacheWidth =
                                (constraints.maxWidth *
                                        MediaQuery.devicePixelRatioOf(context))
                                    .round();
                            return CachedNetworkImage(
                              imageUrl: proxiedImage(event.imageUrl),
                              fit: BoxFit.cover,
                              memCacheWidth: cacheWidth,
                              errorWidget: (_, _, _) => _placeholder(cs),
                              placeholder: (_, _) =>
                                  Container(color: cs.surfaceContainerHighest),
                            );
                          },
                        )
                      : _placeholder(cs),
                  Positioned(
                    top: 2,
                    right: 2,
                    child: SaveButton(event: event, onImagery: true),
                  ),
                ],
              ),
            ),
            // Thin accent tying the card to its poster's dominant color;
            // falls back to the category color while the palette resolves.
            Consumer(
              builder: (context, ref, _) {
                final color =
                    ref.watch(posterColorProvider(event.imageUrl)).valueOrNull ??
                    categoryColor(cs, event.category);
                return Container(height: 3, color: color);
              },
            ),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.all(14),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Row(children: [_CategoryChip(category: event.category)]),
                    const SizedBox(height: 10),
                    Text(
                      event.title,
                      style: Theme.of(context).textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w600,
                        height: 1.2,
                      ),
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                    const SizedBox(height: 8),
                    Row(
                      children: [
                        Icon(Icons.schedule, size: 14, color: cs.outline),
                        const SizedBox(width: 4),
                        Expanded(
                          child: Text(
                            fmt.format(event.startsAt.toLocal()),
                            style: Theme.of(context).textTheme.bodySmall,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 4),
                    Row(
                      children: [
                        Icon(Icons.place_outlined, size: 14, color: cs.outline),
                        const SizedBox(width: 4),
                        Expanded(
                          child: Text(
                            event.venue.name.isNotEmpty
                                ? '${event.venue.name} • ${event.city}'
                                : event.city,
                            style: Theme.of(context).textTheme.bodySmall,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                      ],
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

  Widget _placeholder(ColorScheme cs) => Container(
    color: cs.surfaceContainerHighest,
    child: Icon(Icons.event, size: 40, color: cs.outline),
  );
}

class _CategoryChip extends StatelessWidget {
  const _CategoryChip({required this.category});
  final EventCategory category;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final color = categoryColor(cs, category);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(5),
      ),
      child: Text(
        categoryLabel(category),
        style: TextStyle(
          color: color,
          fontWeight: FontWeight.w600,
          fontSize: 11,
        ),
      ),
    );
  }
}
