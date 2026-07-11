import 'dart:async';
import 'dart:convert';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:uuid/uuid.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../models/chat.dart';
import 'chat_identity.dart';

/// Owns the single chat WebSocket for the whole app: connects once an
/// identity exists and a chat surface asks for it, reconnects with capped
/// exponential backoff, and fans inbound envelopes out on [stream].
///
/// Foreground-only by design: backgrounding the app closes the socket, which
/// also ends any live location share server-side (the plan's privacy cut).
class ChatConnection with WidgetsBindingObserver {
  ChatConnection(this._ref) {
    WidgetsBinding.instance.addObserver(this);
  }

  final Ref _ref;
  final _controller = StreamController<WsEnvelope>.broadcast();
  final connected = ValueNotifier<bool>(false);

  WebSocketChannel? _channel;
  StreamSubscription? _sub;
  Timer? _retry;
  int _attempt = 0;
  bool _wanted = false;

  Stream<WsEnvelope> get stream => _controller.stream;

  /// Called by any chat surface that needs live delivery. Safe to call often.
  void ensureConnected() {
    _wanted = true;
    if (_channel != null) return;
    final identity = _ref.read(chatIdentityProvider).identity;
    if (identity == null) return;
    _connect();
  }

  void _connect() {
    final api = _ref.read(chatApiProvider);
    try {
      final channel = WebSocketChannel.connect(Uri.parse(api.wsUrl()));
      _channel = channel;
      _sub = channel.stream.listen(
        (raw) {
          if (!connected.value) {
            // First frame = the connection round-tripped; reset backoff.
            connected.value = true;
            _attempt = 0;
          }
          try {
            final env = WsEnvelope.fromJson(
                jsonDecode(raw as String) as Map<String, dynamic>);
            _controller.add(env);
          } catch (_) {
            // Skip unparseable frames.
          }
        },
        onDone: _onDrop,
        onError: (_) => _onDrop(),
      );
      // No handshake frame from the server is guaranteed until group
      // activity happens, so consider a surviving channel connected.
      Future.delayed(const Duration(seconds: 2), () {
        if (_channel == channel) {
          connected.value = true;
          _attempt = 0;
        }
      });
    } catch (_) {
      _onDrop();
    }
  }

  void _onDrop() {
    _sub?.cancel();
    _sub = null;
    _channel = null;
    connected.value = false;
    if (!_wanted) return;
    // Capped exponential backoff: 1s, 2s, 4s ... 30s.
    final delay = Duration(seconds: (1 << _attempt).clamp(1, 30));
    if (_attempt < 5) _attempt++;
    _retry?.cancel();
    _retry = Timer(delay, () {
      if (_wanted && _channel == null) _connect();
    });
  }

  /// Sends a client→server envelope; reports whether the socket was open.
  bool send(WsEnvelope env) {
    final channel = _channel;
    if (channel == null || !connected.value) return false;
    channel.sink.add(jsonEncode(env.toJson()));
    return true;
  }

  /// Subscribes the live socket to a group joined after connect.
  void subscribeGroup(String groupId) {
    send(WsEnvelope(type: 'sub', groupId: groupId));
  }

  void disconnect() {
    _wanted = false;
    _retry?.cancel();
    _sub?.cancel();
    _sub = null;
    _channel?.sink.close();
    _channel = null;
    connected.value = false;
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      if (_wanted && _channel == null) {
        _attempt = 0;
        _connect();
      }
    } else if (state == AppLifecycleState.paused) {
      // Keep _wanted so resume reconnects; just drop the socket now instead
      // of letting the OS strand it half-dead.
      _retry?.cancel();
      _sub?.cancel();
      _sub = null;
      _channel?.sink.close();
      _channel = null;
      connected.value = false;
    }
  }
}

final chatConnectionProvider = Provider<ChatConnection>((ref) {
  final conn = ChatConnection(ref);
  // Connect right away when an identity already exists — e.g. a cold start
  // straight to the Map tab, where peer dots need the socket but no chat
  // surface ever calls ensureConnected.
  if (ref.read(chatIdentityProvider).registered) conn.ensureConnected();
  // And connect late when the identity appears (first registration, or the
  // prefs load finishing after this provider was created).
  ref.listen(chatIdentityProvider, (prev, next) {
    if (next.identity != null) conn.ensureConnected();
  });
  ref.onDispose(conn.disconnect);
  return conn;
});

