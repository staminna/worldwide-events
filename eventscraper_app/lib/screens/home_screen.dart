import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';

import '../models/event.dart';
import '../state/location.dart';
import '../state/providers.dart';
import '../state/saved_events.dart';
import '../util/geo.dart';
import '../util/nl_query.dart';
import '../widgets/event_card.dart';
import 'filters_sheet.dart';

class HomeScreen extends ConsumerStatefulWidget {
  const HomeScreen({super.key});

  @override
  ConsumerState<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends ConsumerState<HomeScreen> {
  Timer? _pollTimer;
  Timer? _searchDebounce;
  late final TextEditingController _searchController;

  @override
  void initState() {
    super.initState();
    _searchController = TextEditingController(
      text: ref.read(filtersProvider).search,
    );
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    _searchDebounce?.cancel();
    _searchController.dispose();
    super.dispose();
  }

  void _schedulePoll() {
    _pollTimer?.cancel();
    _pollTimer = Timer(const Duration(seconds: 8), () {
      if (!mounted) return;
      ref.read(eventFeedProvider.notifier).refresh();
    });
  }

  void _cancelPoll() {
    _pollTimer?.cancel();
    _pollTimer = null;
  }

  void _onSearchChanged(String v) {
    _searchDebounce?.cancel();
    _searchDebounce = Timer(const Duration(milliseconds: 350), () {
      if (!mounted) return;
      ref.read(filtersProvider.notifier).setSearch(v);
    });
  }

  /// Interprets the search text with the keyword heuristics in [parseQuery]
  /// and applies the recognised filters. Falls back to plain text search when
  /// nothing structured is found.
  void _onSearchSubmitted(String v) {
    _searchDebounce?.cancel();
    final cities = ref.read(citiesProvider).valueOrNull ?? const [];
    final parsed = parseQuery(v, cities);
    if (!parsed.matchedAnything) {
      ref.read(filtersProvider.notifier).setSearch(v);
      return;
    }
    final fn = ref.read(filtersProvider.notifier);
    final qn = ref.read(quickFiltersProvider.notifier);
    if (parsed.category != null) fn.setCategory(parsed.category);
    if (parsed.cityId != null) fn.setCity(parsed.cityId);
    if (parsed.weekend) {
      final w = upcomingWeekend();
      fn.setRange(w.from, w.to);
    }
    qn.setFree(parsed.free);
    qn.setTonight(parsed.tonight);
    fn.setSearch(parsed.residual);
    // Keep the box in sync with what actually drives the server search.
    if (_searchController.text != parsed.residual) {
      _searchController.text = parsed.residual;
    }
    if (parsed.nearMe) _enableNearMe();
    ScaffoldMessenger.of(context)
      ..clearSnackBars()
      ..showSnackBar(
        SnackBar(
          duration: const Duration(seconds: 3),
          content: Text('Applied: ${parsed.summary}'),
        ),
      );
  }

  Future<void> _enableNearMe() async {
    try {
      if (!ref.read(locationProvider).hasFix) {
        await ref.read(locationProvider.notifier).refreshFix();
      }
      if (!mounted) return;
      ref.read(quickFiltersProvider.notifier).setNearMe(true);
    } catch (_) {
      // Best-effort — leave near-me off if we can't get a fix.
    }
  }

  Future<void> _onNearMe() async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      final nearest = await ref
          .read(locationProvider.notifier)
          .locate(minEvents: 3);
      ref.read(filtersProvider.notifier).setCity(nearest.city.id);
      final km = nearest.distanceKm.round();
      messenger.showSnackBar(
        SnackBar(
          content: Text(
            km <= 30
                ? 'Showing events in ${nearest.city.name}'
                : 'Showing events in ${nearest.city.name} — '
                      'the closest covered city, $km km away',
          ),
        ),
      );
    } catch (e) {
      messenger.showSnackBar(
        SnackBar(
          content: Text(
            e is LocationException ? e.message : 'Location lookup failed: $e',
          ),
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final feed = ref.watch(eventFeedProvider);
    final filters = ref.watch(filtersProvider);

    if (!feed.loading && feed.error == null && feed.events.isEmpty) {
      _schedulePoll();
    } else {
      _cancelPoll();
    }

    return Scaffold(
      appBar: AppBar(
        titleSpacing: 8,
        title: _SearchField(
          controller: _searchController,
          onChanged: _onSearchChanged,
          onSubmitted: _onSearchSubmitted,
          onClear: () {
            _searchController.clear();
            _onSearchChanged('');
          },
        ),
        actions: [
          Consumer(
            builder: (context, ref, _) {
              final locating = ref.watch(
                locationProvider.select((s) => s.locating),
              );
              return IconButton(
                tooltip: 'Events near me',
                onPressed: locating ? null : _onNearMe,
                icon: locating
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.my_location),
              );
            },
          ),
          Consumer(
            builder: (context, ref, _) {
              final count = ref.watch(savedEventsProvider).length;
              return IconButton(
                tooltip: 'My agenda',
                icon: Badge(
                  isLabelVisible: count > 0,
                  label: Text('$count'),
                  child: const Icon(Icons.bookmark_border),
                ),
                onPressed: () => context.push('/agenda'),
              );
            },
          ),
          IconButton(
            tooltip: 'Filters',
            icon: Badge(
              isLabelVisible: _activeFilterCount(filters) > 0,
              label: Text('${_activeFilterCount(filters)}'),
              child: const Icon(Icons.tune),
            ),
            onPressed: () => showModalBottomSheet(
              context: context,
              isScrollControlled: true,
              showDragHandle: false,
              useSafeArea: true,
              builder: (_) => const FiltersSheet(),
            ),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () => ref.read(eventFeedProvider.notifier).refresh(),
        child: _buildBody(feed),
      ),
      floatingActionButton: FloatingActionButton.extended(
        heroTag: 'add-event',
        onPressed: () => context.push('/add'),
        icon: const Icon(Icons.add),
        label: const Text('Add event'),
      ),
    );
  }

  Widget _buildBody(EventFeed feed) {
    if (feed.loading && feed.events.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }
    if (feed.error != null && feed.events.isEmpty) {
      return ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        children: [
          const SizedBox(height: 80),
          Center(
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                children: [
                  const Icon(Icons.cloud_off, size: 56),
                  const SizedBox(height: 12),
                  Text(
                    'Backend unreachable.\n${feed.error}',
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 12),
                  FilledButton(
                    onPressed: () =>
                        ref.read(eventFeedProvider.notifier).refresh(),
                    child: const Text('Retry'),
                  ),
                ],
              ),
            ),
          ),
        ],
      );
    }
    if (feed.events.isEmpty) {
      return const _BuildingFeedView();
    }
    final quick = ref.watch(quickFiltersProvider);
    final loc = ref.watch(locationProvider);
    final displayed = _applyQuickFilters(feed.events, quick, loc);
    return Column(
      children: [
        const _QuickFilterBar(),
        _SortHeader(
          shown: displayed.length,
          count: feed.total,
          nearMe: quick.nearMe && loc.hasFix,
        ),
        Expanded(
          child: displayed.isEmpty
              ? _NoQuickMatches(hasMore: feed.hasMore)
              : NotificationListener<ScrollNotification>(
                  onNotification: (n) {
                    if (n.metrics.pixels >= n.metrics.maxScrollExtent - 600) {
                      ref.read(eventFeedProvider.notifier).loadMore();
                    }
                    return false;
                  },
                  child: _EventGrid(
                    events: displayed,
                    onTap: (id) => context.push('/event/$id'),
                  ),
                ),
        ),
        if (feed.loadingMore)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 8),
            child: SizedBox(
              width: 28,
              height: 28,
              child: CircularProgressIndicator(strokeWidth: 3),
            ),
          ),
      ],
    );
  }

  /// Applies the client-side quick filters over the already-loaded pages:
  /// "free" narrows to free events, "near me" keeps events within the radius
  /// and re-sorts them by distance (nearest first).
  List<Event> _applyQuickFilters(
    List<Event> events,
    QuickFilters q,
    LocationState loc,
  ) {
    var list = events;
    if (q.tonight) {
      final now = DateTime.now();
      final endOfDay = DateTime(now.year, now.month, now.day, 23, 59, 59);
      list = list.where((e) {
        final t = e.startsAt.toLocal();
        // Still-relevant events happening for the rest of today.
        return t.isAfter(now.subtract(const Duration(hours: 2))) &&
            t.isBefore(endOfDay);
      }).toList();
    }
    if (q.freeOnly) {
      list = list.where((e) => e.price?.free ?? false).toList();
    }
    if (q.nearMe && loc.hasFix) {
      final scored = <(Event, double)>[];
      for (final e in list) {
        if (e.venue.lat == 0 && e.venue.lon == 0) continue;
        final d = haversineMeters(loc.lat!, loc.lon!, e.venue.lat, e.venue.lon);
        if (d <= q.radiusKm * 1000) scored.add((e, d));
      }
      scored.sort((a, b) => a.$2.compareTo(b.$2));
      list = [for (final s in scored) s.$1];
    }
    return list;
  }

  int _activeFilterCount(Filters f) {
    var n = 0;
    if (f.cityId != null) n++;
    if (f.category != null) n++;
    if (f.source != null) n++;
    if (f.from != null || f.to != null) n++;
    return n;
  }
}

