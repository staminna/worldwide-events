import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';

import '../models/event.dart';
import '../state/providers.dart';
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
    _searchController =
        TextEditingController(text: ref.read(filtersProvider).search);
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
      ref.invalidate(eventsProvider);
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

  @override
  Widget build(BuildContext context) {
    final eventsAsync = ref.watch(eventsProvider);
    final filters = ref.watch(filtersProvider);

    eventsAsync.whenData((list) {
      if (list.events.isEmpty) {
        _schedulePoll();
      } else {
        _cancelPoll();
      }
    });

    return Scaffold(
      appBar: AppBar(
        titleSpacing: 8,
        title: _SearchField(
          controller: _searchController,
          onChanged: _onSearchChanged,
          onClear: () {
            _searchController.clear();
            _onSearchChanged('');
          },
        ),
        actions: [
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
              builder: (_) => const FiltersSheet(),
            ),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () async => ref.refresh(eventsProvider.future),
        child: eventsAsync.when(
          data: (list) {
            if (list.events.isEmpty) {
              return const _BuildingFeedView();
            }
            return Column(
              children: [
                _SortHeader(count: list.total),
                Expanded(
                  child: _EventGrid(
                    events: list.events,
                    onTap: (id) => context.push('/event/$id'),
                  ),
                ),
              ],
            );
          },
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (e, _) => ListView(
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
                      Text('Backend unreachable.\n$e',
                          textAlign: TextAlign.center),
                      const SizedBox(height: 12),
                      FilledButton(
                        onPressed: () => ref.invalidate(eventsProvider),
                        child: const Text('Retry'),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
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
  });

  final TextEditingController controller;
  final ValueChanged<String> onChanged;
  final VoidCallback onClear;

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
              textInputAction: TextInputAction.search,
              decoration: const InputDecoration(
                hintText: 'Search events…',
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
                        minHeight: 24, minWidth: 24),
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
  const _SortHeader({required this.count});
  final int count;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final fmt = NumberFormat.decimalPattern();
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 12, 20, 0),
      child: Row(
        children: [
          Text(
            '${fmt.format(count)} events',
            style: Theme.of(context).textTheme.titleSmall,
          ),
          const Spacer(),
          Icon(Icons.swap_vert, size: 16, color: cs.onSurfaceVariant),
          const SizedBox(width: 4),
          Text(
            'Sorted by date • soonest first',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: cs.onSurfaceVariant,
                ),
          ),
        ],
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

        final contentWidth =
            available > _maxContentWidth ? _maxContentWidth : available;
        final horizontalPad = (available - contentWidth) / 2;

        final cellWidth = (contentWidth - (_gap * 2) -
                (_gap * (crossAxisCount - 1))) /
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
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                        color: cs.onSurfaceVariant,
                      ),
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
