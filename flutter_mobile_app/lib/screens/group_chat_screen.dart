import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_chat_core/flutter_chat_core.dart' as flyer;
import 'package:flutter_chat_ui/flutter_chat_ui.dart' as flyer_ui;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../models/chat.dart';
import '../state/chat.dart';
import '../state/chat_identity.dart';
import '../state/location_share.dart';

/// One group's conversation. The flyer_chat Chat widget renders; our Riverpod
/// GroupMessagesNotifier stays the single source of truth and is mirrored
/// into the widget's InMemoryChatController (one-way sync).
class GroupChatScreen extends ConsumerStatefulWidget {
  const GroupChatScreen({super.key, required this.groupId, this.group});

  final String groupId;

  /// Passed via GoRouter extra when navigating from a list; when absent
  /// (e.g. hot restart deep on this route) we fall back to groupsProvider.
  final ChatGroup? group;

  @override
  ConsumerState<GroupChatScreen> createState() => _GroupChatScreenState();
}

class _GroupChatScreenState extends ConsumerState<GroupChatScreen> {
  final _controller = flyer.InMemoryChatController();

  /// userId → display name, learned from every message that passes by; feeds
  /// resolveUser so flyer can label bubbles/avatars.
  final _names = <String, String>{};

  @override
  void initState() {
    super.initState();
    ref.read(chatConnectionProvider).ensureConnected();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  flyer.Message _toFlyer(ChatMessageModel m) {
    _names[m.userId] = m.name;
    if (m.kind == 'system') {
      return flyer.Message.system(
        id: m.uiKey,
        authorId: m.userId,
        createdAt: m.createdAt.toUtc(),
        text: m.body,
      );
    }
    return flyer.Message.text(
      id: m.uiKey,
      authorId: m.userId,
      createdAt: m.createdAt.toUtc(),
      text: m.body,
      status: m.failed
          ? flyer.MessageStatus.error
          : m.pending
              ? flyer.MessageStatus.sending
              : flyer.MessageStatus.sent,
    );
  }

  /// Mirrors provider state into the controller: appends and in-place
  /// updates are incremental; a prepend (older page) rebuilds the list.
  void _sync(List<ChatMessageModel> models) {
    final existing = {for (final m in _controller.messages) m.id: m};
    final target = models.map(_toFlyer).toList();

    final prepended = target.isNotEmpty &&
        _controller.messages.isNotEmpty &&
        !existing.containsKey(target.first.id);
    if (prepended) {
      _controller.setMessages(target);
      return;
    }
    for (final msg in target) {
      final old = existing[msg.id];
      if (old == null) {
        _controller.insertMessage(msg);
      } else if (old != msg) {
        _controller.updateMessage(old, msg);
      }
    }
  }

  Future<void> _toggleShare(ChatGroup group) async {
    final share = ref.read(locationShareProvider.notifier);
    if (share.isSharing(widget.groupId)) {
      share.stop(widget.groupId);
      return;
    }
    final ok = await share.start(widget.groupId);
    if (!mounted) return;
    if (!ok) {
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
        content: Text('Location permission is needed to share where you are'),
      ));
      return;
    }
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(
      content: Text('Sharing your live location with "${group.name}" — '
          'watch the Map tab for your friends'),
    ));
  }

  Future<void> _leave(ChatGroup group) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Leave "${group.name}"?'),
        content: const Text('You can rejoin later with the invite code or from the event.'),
        actions: [
          TextButton(
              onPressed: () => Navigator.of(ctx).pop(false),
              child: const Text('Cancel')),
          FilledButton(
              onPressed: () => Navigator.of(ctx).pop(true),
              child: const Text('Leave')),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    ref.read(locationShareProvider.notifier).stop(widget.groupId);
    try {
      await ref.read(chatApiProvider).leaveGroup(widget.groupId);
    } catch (_) {}
    if (!mounted) return;
    ref.invalidate(groupsProvider);
    context.pop();
  }

  void _copyInvite(ChatGroup group) {
    Clipboard.setData(ClipboardData(text: group.inviteCode));
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(
      content: Text('Invite code ${group.inviteCode} copied — send it to your friends'),
    ));
  }

  @override
  Widget build(BuildContext context) {
    final identity = ref.watch(chatIdentityProvider).identity;
    final messagesState = ref.watch(groupMessagesProvider(widget.groupId));
    final sharing = ref.watch(locationShareProvider).contains(widget.groupId);
    final group = widget.group ??
        ref
            .watch(groupsProvider)
            .valueOrNull
            ?.where((g) => g.id == widget.groupId)
            .firstOrNull ??
        ChatGroup(id: widget.groupId, type: 'private', name: 'Chat');

    ref.listen(groupMessagesProvider(widget.groupId), (_, next) {
      _sync(next.messages);
    });

    if (identity == null) {
      // Shouldn't happen (all entry points register first), but don't crash.
      return Scaffold(
        appBar: AppBar(title: Text(group.name)),
        body: const Center(child: Text('No chat identity')),
      );
    }

    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(group.name, maxLines: 1, overflow: TextOverflow.ellipsis),
            if (group.memberCount > 0)
              Text(
                '${group.memberCount} ${group.memberCount == 1 ? 'member' : 'members'}',
                style: theme.textTheme.labelSmall
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: sharing ? 'Stop sharing location' : 'Share my live location',
            icon: Icon(
              sharing ? Icons.location_on : Icons.location_on_outlined,
              color: sharing ? theme.colorScheme.tertiary : null,
            ),
            onPressed: () => _toggleShare(group),
          ),
          if (!group.isEventRoom && group.inviteCode.isNotEmpty)
            IconButton(
              tooltip: 'Copy invite code',
              icon: const Icon(Icons.person_add_alt_outlined),
              onPressed: () => _copyInvite(group),
            ),
          PopupMenuButton<String>(
            onSelected: (v) {
              if (v == 'leave') _leave(group);
            },
            itemBuilder: (_) => const [
              PopupMenuItem(value: 'leave', child: Text('Leave group')),
            ],
          ),
        ],
        bottom: sharing
            ? PreferredSize(
                preferredSize: const Size.fromHeight(32),
                child: Container(
                  height: 32,
                  padding: const EdgeInsets.symmetric(horizontal: 12),
                  color: theme.colorScheme.tertiaryContainer,
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Icon(Icons.podcasts,
                          size: 16, color: theme.colorScheme.onTertiaryContainer),
                      const SizedBox(width: 8),
                      Flexible(
                        child: Text(
                          'Live location on — visible to this group',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.labelSmall?.copyWith(
                              color: theme.colorScheme.onTertiaryContainer),
                        ),
                      ),
                      TextButton(
                        style: TextButton.styleFrom(
                          visualDensity: VisualDensity.compact,
                          padding: const EdgeInsets.symmetric(horizontal: 8),
                        ),
                        onPressed: () =>
                            ref.read(locationShareProvider.notifier).stop(widget.groupId),
                        child: const Text('Stop'),
                      ),
                    ],
                  ),
                ),
              )
            : null,
      ),
      body: messagesState.loading && messagesState.messages.isEmpty
          ? const Center(child: CircularProgressIndicator())
          : messagesState.error.isNotEmpty && messagesState.messages.isEmpty
              ? Center(child: Text(messagesState.error))
              : flyer_ui.Chat(
                  currentUserId: identity.id,
                  chatController: _controller,
                  resolveUser: (id) async => flyer.User(id: id, name: _names[id]),
                  theme: flyer.ChatTheme.fromThemeData(theme),
                  onMessageSend: (text) => ref
                      .read(groupMessagesProvider(widget.groupId).notifier)
                      .sendText(text),
                  builders: flyer.Builders(
                    chatAnimatedListBuilder: (context, itemBuilder) =>
                        flyer_ui.ChatAnimatedList(
                      itemBuilder: itemBuilder,
                      onEndReached: () => ref
                          .read(groupMessagesProvider(widget.groupId).notifier)
                          .loadOlder(),
                    ),
                  ),
                ),
    );
  }
}