class _SearchField extends StatelessWidget {
  const _SearchField({
    required this.controller,
    required this.onChanged,
    required this.onClear,
    required this.onSubmitted,
  });

  final TextEditingController controller;
  final ValueChanged<String> onChanged;
  final VoidCallback onClear;
  final ValueChanged<String> onSubmitted;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Container(
      height: 42,
      decoration: BoxDecoration(
        color: cs.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(22),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 12),
      child: Row(
        children: [
          Icon(Icons.search, size: 20, color: cs.onSurfaceVariant),
          const SizedBox(width: 8),
          Expanded(
            child: TextField(
              controller: controller,
              onChanged: onChanged,
              onSubmitted: onSubmitted,
              textInputAction: TextInputAction.search,
              decoration: const InputDecoration(
                hintText: 'Try “free jazz this weekend in Lisbon”',
                border: InputBorder.none,
                isDense: true,
              ),
            ),
          ),
          ValueListenableBuilder<TextEditingValue>(
            valueListenable: controller,
            builder: (_, value, __) => value.text.isEmpty
                ? const SizedBox.shrink()
                : IconButton(
                    iconSize: 18,
                    splashRadius: 18,
                    padding: EdgeInsets.zero,
                    constraints: const BoxConstraints(
                      minHeight: 24,
                      minWidth: 24,
                    ),
                    icon: const Icon(Icons.close),
                    onPressed: onClear,
                  ),
          ),
        ],
      ),
    );
  }
}

