import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';

import '../models/chat.dart';
import 'chat.dart';
import 'chat_identity.dart';

/// Session-based location sharing: while a group id is in the state set, the
/// device streams its position to that group over the chat socket. Foreground
/// only — backgrounding drops the socket and the server ends the share for
/// everyone else (their dot disappears), which is the intended privacy model.
class LocationShareNotifier extends StateNotifier<Set<String>> {
  LocationShareNotifier(this._ref) : super(const {}) {
    // The server forgets shares on disconnect; when the socket comes back,
    // one re-sent fix restores the session for every group we share to.
    _conn.connected.addListener(_onConnectionChange);
  }

  final Ref _ref;
  StreamSubscription<Position>? _posSub;
  Timer? _heartbeat;
  Position? _last;
  DateTime _lastSentAt = DateTime.fromMillisecondsSinceEpoch(0);

  ChatConnection get _conn => _ref.read(chatConnectionProvider);

  bool isSharing(String groupId) => state.contains(groupId);

  /// Starts sharing to [groupId]. Returns false when location permission is
  /// missing/denied (callers surface a snackbar).
  Future<bool> start(String groupId) async {
    var perm = await Geolocator.checkPermission();
    if (perm == LocationPermission.denied) {
      perm = await Geolocator.requestPermission();
    }
    if (perm == LocationPermission.denied ||
        perm == LocationPermission.deniedForever) {
      return false;
    }
    _conn.ensureConnected();
    state = {...state, groupId};
    _ensurePipelines();
    // Seed immediately with the last known position so peers see the dot
    // without waiting for the first stream fix.
    final seed = _last ?? await Geolocator.getLastKnownPosition();
    if (seed != null) _sendFix(seed, force: true);
    return true;
  }

  void stop(String groupId) {
    if (!state.contains(groupId)) return;
    state = {...state}..remove(groupId);
    _conn.send(WsEnvelope(type: 'location_stop', groupId: groupId));
    if (state.isEmpty) _teardownPipelines();
  }

  void _ensurePipelines() {
    // Same continuous-fix pattern as the venue navigation screen
    // (venue_map_screen.dart) — high accuracy, only report >=15 m moves.
    _posSub ??= Geolocator.getPositionStream(
      locationSettings: const LocationSettings(
        accuracy: LocationAccuracy.high,
        distanceFilter: 15,
      ),
    ).listen(_sendFix, onError: (_) {});
    // Stationary users still heartbeat so the server's 2-minute staleness
    // sweep doesn't end their session.
    _heartbeat ??= Timer.periodic(const Duration(seconds: 20), (_) {
      final last = _last;
      if (last != null) _sendFix(last, force: true);
    });
  }

  void _teardownPipelines() {
    _posSub?.cancel();
    _posSub = null;
    _heartbeat?.cancel();
    _heartbeat = null;
  }

  void _sendFix(Position pos, {bool force = false}) {
    _last = pos;
    final now = DateTime.now();
    // Throttle stream bursts to one frame per 5s (server drops <2s anyway).
    if (!force && now.difference(_lastSentAt).inSeconds < 5) return;
    _lastSentAt = now;
    for (final groupId in state) {
      _conn.send(WsEnvelope(
        type: 'location',
        groupId: groupId,
        lat: pos.latitude,
        lon: pos.longitude,
        acc: pos.accuracy,
      ));
    }
  }

  void _onConnectionChange() {
    if (_conn.connected.value && state.isNotEmpty) {
      final last = _last;
      if (last != null) _sendFix(last, force: true);
    }
  }

  @override
  void dispose() {
    _conn.connected.removeListener(_onConnectionChange);
    _teardownPipelines();
    super.dispose();
  }
}

final locationShareProvider =
    StateNotifierProvider<LocationShareNotifier, Set<String>>(
  (ref) => LocationShareNotifier(ref),
);

/// Other members' live positions across all groups, keyed by user id — the
/// map layer renders exactly this. Cleared when the socket drops (whatever we
/// had is stale by the time it comes back; presence snapshots repopulate it).
class PeersNotifier extends StateNotifier<Map<String, PeerFix>> {
  PeersNotifier(this._ref) : super(const {}) {
    _wsSub = _ref.read(chatConnectionProvider).stream.listen(_onEnvelope);
    _ref.read(chatConnectionProvider).connected.addListener(_onConnectionChange);
    _sweep = Timer.periodic(const Duration(seconds: 30), (_) => _dropStale());
  }

  final Ref _ref;
  late final StreamSubscription _wsSub;
  late final Timer _sweep;

  String get _selfId => _ref.read(chatIdentityProvider).identity?.id ?? '';

  void _onEnvelope(WsEnvelope env) {
    switch (env.type) {
      case 'location':
        if (env.userId == _selfId) return;
        state = {
          ...state,
          env.userId: PeerFix(
            userId: env.userId,
            name: env.name,
            groupId: env.groupId,
            lat: env.lat,
            lon: env.lon,
            acc: env.acc,
            at: DateTime.tryParse(env.at)?.toLocal() ?? DateTime.now(),
          ),
        };
      case 'location_stop':
        if (state.containsKey(env.userId)) {
          state = {...state}..remove(env.userId);
        }
      case 'presence':
        var next = state;
        for (final s in env.sharing) {
          final uid = s['userId'] as String? ?? '';
          if (uid.isEmpty || uid == _selfId) continue;
          next = {
            ...next,
            uid: PeerFix(
              userId: uid,
              name: s['name'] as String? ?? '?',
              groupId: env.groupId,
              lat: (s['lat'] as num?)?.toDouble() ?? 0,
              lon: (s['lon'] as num?)?.toDouble() ?? 0,
              acc: (s['acc'] as num?)?.toDouble() ?? 0,
              at: DateTime.tryParse(s['at'] as String? ?? '')?.toLocal() ??
                  DateTime.now(),
            ),
          };
        }
        if (!identical(next, state)) state = next;
    }
  }

  void _onConnectionChange() {
    if (!_ref.read(chatConnectionProvider).connected.value && state.isNotEmpty) {
      state = const {};
    }
  }

  void _dropStale() {
    final cutoff = DateTime.now().subtract(const Duration(minutes: 2));
    final fresh = {
      for (final e in state.entries)
        if (e.value.at.isAfter(cutoff)) e.key: e.value,
    };
    if (fresh.length != state.length) state = fresh;
  }

  @override
  void dispose() {
    _wsSub.cancel();
    _sweep.cancel();
    _ref.read(chatConnectionProvider).connected.removeListener(_onConnectionChange);
    super.dispose();
  }
}

final peersProvider = StateNotifierProvider<PeersNotifier, Map<String, PeerFix>>(
  (ref) => PeersNotifier(ref),
);
