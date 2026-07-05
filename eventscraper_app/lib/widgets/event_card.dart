import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../api/event_api.dart';
import '../models/event.dart';
import 'category_style.dart';

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
              child: event.imageUrl.isNotEmpty
                  ? CachedNetworkImage(
                      imageUrl: proxiedImage(event.imageUrl),
                      fit: BoxFit.cover,
                      errorWidget: (_, __, ___) => _placeholder(cs),
                      placeholder: (_, __) =>
                          Container(color: cs.surfaceContainerHighest),
                    )
                  : _placeholder(cs),
            ),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.all(14),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Row(
                      children: [
                        _CategoryChip(category: event.category),
                        const SizedBox(width: 6),
                        _SourceChip(source: event.source),
                      ],
                    ),
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

class _SourceChip extends StatelessWidget {
  const _SourceChip({required this.source});
  final EventSource source;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
      decoration: BoxDecoration(
        border: Border.all(color: cs.outlineVariant),
        borderRadius: BorderRadius.circular(5),
      ),
      child: Text(
        sourceLabel(source),
        style: TextStyle(color: cs.onSurfaceVariant, fontSize: 11),
      ),
    );
  }
}