/// The user's groups, kept live: loaded over REST, then updated in place by
/// incoming WS message envelopes (last message + resort by activity), so the
/// Groups tab reflects new messages without a manual refresh. Screens still
/// call `ref.invalidate(groupsProvider)` after create/join/leave.
class GroupsNotifier extends StateNotifier<AsyncValue<List<ChatGroup>>> {
  GroupsNotifier(this._ref, {required bool registered})
      : super(const AsyncValue.loading()) {
    _wsSub = _ref.read(chatConnectionProvider).stream.listen(_onEnvelope);
    if (registered) {
      refresh();
    } else {
      state = const AsyncValue.data([]);
    }
  }

  final Ref _ref;
  late final StreamSubscription _wsSub;
  bool _refreshing = false;

  Future<void> refresh() async {
    if (_refreshing) return;
    _refreshing = true;
    try {
      final groups = await _ref.read(chatApiProvider).myGroups();
      if (mounted) state = AsyncValue.data(groups);
    } catch (e, st) {
      if (mounted && state.valueOrNull == null) {
        state = AsyncValue.error(e, st);
      }
    } finally {
      _refreshing = false;
    }
  }

  void _onEnvelope(WsEnvelope env) {
    if (env.type != 'message') return;
    final groups = state.valueOrNull;
    if (groups == null) return;
    final i = groups.indexWhere((g) => g.id == env.groupId);
    if (i < 0) {
      // A message for a group we don't know yet (joined on another surface):
      // reload the list.
      refresh();
      return;
    }
    final updated = [...groups];
    updated[i] = updated[i].copyWith(
      lastMessage: env.body,
      lastMessageAt:
          DateTime.tryParse(env.createdAt)?.toLocal() ?? DateTime.now(),
    );
    updated.sort((a, b) {
      final at = a.lastMessageAt ?? DateTime.fromMillisecondsSinceEpoch(0);
      final bt = b.lastMessageAt ?? DateTime.fromMillisecondsSinceEpoch(0);
      return bt.compareTo(at);
    });
    state = AsyncValue.data(updated);
  }

  @override
  void dispose() {
    _wsSub.cancel();
    super.dispose();
  }
}

final groupsProvider =
    StateNotifierProvider<GroupsNotifier, AsyncValue<List<ChatGroup>>>((ref) {
  final identity = ref.watch(chatIdentityProvider);
  return GroupsNotifier(ref, registered: identity.registered);
});

class GroupMessagesState {
  const GroupMessagesState({
    this.messages = const [],
    this.loading = true,
    this.hasMore = false,
    this.error = '',
  });

  /// Ascending by time (oldest first).
  final List<ChatMessageModel> messages;
  final bool loading;
  final bool hasMore;
  final String error;

  GroupMessagesState copyWith({
    List<ChatMessageModel>? messages,
    bool? loading,
    bool? hasMore,
    String? error,
  }) =>
      GroupMessagesState(
        messages: messages ?? this.messages,
        loading: loading ?? this.loading,
        hasMore: hasMore ?? this.hasMore,
        error: error ?? this.error,
      );
}

const _pageSize = 50;

/// Per-group message list: first page over REST, older pages by cursor, live
/// appends from the WebSocket, and optimistic sends reconciled by clientRef.
class GroupMessagesNotifier extends StateNotifier<GroupMessagesState> {
  GroupMessagesNotifier(this._ref, this.groupId)
      : super(const GroupMessagesState()) {
    _wsSub = _ref.read(chatConnectionProvider).stream.listen(_onEnvelope);
    _ref.read(chatConnectionProvider).ensureConnected();
    _loadInitial();
  }

  final Ref _ref;
  final String groupId;
  late final StreamSubscription _wsSub;
  static const _uuid = Uuid();

