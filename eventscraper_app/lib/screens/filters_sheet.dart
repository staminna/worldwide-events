import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/event.dart';
import '../state/providers.dart';

class FiltersSheet extends ConsumerWidget {
  const FiltersSheet({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final filters = ref.watch(filtersProvider);
    final notifier = ref.read(filtersProvider.notifier);
    final citiesAsync = ref.watch(citiesProvider);
    final sourcesAsync = ref.watch(sourcesProvider);

    return DraggableScrollableSheet(
      expand: false,
      initialChildSize: 0.7,
      minChildSize: 0.4,
      maxChildSize: 0.95,
      builder: (_, controller) => ListView(
        controller: controller,
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
        children: [
          Center(
            child: Container(
              width: 40,
              height: 4,
              margin: const EdgeInsets.only(bottom: 16),
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.outlineVariant,
                borderRadius: BorderRadius.circular(4),
              ),
            ),
          ),
          Text('City', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          citiesAsync.when(
            data: (cities) {
              final sorted = [...cities]
                ..sort(
                  (a, b) =>
                      a.name.toLowerCase().compareTo(b.name.toLowerCase()),
                );
              // Type-to-filter menu: with 150+ cities a plain dropdown is
              // unusable, so let the user narrow it down by typing.
              return DropdownMenu<String?>(
                // Re-key when the selection changes elsewhere (e.g. "Clear
                // filters") so the menu's internal text field resets too.
                key: ValueKey(filters.cityId),
                initialSelection: filters.cityId,
                enableFilter: true,
                requestFocusOnTap: true,
                expandedInsets: EdgeInsets.zero,
                hintText: 'All cities',
                leadingIcon: const Icon(Icons.location_on_outlined),
                menuHeight: 320,
                dropdownMenuEntries: [
                  const DropdownMenuEntry<String?>(
                    value: null,
                    label: 'All cities',
                  ),
                  for (final c in sorted)
                    DropdownMenuEntry<String?>(
                      value: c.id,
                      label: '${c.name}, ${c.country}',
                    ),
                ],
                onSelected: notifier.setCity,
              );
            },
            loading: () => const LinearProgressIndicator(),
            error: (e, _) => Text('Failed to load cities: $e'),
          ),
          const SizedBox(height: 20),
          Text('Category', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            children: [
              ChoiceChip(
                label: const Text('All'),
                selected: filters.category == null,
                onSelected: (_) => notifier.setCategory(null),
              ),
              for (final c in EventCategory.values.where(
                (c) => c != EventCategory.unknown,
              ))
                ChoiceChip(
                  label: Text(categoryLabel(c)),
                  selected: filters.category == c,
                  onSelected: (_) => notifier.setCategory(c),
                ),
            ],
          ),
          const SizedBox(height: 20),
          Text('Source', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          sourcesAsync.when(
            data: (sources) {
              final enabled = sources
                  .where((s) => s.configured && s.id != EventSource.unknown)
                  .map((s) => s.id)
                  .toList();
              return Wrap(
                spacing: 8,
                children: [
                  ChoiceChip(
                    label: const Text('All'),
                    selected: filters.source == null,
                    onSelected: (_) => notifier.setSource(null),
                  ),
                  for (final s in enabled)
                    ChoiceChip(
                      label: Text(sourceLabel(s)),
                      selected: filters.source == s,
                      onSelected: (_) => notifier.setSource(s),
                    ),
                ],
              );
            },
            loading: () => const LinearProgressIndicator(),
            error: (e, _) => Text('Failed to load sources: $e'),
          ),
          const SizedBox(height: 20),
          Text('Date range', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            icon: const Icon(Icons.date_range),
            label: Text(
              filters.from == null && filters.to == null
                  ? 'Any date'
                  : '${_fmt(filters.from)} → ${_fmt(filters.to)}',
            ),
            onPressed: () async {
              final now = DateTime.now();
              final result = await showDateRangePicker(
                context: context,
                firstDate: now.subtract(const Duration(days: 1)),
                lastDate: now.add(const Duration(days: 365)),
              );
              if (result != null) {
                notifier.setRange(result.start, result.end);
              }
            },
          ),
          const SizedBox(height: 24),
          Row(
            children: [
              Expanded(
                child: OutlinedButton(
                  onPressed: notifier.clear,
                  child: const Text('Clear filters'),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: FilledButton(
                  onPressed: () => Navigator.of(context).pop(),
                  child: const Text('Apply'),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }

  String _fmt(DateTime? d) => d == null
      ? '—'
      : '${d.year}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';
}
