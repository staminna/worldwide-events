/// Chat + live-location models mirroring the backend's /chat REST shapes and
/// the WebSocket envelope (see eventscraper_go/internal/chat/envelope.go).
library;

/// The on-device anonymous identity: a server-issued id and bearer token,
/// persisted in shared_preferences. Possession of the token IS the identity.
class ChatIdentity {
  const ChatIdentity({required this.id, required this.name, required this.token});

  final String id;
  final String name;
  final String token;

  factory ChatIdentity.fromJson(Map<String, dynamic> json) => ChatIdentity(
        id: json['id'] as String,
        name: json['name'] as String,
        token: json['token'] as String,
      );

  Map<String, dynamic> toJson() => {'id': id, 'name': name, 'token': token};
}

/// A chat room: the public room of an event or a private invite-code group.
class ChatGroup {
  const ChatGroup({
    required this.id,
    required this.type,
    required this.name,
    this.eventId = '',
    this.inviteCode = '',
    this.memberCount = 0,
    this.lastMessage = '',
    this.lastMessageAt,
  });

  final String id;
  final String type; // 'event' | 'private'
  final String name;
  final String eventId;
  final String inviteCode;
  final int memberCount;
  final String lastMessage;
  final DateTime? lastMessageAt;

  bool get isEventRoom => type == 'event';

  factory ChatGroup.fromJson(Map<String, dynamic> json) => ChatGroup(
        id: json['id'] as String,
        type: json['type'] as String? ?? 'private',
        name: json['name'] as String? ?? '',
        eventId: json['eventId'] as String? ?? '',
        inviteCode: json['inviteCode'] as String? ?? '',
        memberCount: json['memberCount'] as int? ?? 0,
        lastMessage: json['lastMessage'] as String? ?? '',
        lastMessageAt: json['lastMessageAt'] != null
            ? DateTime.tryParse(json['lastMessageAt'] as String)
            : null,
      );
}

/// One chat message. Server messages carry a positive [id]; optimistic local
/// sends carry id 0 plus a [clientRef] until the server echo reconciles them.
class ChatMessageModel {
  const ChatMessageModel({
    required this.id,
    required this.groupId,
    required this.userId,
    required this.name,
    required this.kind,
    required this.body,
    required this.createdAt,
    this.clientRef = '',
    this.pending = false,
    this.failed = false,
  });

  final int id;
  final String groupId;
  final String userId;
  final String name;
  final String kind; // 'text' | 'system'
  final String body;
  final DateTime createdAt;
  final String clientRef;
  final bool pending;
  final bool failed;

  /// Stable identity for UI lists: locally-originated messages keep their
  /// clientRef even after the server assigns an id, so reconciliation is an
  /// update, not a remove+insert.
  String get uiKey => clientRef.isNotEmpty ? clientRef : 'm$id';

  ChatMessageModel copyWith({int? id, bool? pending, bool? failed, DateTime? createdAt}) =>
      ChatMessageModel(
        id: id ?? this.id,
        groupId: groupId,
        userId: userId,
        name: name,
        kind: kind,
        body: body,
        createdAt: createdAt ?? this.createdAt,
        clientRef: clientRef,
        pending: pending ?? this.pending,
        failed: failed ?? this.failed,
      );

  factory ChatMessageModel.fromJson(Map<String, dynamic> json) => ChatMessageModel(
        id: (json['id'] as num?)?.toInt() ?? 0,
        groupId: json['groupId'] as String? ?? '',
        userId: json['userId'] as String? ?? '',
        name: json['name'] as String? ?? '?',
        kind: json['kind'] as String? ?? 'text',
        body: json['body'] as String? ?? '',
        createdAt:
            DateTime.tryParse(json['createdAt'] as String? ?? '')?.toLocal() ??
                DateTime.now(),
      );
}

/// A group member's last known shared position, rendered on the map.
class PeerFix {
  const PeerFix({
    required this.userId,
    required this.name,
    required this.groupId,
    required this.lat,
    required this.lon,
    this.acc = 0,
    required this.at,
  });

  final String userId;
  final String name;
  final String groupId;
  final double lat;
  final double lon;
  final double acc;
  final DateTime at;
}

/// The WebSocket wire envelope, both directions, discriminated by [type].
class WsEnvelope {
  const WsEnvelope({
    required this.type,
    this.groupId = '',
    this.id = 0,
    this.userId = '',
    this.name = '',
    this.kind = '',
    this.body = '',
    this.clientRef = '',
    this.createdAt = '',
    this.lat = 0,
    this.lon = 0,
    this.acc = 0,
    this.at = '',
    this.online = const [],
    this.sharing = const [],
    this.code = '',
    this.message = '',
  });

  final String type;
  final String groupId;
  final int id;
  final String userId;
  final String name;
  final String kind;
  final String body;
  final String clientRef;
  final String createdAt;
  final double lat;
  final double lon;
  final double acc;
  final String at;
  final List<String> online;
  final List<Map<String, dynamic>> sharing;
  final String code;
  final String message;

  factory WsEnvelope.fromJson(Map<String, dynamic> json) => WsEnvelope(
        type: json['type'] as String? ?? '',
        groupId: json['groupId'] as String? ?? '',
        id: (json['id'] as num?)?.toInt() ?? 0,
        userId: json['userId'] as String? ?? '',
        name: json['name'] as String? ?? '',
        kind: json['kind'] as String? ?? '',
        body: json['body'] as String? ?? '',
        clientRef: json['clientRef'] as String? ?? '',
        createdAt: json['createdAt'] as String? ?? '',
        lat: (json['lat'] as num?)?.toDouble() ?? 0,
        lon: (json['lon'] as num?)?.toDouble() ?? 0,
        acc: (json['acc'] as num?)?.toDouble() ?? 0,
        at: json['at'] as String? ?? '',
        online: (json['online'] as List?)?.cast<String>() ?? const [],
        sharing:
            (json['sharing'] as List?)?.cast<Map<String, dynamic>>() ?? const [],
        code: json['code'] as String? ?? '',
        message: json['message'] as String? ?? '',
      );

  /// Encodes only what client→server frames use; server-only fields stay out.
  Map<String, dynamic> toJson() => {
        'type': type,
        if (groupId.isNotEmpty) 'groupId': groupId,
        if (body.isNotEmpty) 'body': body,
        if (clientRef.isNotEmpty) 'clientRef': clientRef,
        if (type == 'location') ...{'lat': lat, 'lon': lon, 'acc': acc},
      };

  ChatMessageModel toMessage() => ChatMessageModel(
        id: id,
        groupId: groupId,
        userId: userId,
        name: name,
        kind: kind.isEmpty ? 'text' : kind,
        body: body,
        clientRef: clientRef,
        createdAt: DateTime.tryParse(createdAt)?.toLocal() ?? DateTime.now(),
      );
}