  Future<void> _loadInitial() async {
    try {
      final page =
          await _ref.read(chatApiProvider).messages(groupId, limit: _pageSize);
      if (!mounted) return;
      state = GroupMessagesState(
        messages: page.reversed.toList(),
        loading: false,
        hasMore: page.length == _pageSize,
      );
    } catch (e) {
      if (!mounted) return;
      state = GroupMessagesState(loading: false, error: 'Could not load messages');
    }
  }

  Future<void> loadOlder() async {
    final current = state;
    if (current.loading || !current.hasMore || current.messages.isEmpty) return;
    final oldest = current.messages
        .firstWhere((m) => m.id > 0, orElse: () => current.messages.first);
    try {
      final page = await _ref
          .read(chatApiProvider)
          .messages(groupId, before: oldest.id, limit: _pageSize);
      if (!mounted) return;
      state = state.copyWith(
        messages: [...page.reversed, ...state.messages],
        hasMore: page.length == _pageSize,
      );
    } catch (_) {
      // Leave hasMore set; the user can retry by scrolling again.
    }
  }

  void _onEnvelope(WsEnvelope env) {
    if (env.groupId != groupId) return;
    switch (env.type) {
      case 'message':
        _mergeIncoming(env.toMessage());
      case 'join':
      case 'leave':
        // Presence chatter; the join also arrives as a system message which
        // is what we render. Nothing to do here.
        break;
    }
  }

  void _mergeIncoming(ChatMessageModel incoming) {
    final msgs = [...state.messages];
    if (incoming.clientRef.isNotEmpty) {
      final i = msgs.indexWhere((m) => m.clientRef == incoming.clientRef);
      if (i >= 0) {
        // Echo of our optimistic send: keep the uiKey (clientRef), take the
        // server id/timestamp.
        msgs[i] = ChatMessageModel(
          id: incoming.id,
          groupId: incoming.groupId,
          userId: incoming.userId,
          name: incoming.name,
          kind: incoming.kind,
          body: incoming.body,
          createdAt: incoming.createdAt,
          clientRef: incoming.clientRef,
        );
        state = state.copyWith(messages: msgs);
        return;
      }
    }
    if (incoming.id > 0 && msgs.any((m) => m.id == incoming.id)) return;
    msgs.add(incoming);
    state = state.copyWith(messages: msgs);
  }

  /// Optimistic send: append immediately, deliver over WS when the socket is
  /// open, otherwise fall back to REST (whose response reconciles the same
  /// way the WS echo does).
  Future<void> sendText(String body) async {
    final trimmed = body.trim();
    if (trimmed.isEmpty) return;
    final identity = _ref.read(chatIdentityProvider).identity;
    if (identity == null) return;

    final clientRef = 'c-${_uuid.v4()}';
    final local = ChatMessageModel(
      id: 0,
      groupId: groupId,
      userId: identity.id,
      name: identity.name,
      kind: 'text',
      body: trimmed,
      createdAt: DateTime.now(),
      clientRef: clientRef,
      pending: true,
    );
    state = state.copyWith(messages: [...state.messages, local]);

    final sentLive = _ref.read(chatConnectionProvider).send(WsEnvelope(
        type: 'message', groupId: groupId, body: trimmed, clientRef: clientRef));
    if (sentLive) return; // WS echo reconciles.

    try {
      final stored =
          await _ref.read(chatApiProvider).sendMessage(groupId, trimmed);
      if (!mounted) return;
      _mergeIncoming(ChatMessageModel(
        id: stored.id,
        groupId: stored.groupId,
        userId: stored.userId,
        name: stored.name,
        kind: stored.kind,
        body: stored.body,
        createdAt: stored.createdAt,
        clientRef: clientRef,
      ));
    } catch (_) {
      if (!mounted) return;
      final msgs = [...state.messages];
      final i = msgs.indexWhere((m) => m.clientRef == clientRef);
      if (i >= 0) {
        msgs[i] = msgs[i].copyWith(pending: false, failed: true);
        state = state.copyWith(messages: msgs);
      }
    }
  }

  @override
  void dispose() {
    _wsSub.cancel();
    super.dispose();
  }
}

final groupMessagesProvider = StateNotifierProvider.autoDispose
    .family<GroupMessagesNotifier, GroupMessagesState, String>(
  (ref, groupId) => GroupMessagesNotifier(ref, groupId),
);
