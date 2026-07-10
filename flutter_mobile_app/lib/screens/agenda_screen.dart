import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';

import '../api/event_api.dart';
import '../models/event.dart';
import '../state/saved_events.dart';
import '../util/calendar.dart';
import 'crawl_screen.dart';

/// The user's bookmarked events. Reached from the bookmark icon in the feed's
/// top bar; backed by [savedEventsProvider] (persisted locally).
class AgendaScreen extends ConsumerWidget {
  const AgendaScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final saved = ref.watch(savedEventsProvider);
    // Chronological crawl over the saved events that have real coordinates.
    final crawlStops = [
      for (final e in saved)
        if (e.venue.lat != 0 && e.venue.lon != 0) e,
    ]..sort((a, b) => a.startsAt.compareTo(b.startsAt));
    return Scaffold(
      appBar: AppBar(
        title: Text(saved.isEmpty ? 'My agenda' : 'My agenda · ${saved.length}'),
        actions: [
          IconButton(
            tooltip: crawlStops.length < 2
                ? 'Save 2+ events with a location to plan a crawl'
                : 'Plan a walking crawl',
            icon: const Icon(Icons.directions_walk),
            onPressed: crawlStops.length < 2
                ? null
                : () => Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => CrawlScreen(stops: crawlStops),
                    ),
                  ),
          ),
        ],
      ),
      body: saved.isEmpty
          ? const _EmptyAgenda()
          : ListView.separated(
              padding: const EdgeInsets.all(16),
              itemCount: saved.length,
              separatorBuilder: (_, _) => const SizedBox(height: 12),
              itemBuilder: (_, i) => _AgendaTile(event: saved[i]),
            ),
    );
  }
}

class _EmptyAgenda extends StatelessWidget {
  const _EmptyAgenda();

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.bookmark_border, size: 56, color: cs.outline),
            const SizedBox(height: 12),
            Text(
              'No saved events yet',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 6),
            Text(
              'Tap the bookmark on any event to add it to your agenda.',
              textAlign: TextAlign.center,
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

class _AgendaTile extends ConsumerWidget {
  const _AgendaTile({required this.event});

  final Event event;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final cs = Theme.of(context).colorScheme;
    final fmt = DateFormat.MMMEd().add_jm();
    return Card(
      clipBehavior: Clip.antiAlias,
      margin: EdgeInsets.zero,
      child: InkWell(
        onTap: () => context.push('/event/${event.id}'),
        child: IntrinsicHeight(
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              SizedBox(
                width: 96,
                child: event.imageUrl.isNotEmpty
                    ? CachedNetworkImage(
                        imageUrl: proxiedImage(event.imageUrl),
                        fit: BoxFit.cover,
                        memCacheWidth: 240,
                        errorWidget: (_, _, _) =>
                            Container(color: cs.surfaceContainerHighest),
                        placeholder: (_, _) =>
                            Container(color: cs.surfaceContainerHighest),
                      )
                    : Container(
                        color: cs.surfaceContainerHighest,
                        child: Icon(Icons.event, color: cs.outline),
                      ),
              ),
              Expanded(
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(12, 10, 4, 10),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        event.title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 6),
                      Text(
                        fmt.format(event.startsAt.toLocal()),
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: cs.primary,
                        ),
                      ),
                      Text(
                        event.venue.name.isNotEmpty
                            ? '${event.venue.name} • ${event.city}'
                            : event.city,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(
                          context,
                        ).textTheme.bodySmall?.copyWith(color: cs.onSurfaceVariant),
                      ),
                    ],
                  ),
                ),
              ),
              Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  IconButton(
                    tooltip: 'Add to calendar',
                    visualDensity: VisualDensity.compact,
                    icon: const Icon(Icons.event_available_outlined),
                    onPressed: () => addEventToCalendar(event),
                  ),
                  IconButton(
                    tooltip: 'Remove from agenda',
                    visualDensity: VisualDensity.compact,
                    icon: Icon(Icons.bookmark_remove_outlined, color: cs.error),
                    onPressed: () =>
                        ref.read(savedEventsProvider.notifier).remove(event.id),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
