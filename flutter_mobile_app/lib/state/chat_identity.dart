import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/chat_api.dart';
import '../models/chat.dart';

const _identityPrefKey = 'chat_identity_v1';

final chatApiProvider = Provider<ChatApi>((ref) => ChatApi());

class ChatIdentityState {
  const ChatIdentityState({this.loading = true, this.identity});

  final bool loading;
  final ChatIdentity? identity;

  bool get registered => identity != null;
}

/// Loads the persisted anonymous identity, or registers a new one on the
/// user's first touch of any chat feature. The token doubles as the ChatApi
/// bearer, so it's pushed into the api instance whenever it becomes known.
class ChatIdentityNotifier extends StateNotifier<ChatIdentityState> {
  ChatIdentityNotifier(this._api) : super(const ChatIdentityState()) {
    _load();
  }

  final ChatApi _api;

  Future<void> _load() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString(_identityPrefKey);
      if (raw != null) {
        final identity =
            ChatIdentity.fromJson(jsonDecode(raw) as Map<String, dynamic>);
        _api.token = identity.token;
        state = ChatIdentityState(loading: false, identity: identity);
        return;
      }
    } catch (_) {
      // Corrupt prefs entry — treat as unregistered.
    }
    state = const ChatIdentityState(loading: false);
  }

  /// Registers with the chosen display name and persists the result. No-op
  /// when an identity already exists.
  Future<ChatIdentity> ensureRegistered(String name) async {
    final existing = state.identity;
    if (existing != null) return existing;
    final identity = await _api.register(name.trim());
    _api.token = identity.token;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_identityPrefKey, jsonEncode(identity.toJson()));
    state = ChatIdentityState(loading: false, identity: identity);
    return identity;
  }
}

final chatIdentityProvider =
    StateNotifierProvider<ChatIdentityNotifier, ChatIdentityState>(
  (ref) => ChatIdentityNotifier(ref.watch(chatApiProvider)),
);
