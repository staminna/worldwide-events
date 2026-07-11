import 'dart:async';
import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/chat.dart';
import 'chat.dart';
import 'chat_identity.dart';

const _readPrefKey = 'chat_read_v1';

/// The group chat screen currently on top, if any — messages arriving for it
/// are read by definition and never counted as unread.
final activeGroupProvider = StateProvider<String?>((ref) => null);

/// When each group was last read, persisted across launches. Opening a group
/// (and leaving it) marks it read; the Groups tab compares this against
/// lastMessageAt for the cold-start "something new" dot.
class ReadMarksNotifier extends StateNotifier<Map<String, DateTime>> {
  ReadMarksNotifier() : super(const {}) {
    _load();
  }

  Future<void> _load() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString(_readPrefKey);
      if (raw == null) return;
      final decoded = (jsonDecode(raw) as Map<String, dynamic>);
      state = {
        for (final e in decoded.entries)
          if (DateTime.tryParse(e.value as String) != null)
            e.key: DateTime.parse(e.value as String),
      };
    } catch (_) {
      // Corrupt entry — start over.
    }
  }

  Future<void> markRead(String groupId) async {
    state = {...state, groupId: DateTime.now()};
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(
      _readPrefKey,
      jsonEncode({
        for (final e in state.entries) e.key: e.value.toIso8601String(),
      }),
    );
  }
}

final readMarksProvider =
    StateNotifierProvider<ReadMarksNotifier, Map<String, DateTime>>(
  (ref) => ReadMarksNotifier(),
);

/// Per-group unread counts for this session, fed live by the socket. Exact
/// while the app runs; the persisted read marks cover what happened while it
/// was closed (as a dot, since counts can't be known without fetching).
class UnreadCountsNotifier extends StateNotifier<Map<String, int>> {
  UnreadCountsNotifier(this._ref) : super(const {}) {
    _wsSub = _ref.read(chatConnectionProvider).stream.listen(_onEnvelope);
  }

  final Ref _ref;
  late final StreamSubscription _wsSub;

  void _onEnvelope(WsEnvelope env) {
    if (env.type != 'message') return;
    if (env.kind == 'system') return; // joins don't demand attention
    if (env.groupId == _ref.read(activeGroupProvider)) return;
    if (env.userId == _ref.read(chatIdentityProvider).identity?.id) return;
    state = {...state, env.groupId: (state[env.groupId] ?? 0) + 1};
  }

  void clear(String groupId) {
    if (!state.containsKey(groupId)) return;
    state = {...state}..remove(groupId);
  }

  @override
  void dispose() {
    _wsSub.cancel();
    super.dispose();
  }
}

final unreadCountsProvider =
    StateNotifierProvider<UnreadCountsNotifier, Map<String, int>>(
  (ref) => UnreadCountsNotifier(ref),
);

/// Whether a specific group has anything unread: a live session count, or a
/// last message newer than the persisted read mark.
bool groupHasUnread({
  required String groupId,
  required DateTime? lastMessageAt,
  required Map<String, int> counts,
  required Map<String, DateTime> readMarks,
}) {
  if ((counts[groupId] ?? 0) > 0) return true;
  if (lastMessageAt == null) return false;
  final readAt = readMarks[groupId];
  return readAt == null || lastMessageAt.isAfter(readAt);
}

/// Drives the badge on the Groups tab icon.
final hasAnyUnreadProvider = Provider<bool>((ref) {
  final groups = ref.watch(groupsProvider).valueOrNull ?? const [];
  final counts = ref.watch(unreadCountsProvider);
  final readMarks = ref.watch(readMarksProvider);
  return groups.any((g) => groupHasUnread(
        groupId: g.id,
        lastMessageAt: g.lastMessageAt,
        counts: counts,
        readMarks: readMarks,
      ));
});
