import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:intl/intl.dart';

import '../models/event.dart';
import '../state/providers.dart';
import '../widgets/location_search_field.dart';

/// Form for adding a user-authored event (POST /events, "manual" source).
class AddEventScreen extends ConsumerStatefulWidget {
  const AddEventScreen({super.key});

  @override
  ConsumerState<AddEventScreen> createState() => _AddEventScreenState();
}

class _AddEventScreenState extends ConsumerState<AddEventScreen> {
  final _titleController = TextEditingController();
  final _descController = TextEditingController();
  final _venueController = TextEditingController();
  final _imageController = TextEditingController();

  EventCategory _category = EventCategory.music;
  String? _cityId;
  DateTime? _startsAt;
  LocationResult? _location;
  bool _submitting = false;

  @override
  void initState() {
    super.initState();
    // Default to whatever city the feed is currently filtered on.
    _cityId = ref.read(filtersProvider).cityId;
  }

  @override
  void dispose() {
    _titleController.dispose();
    _descController.dispose();
    _venueController.dispose();
    _imageController.dispose();
    super.dispose();
  }

  Future<void> _pickStart() async {
    final now = DateTime.now();
    final date = await showDatePicker(
      context: context,
      initialDate: _startsAt ?? now,
      firstDate: now.subtract(const Duration(days: 1)),
      lastDate: now.add(const Duration(days: 730)),
    );
    if (date == null || !mounted) return;
    final time = await showTimePicker(
      context: context,
      initialTime: TimeOfDay.fromDateTime(_startsAt ?? now),
    );
    if (!mounted) return;
    setState(() {
      _startsAt = DateTime(
        date.year,
        date.month,
        date.day,
        time?.hour ?? 19,
        time?.minute ?? 0,
      );
    });
  }

  /// When a venue is picked, remember its coordinates and auto-select the
  /// nearest catalog city (the user can still override via the dropdown).
  Future<void> _onLocationSelected(LocationResult r) async {
    setState(() {
      _location = r;
      if (_venueController.text.trim().isEmpty) {
        _venueController.text = r.displayName.split(',').first.trim();
      }
    });
    try {
      final nearest = await ref
          .read(apiProvider)
          .reverseGeocode(r.lat, r.lon);
      if (mounted) setState(() => _cityId = nearest.city.id);
    } catch (_) {
      // Nearest-city is a convenience; the user can pick manually.
    }
  }

  Future<void> _submit() async {
    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);
    final title = _titleController.text.trim();
    if (title.isEmpty) {
      messenger.showSnackBar(const SnackBar(content: Text('Title is required')));
      return;
    }
    if (_cityId == null) {
      messenger.showSnackBar(const SnackBar(content: Text('Pick a city')));
      return;
    }
    if (_startsAt == null) {
      messenger.showSnackBar(
        const SnackBar(content: Text('Pick a start date & time')),
      );
      return;
    }
    setState(() => _submitting = true);
    try {
      await ref
          .read(apiProvider)
          .createEvent(
            title: title,
            description: _descController.text.trim(),
            category: _category,
            startsAt: _startsAt!,
            cityId: _cityId!,
            venueName: _venueController.text.trim(),
            address: _location?.displayName ?? '',
            lat: _location?.lat,
            lon: _location?.lon,
            imageUrl: _imageController.text.trim(),
          );
      // Surface the new event immediately, and drop the city filter onto the
      // event's city so it's guaranteed to show up in the feed.
      ref.read(filtersProvider.notifier).setCity(_cityId);
      await ref.read(eventFeedProvider.notifier).refresh();
      if (!mounted) return;
      messenger.showSnackBar(const SnackBar(content: Text('Event added')));
      navigator.pop();
    } catch (e) {
      if (!mounted) return;
      setState(() => _submitting = false);
      messenger.showSnackBar(SnackBar(content: Text('Could not add event: $e')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final citiesAsync = ref.watch(citiesProvider);
    final startLabel = _startsAt == null
        ? 'Pick date & time'
        : DateFormat.yMMMEd().add_jm().format(_startsAt!);

    return Scaffold(
      appBar: AppBar(title: const Text('Add event')),
      body: ListView(
        padding: EdgeInsets.fromLTRB(
          16,
          16,
          16,
          16 + MediaQuery.viewInsetsOf(context).bottom,
        ),
        children: [
          TextField(
            controller: _titleController,
            textCapitalization: TextCapitalization.sentences,
            decoration: const InputDecoration(
              labelText: 'Title *',
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 16),
          Text('Category', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            children: [
              for (final c in EventCategory.values.where(
                (c) => c != EventCategory.unknown,
              ))
                ChoiceChip(
                  label: Text(categoryLabel(c)),
                  selected: _category == c,
                  onSelected: (_) => setState(() => _category = c),
                ),
            ],
          ),
          const SizedBox(height: 16),
          Text('City *', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          citiesAsync.when(
            data: (cities) {
              final sorted = [...cities]
                ..sort(
                  (a, b) =>
                      a.name.toLowerCase().compareTo(b.name.toLowerCase()),
                );
              return DropdownMenu<String?>(
                key: ValueKey(_cityId),
                initialSelection: _cityId,
                enableFilter: true,
                requestFocusOnTap: true,
                expandedInsets: EdgeInsets.zero,
                hintText: 'Choose a city',
                leadingIcon: const Icon(Icons.location_city_outlined),
                menuHeight: 320,
                dropdownMenuEntries: [
                  for (final c in sorted)
                    DropdownMenuEntry<String?>(
                      value: c.id,
                      label: '${c.name}, ${c.country}',
                    ),
                ],
                onSelected: (v) => setState(() => _cityId = v),
              );
            },
            loading: () => const LinearProgressIndicator(),
            error: (e, _) => Text('Failed to load cities: $e'),
          ),
          const SizedBox(height: 16),
          Text('When *', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          OutlinedButton.icon(
            icon: const Icon(Icons.event),
            label: Text(startLabel),
            onPressed: _pickStart,
          ),
          const SizedBox(height: 16),
          Text('Venue', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          LocationSearchField(
            hintText: 'Search venue or address…',
            onSelected: _onLocationSelected,
          ),
          const SizedBox(height: 8),
          TextField(
            controller: _venueController,
            decoration: const InputDecoration(
              labelText: 'Venue name',
              border: OutlineInputBorder(),
              isDense: true,
            ),
          ),
          if (_location != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Row(
                children: [
                  const Icon(Icons.place, size: 16),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      'Pinned: ${_location!.lat.toStringAsFixed(4)}, '
                      '${_location!.lon.toStringAsFixed(4)}',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ),
                ],
              ),
            ),
          const SizedBox(height: 16),
          TextField(
            controller: _imageController,
            keyboardType: TextInputType.url,
            decoration: const InputDecoration(
              labelText: 'Image URL (optional)',
              helperText: 'Adds a cover image to the event card',
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _descController,
            maxLines: 3,
            textCapitalization: TextCapitalization.sentences,
            decoration: const InputDecoration(
              labelText: 'Description (optional)',
              border: OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 24),
          FilledButton.icon(
            onPressed: _submitting ? null : _submit,
            icon: _submitting
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.check),
            label: Text(_submitting ? 'Adding…' : 'Add event'),
          ),
        ],
      ),
    );
  }
}