class _SortHeader extends StatelessWidget {
  const _SortHeader({
    required this.shown,
    required this.count,
    this.nearMe = false,
  });
  final int shown;
  final int count;
  final bool nearMe;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final fmt = NumberFormat.decimalPattern();
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 12, 20, 0),
      child: Row(
        children: [
          Flexible(
            child: Text(
              shown < count
                  ? '${fmt.format(shown)} of ${fmt.format(count)} events'
                  : '${fmt.format(count)} events',
              style: Theme.of(context).textTheme.titleSmall,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const Spacer(),
          Icon(
            nearMe ? Icons.near_me : Icons.swap_vert,
            size: 16,
            color: cs.onSurfaceVariant,
          ),
          const SizedBox(width: 4),
          Text(
            nearMe ? 'Nearest first' : 'Sorted by date • soonest first',
            style: Theme.of(
              context,
            ).textTheme.bodySmall?.copyWith(color: cs.onSurfaceVariant),
          ),
        ],
      ),
    );
  }
}

/// Horizontal quick-filter chips above the feed. "This weekend" sets the real
/// server date range; "Free" and "Near me" are client-side toggles.
class _QuickFilterBar extends ConsumerWidget {
  const _QuickFilterBar();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final quick = ref.watch(quickFiltersProvider);
    final filters = ref.watch(filtersProvider);
    final qn = ref.read(quickFiltersProvider.notifier);
    final weekend = upcomingWeekend();
    final weekendOn =
        _sameDate(filters.from, weekend.from) &&
        _sameDate(filters.to, weekend.to);
    final locating = ref.watch(
      locationProvider.select((s) => s.locating),
    );

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
          child: Row(
            children: [
              FilterChip(
                avatar: const Icon(Icons.nightlife, size: 18),
                label: const Text('Tonight'),
                selected: quick.tonight,
                onSelected: (_) => qn.toggleTonight(),
              ),
              const SizedBox(width: 8),
              FilterChip(
                avatar: const Icon(Icons.today, size: 18),
                label: const Text('This weekend'),
                selected: weekendOn,
                onSelected: (on) {
                  final n = ref.read(filtersProvider.notifier);
                  n.setRange(
                    on ? weekend.from : null,
                    on ? weekend.to : null,
                  );
                },
              ),
              const SizedBox(width: 8),
              FilterChip(
                avatar: const Icon(Icons.sell_outlined, size: 18),
                label: const Text('Free'),
                selected: quick.freeOnly,
                onSelected: (_) => qn.toggleFree(),
              ),
              const SizedBox(width: 8),
              FilterChip(
                avatar: locating
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.near_me, size: 18),
                label: const Text('Near me'),
                selected: quick.nearMe,
                onSelected: (on) => _onNearMe(context, ref, on),
              ),
            ],
          ),
        ),
        if (quick.nearMe)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 0),
            child: Row(
              children: [
                Icon(
                  Icons.social_distance,
                  size: 18,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
                Expanded(
                  child: Slider(
                    min: 1,
                    max: 50,
                    divisions: 49,
                    value: quick.radiusKm.clamp(1, 50),
                    label: '${quick.radiusKm.round()} km',
                    onChanged: qn.setRadius,
                  ),
                ),
                SizedBox(
                  width: 52,
                  child: Text(
                    'within ${quick.radiusKm.round()} km',
                    style: Theme.of(context).textTheme.labelSmall,
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }

  Future<void> _onNearMe(BuildContext context, WidgetRef ref, bool on) async {
    final qn = ref.read(quickFiltersProvider.notifier);
    if (!on) {
      qn.setNearMe(false);
      return;
    }
    final messenger = ScaffoldMessenger.of(context);
    try {
      if (!ref.read(locationProvider).hasFix) {
        await ref.read(locationProvider.notifier).refreshFix();
      }
      qn.setNearMe(true);
    } catch (e) {
      messenger.showSnackBar(
        SnackBar(
          content: Text(
            e is LocationException ? e.message : 'Location lookup failed: $e',
          ),
        ),
      );
    }
  }

  bool _sameDate(DateTime? a, DateTime? b) =>
      a != null &&
      b != null &&
      a.year == b.year &&
      a.month == b.month &&
      a.day == b.day;
}

/// Shown when the active quick filters exclude every loaded event — the feed
/// is paged, so the match may be on a page not yet fetched.
class _NoQuickMatches extends ConsumerWidget {
  const _NoQuickMatches({required this.hasMore});
  final bool hasMore;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final cs = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.filter_alt_off_outlined, size: 48, color: cs.outline),
            const SizedBox(height: 12),
            Text(
              'No loaded events match',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 6),
            Text(
              hasMore
                  ? 'Try widening the radius, or load more events.'
                  : 'Try widening the radius or clearing a chip.',
              textAlign: TextAlign.center,
              style: Theme.of(
                context,
              ).textTheme.bodyMedium?.copyWith(color: cs.onSurfaceVariant),
            ),
            if (hasMore) ...[
              const SizedBox(height: 16),
              FilledButton.tonalIcon(
                icon: const Icon(Icons.expand_more),
                label: const Text('Load more events'),
                onPressed: () =>
                    ref.read(eventFeedProvider.notifier).loadMore(),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _EventGrid extends StatelessWidget {
  const _EventGrid({required this.events, required this.onTap});

  final List<Event> events;
  final void Function(String id) onTap;

  static const double _maxContentWidth = 1500;
  static const double _gap = 16;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final available = constraints.maxWidth;
        final crossAxisCount = available >= 1100
            ? 3
            : available >= 720
            ? 2
            : 1;

        final contentWidth = available > _maxContentWidth
            ? _maxContentWidth
            : available;
        final horizontalPad = (available - contentWidth) / 2;

        final cellWidth =
            (contentWidth - (_gap * 2) - (_gap * (crossAxisCount - 1))) /
            crossAxisCount;
        final cellHeight = (cellWidth * 9 / 16) + 158;
        final aspect = cellWidth / cellHeight;

        return GridView.builder(
          physics: const AlwaysScrollableScrollPhysics(),
          padding: EdgeInsets.fromLTRB(
            horizontalPad + _gap,
            _gap,
            horizontalPad + _gap,
            _gap,
          ),
          gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: crossAxisCount,
            mainAxisSpacing: _gap,
            crossAxisSpacing: _gap,
            childAspectRatio: aspect,
          ),
          itemCount: events.length,
          itemBuilder: (_, i) {
            final ev = events[i];
            return EventCard(event: ev, onTap: () => onTap(ev.id));
          },
        );
      },
    );
  }
}

class _BuildingFeedView extends StatelessWidget {
  const _BuildingFeedView();

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      children: [
        const SizedBox(height: 80),
        Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              children: [
                SizedBox(
                  width: 48,
                  height: 48,
                  child: CircularProgressIndicator(
                    strokeWidth: 3,
                    color: cs.primary,
                  ),
                ),
                const SizedBox(height: 20),
                Text(
                  'Building your feed…',
                  style: Theme.of(context).textTheme.titleMedium,
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 8),
                Text(
                  'Pulling the latest events from free sources across\n'
                  'the world. This usually takes under a minute.',
                  style: Theme.of(
                    context,
                  ).textTheme.bodyMedium?.copyWith(color: cs.onSurfaceVariant),
                  textAlign: TextAlign.center,
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}
