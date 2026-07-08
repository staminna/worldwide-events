import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/event.dart';

/// Bookmarked events ("My agenda"), persisted as JSON in shared_preferences so
/// the list survives restarts and renders offline without re-fetching.
class SavedEventsNotifier extends StateNotifier<List<Event>> {
  SavedEventsNotifier() : super(const []) {
    _load();
  }

  static const _prefKey = 'saved_events_v1';

  Future<void> _load() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_prefKey);
    if (raw == null || raw.isEmpty) return;
    try {
      state = (jsonDecode(raw) as List)
          .cast<Map<String, dynamic>>()
          .map(Event.fromJson)
          .toList();
    } catch (_) {
      // Corrupt/obsolete store — start clean rather than crash on boot.
    }
  }

  Future<void> _persist() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(
      _prefKey,
      jsonEncode(state.map((e) => e.toJson()).toList()),
    );
  }

  bool isSaved(String id) => state.any((e) => e.id == id);

  /// Adds the event if absent (newest first), removes it if already saved.
  Future<void> toggle(Event event) async {
    state = isSaved(event.id)
        ? state.where((e) => e.id != event.id).toList()
        : [event, ...state];
    await _persist();
  }

  Future<void> remove(String id) async {
    state = state.where((e) => e.id != id).toList();
    await _persist();
  }
}

final savedEventsProvider =
    StateNotifierProvider<SavedEventsNotifier, List<Event>>(
      (ref) => SavedEventsNotifier(),
    );

/// Whether a given event id is currently saved — narrow enough that only the
/// bookmark buttons for that event rebuild when it toggles.
final isSavedProvider = Provider.autoDispose.family<bool, String>((ref, id) {
  return ref.watch(savedEventsProvider).any((e) => e.id == id);
});
