import 'package:dio/dio.dart';

import '../models/chat.dart';
import 'event_api.dart' show kApiBase;

/// REST client for the backend's /chat endpoints plus the WebSocket URL
/// helper. The bearer [token] is set once the identity loads/registers
/// (see ChatIdentityNotifier) — every call except [register] requires it.
class ChatApi {
  ChatApi({String? baseUrl})
      : _dio = Dio(BaseOptions(
          baseUrl: baseUrl ?? kApiBase,
          connectTimeout: const Duration(seconds: 8),
          receiveTimeout: const Duration(seconds: 15),
          responseType: ResponseType.json,
        ));

  final Dio _dio;
  String token = '';

  Options get _auth => Options(headers: {'Authorization': 'Bearer $token'});

  /// ws(s):// address of the chat socket. Tokens go in the query string
  /// because WebSocket clients can't set headers.
  String wsUrl() {
    final base = _dio.options.baseUrl;
    final ws = base.startsWith('https')
        ? base.replaceFirst('https', 'wss')
        : base.replaceFirst('http', 'ws');
    return '$ws/chat/ws?token=$token';
  }

  Map<String, dynamic> _data(Response res) =>
      (res.data as Map<String, dynamic>)['data'] as Map<String, dynamic>;

  List<Map<String, dynamic>> _dataList(Response res) =>
      ((res.data as Map<String, dynamic>)['data'] as List? ?? const [])
          .cast<Map<String, dynamic>>();

  Future<ChatIdentity> register(String name) async {
    final res = await _dio.post('/chat/register', data: {'name': name});
    return ChatIdentity.fromJson(_data(res));
  }

  Future<List<ChatGroup>> myGroups() async {
    final res = await _dio.get('/chat/groups', options: _auth);
    return _dataList(res).map(ChatGroup.fromJson).toList();
  }

  Future<ChatGroup> createGroup(String name) async {
    final res =
        await _dio.post('/chat/groups', data: {'name': name}, options: _auth);
    return ChatGroup.fromJson(_data(res));
  }

  Future<ChatGroup> joinByCode(String code) async {
    final res = await _dio.post('/chat/groups/join',
        data: {'code': code}, options: _auth);
    return ChatGroup.fromJson(_data(res));
  }

  Future<ChatGroup> joinEventRoom(String eventId) async {
    final res =
        await _dio.post('/chat/events/$eventId/join', data: {}, options: _auth);
    return ChatGroup.fromJson(_data(res));
  }

  Future<void> leaveGroup(String groupId) async {
    await _dio.post('/chat/groups/$groupId/leave', data: {}, options: _auth);
  }

  /// Newest-first page of messages; pass [before] (a message id) to page back.
  Future<List<ChatMessageModel>> messages(
    String groupId, {
    int before = 0,
    int limit = 50,
  }) async {
    final res = await _dio.get(
      '/chat/groups/$groupId/messages',
      queryParameters: {if (before > 0) 'before': before, 'limit': limit},
      options: _auth,
    );
    return _dataList(res).map(ChatMessageModel.fromJson).toList();
  }

  /// HTTP fallback send — the WS path is primary, but this keeps sending
  /// working while the socket is (re)connecting.
  Future<ChatMessageModel> sendMessage(String groupId, String body) async {
    final res = await _dio.post('/chat/groups/$groupId/messages',
        data: {'body': body}, options: _auth);
    return ChatMessageModel.fromJson(_data(res));
  }
}
