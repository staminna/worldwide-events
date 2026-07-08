import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/event_api.dart';
import '../models/event.dart';
import '../util/notifications.dart';

enum FollowType { category, source, city }

/// Something the user wants to be notified about — a category, a source, or a
/// city. [createdAt] is the baseline: only events scraped after this fire a
/// notification, so following doesn't spam the back-catalog.
class Follow {
  const Follow({
    required this.type,
    required this.value,
    required this.label,
    required this.createdAt,
  });

  final FollowType type;
  final String value; // category.name / source.name / cityId
  final String label; // for display
  final DateTime createdAt;

  Map<String, dynamic> toJson() => {
    'type': type.name,
    'value': value,
    'label': label,
    'createdAt': createdAt.toIso8601String(),
  };

  factory Follow.fromJson(Map<String, dynamic> j) => Follow(
    type: FollowType.values.byName(j['type'] as String),
    value: j['value'] as String,
    label: j['label'] as String? ?? '',
    createdAt: DateTime.tryParse(j['createdAt'] as String? ?? '') ??
        DateTime.now(),
  );
}

class FollowsNotifier extends StateNotifier<List<Follow>> {
  FollowsNotifier() : super(const []) {
    _load();
  }

  static const _prefKey = 'follows_v1';

  Future<void> _load() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_prefKey);
    if (raw == null || raw.isEmpty) return;
    try {
      state = (jsonDecode(raw) as List)
          .cast<Map<String, dynamic>>()
          .map(Follow.fromJson)
          .toList();
    } catch (_) {}
  }

  Future<void> _persist() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(
      _prefKey,
      jsonEncode(state.map((f) => f.toJson()).toList()),
    );
  }

  bool isFollowing(FollowType type, String value) =>
      state.any((f) => f.type == type && f.value == value);

  Future<void> toggle(FollowType type, String value, String label) async {
    state = isFollowing(type, value)
        ? state.where((f) => !(f.type == type && f.value == value)).toList()
        : [
            ...state,
            Follow(
              type: type,
              value: value,
              label: label,
              createdAt: DateTime.now(),
            ),
          ];
    await _persist();
  }
}

final followsProvider =
    StateNotifierProvider<FollowsNotifier, List<Follow>>(
      (ref) => FollowsNotifier(),
    );

const _notifiedKey = 'notified_event_ids_v1';

/// Checks every follow against the backend and fires a local notification for
/// events scraped since the follow was created that we haven't announced yet.
///
/// This is a foreground/best-effort check (run on app start and resume) — it
/// is NOT true background push. Real push while the app is closed needs a
/// server + FCM/APNs, which can be layered on later without changing this UI.
Future<void> checkFollowsAndNotify(EventApi api, List<Follow> follows) async {
  if (follows.isEmpty) return;
  final prefs = await SharedPreferences.getInstance();
  final notified = (prefs.getStringList(_notifiedKey) ?? const <String>[])
      .toSet();

  final fresh = <Event>[];
  for (final f in follows) {
    try {
      final list = await api.fetchEvents(
        category: f.type == FollowType.category
            ? categoryFromString(f.value)
            : null,
        source: f.type == FollowType.source ? sourceFromString(f.value) : null,
        cityId: f.type == FollowType.city ? f.value : null,
        limit: 20,
      );
      for (final e in list.events) {
        if (e.scrapedAt.isAfter(f.createdAt) && notified.add(e.id)) {
          fresh.add(e);
        }
      }
    } catch (_) {
      // Skip this follow on any network error.
    }
  }
  if (fresh.isEmpty) return;

  await prefs.setStringList(_notifiedKey, notified.toList());
  final title = fresh.length == 1
      ? 'New event you follow'
      : '${fresh.length} new events you follow';
  final body = fresh.take(3).map((e) => e.title).join(' · ') +
      (fresh.length > 3 ? ' …' : '');
  await showFollowNotification(id: 1001, title: title, body: body);
}
