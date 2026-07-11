import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';

import '../models/chat.dart';
import '../state/chat.dart';
import '../state/chat_identity.dart';
import '../widgets/chat_name_prompt.dart';

/// The Groups tab: the user's chat rooms (event rooms + private groups),
/// with create / join-by-code entry points.
class GroupsScreen extends ConsumerStatefulWidget {
  const GroupsScreen({super.key});

  @override
  ConsumerState<GroupsScreen> createState() => _GroupsScreenState();
}

class _GroupsScreenState extends ConsumerState<GroupsScreen> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (ref.read(chatIdentityProvider).registered) {
        ref.read(chatConnectionProvider).ensureConnected();
      }
    });
  }

  Future<void> _openGroup(ChatGroup group) async {
    ref.read(chatConnectionProvider).ensureConnected();
    await context.push('/group/${group.id}', extra: group);
    if (mounted) ref.invalidate(groupsProvider);
  }

  Future<void> _createGroup() async {
    if (!await ensureChatIdentity(context, ref)) return;
    if (!mounted) return;
    final name = await _promptText(
      title: 'New group',
      label: 'Group name',
      hint: 'e.g. Friday night crew',
      confirm: 'Create',
    );
    if (name == null || name.trim().isEmpty) return;
    try {
      final group = await ref.read(chatApiProvider).createGroup(name.trim());
      ref.invalidate(groupsProvider);
      ref.read(chatConnectionProvider).subscribeGroup(group.id);
      if (mounted) _openGroup(group);
    } catch (_) {
      _toast('Could not create the group');
    }
  }

  Future<void> _joinByCode() async {
    if (!await ensureChatIdentity(context, ref)) return;
    if (!mounted) return;
    final code = await _promptText(
      title: 'Join a group',
      label: 'Invite code',
      hint: '6 characters, e.g. XK42MP',
      confirm: 'Join',
      upperCase: true,
    );
    if (code == null || code.trim().isEmpty) return;
    try {
      final group =
          await ref.read(chatApiProvider).joinByCode(code.trim().toUpperCase());
      ref.invalidate(groupsProvider);
      ref.read(chatConnectionProvider).subscribeGroup(group.id);
      if (mounted) _openGroup(group);
    } catch (_) {
      _toast('No group found for that code');
    }
  }

  Future<String?> _promptText({
    required String title,
    required String label,
    required String hint,
    required String confirm,
    bool upperCase = false,
  }) {
    final controller = TextEditingController();
    return showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(title),
        content: TextField(
          controller: controller,
          autofocus: true,
          textCapitalization:
              upperCase ? TextCapitalization.characters : TextCapitalization.sentences,
          inputFormatters:
              upperCase ? [UpperCaseTextFormatter()] : const [],
          decoration: InputDecoration(
            labelText: label,
            hintText: hint,
            border: const OutlineInputBorder(),
          ),
          onSubmitted: (v) => Navigator.of(ctx).pop(v),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(controller.text),
            child: Text(confirm),
          ),
        ],
      ),
    );
  }

  void _toast(String msg) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
  }

  @override
  Widget build(BuildContext context) {
    final identity = ref.watch(chatIdentityProvider);

    return Scaffold(
      appBar: AppBar(
        title: const Text('Groups'),
        actions: [
          if (identity.identity != null)
            Padding(
              padding: const EdgeInsets.only(right: 16),
              child: Center(
                child: Text(
                  identity.identity!.name,
                  style: Theme.of(context).textTheme.labelLarge?.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                ),
              ),
            ),
        ],
      ),
      body: identity.loading
          ? const Center(child: CircularProgressIndicator())
          : identity.registered
              ? _GroupsList(onOpen: _openGroup)
              : _Welcome(onStart: () async {
                  if (await ensureChatIdentity(context, ref)) {
                    ref.read(chatConnectionProvider).ensureConnected();
                  }
                }),
      floatingActionButton: FloatingActionButton(
        onPressed: () => showModalBottomSheet<void>(
          context: context,
          showDragHandle: true,
          builder: (ctx) => SafeArea(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                ListTile(
                  leading: const Icon(Icons.group_add_outlined),
                  title: const Text('Create a group'),
                  subtitle:
                      const Text('Get an invite code to share with friends'),
                  onTap: () {
                    Navigator.of(ctx).pop();
                    _createGroup();
                  },
                ),
                ListTile(
                  leading: const Icon(Icons.key_outlined),
                  title: const Text('Join with a code'),
                  subtitle: const Text('Enter a 6-character invite code'),
                  onTap: () {
                    Navigator.of(ctx).pop();
                    _joinByCode();
                  },
                ),
              ],
            ),
          ),
        ),
        child: const Icon(Icons.add),
      ),
    );
  }
}

class _Welcome extends StatelessWidget {
  const _Welcome({required this.onStart});

  final VoidCallback onStart;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.forum_outlined, size: 64, color: cs.primary),
            const SizedBox(height: 16),
            Text('Chat about events, find your friends',
                style: Theme.of(context).textTheme.titleLarge,
                textAlign: TextAlign.center),
            const SizedBox(height: 8),
            Text(
              'Join an event’s room, make private groups, and share your '
              'live location on the map while you’re out.',
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: cs.onSurfaceVariant,
                  ),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 24),
            FilledButton.icon(
              onPressed: onStart,
              icon: const Icon(Icons.chat_bubble_outline),
              label: const Text('Pick a name to start'),
            ),
          ],
        ),
      ),
    );
  }
}

class _GroupsList extends ConsumerWidget {
  const _GroupsList({required this.onOpen});

  final void Function(ChatGroup) onOpen;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final groups = ref.watch(groupsProvider);
    return groups.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (_, __) => Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text('Could not load your groups'),
            const SizedBox(height: 8),
            OutlinedButton(
              onPressed: () => ref.invalidate(groupsProvider),
              child: const Text('Retry'),
            ),
          ],
        ),
      ),
      data: (list) {
        if (list.isEmpty) {
          return Center(
            child: Padding(
              padding: const EdgeInsets.all(32),
              child: Text(
                'No groups yet.\nOpen an event and tap "Event chat", or '
                'create a group with the + button.',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
              ),
            ),
          );
        }
        return RefreshIndicator(
          onRefresh: () async => ref.invalidate(groupsProvider),
          child: ListView.builder(
            physics: const AlwaysScrollableScrollPhysics(),
            itemCount: list.length,
            itemBuilder: (_, i) => _GroupTile(group: list[i], onOpen: onOpen),
          ),
        );
      },
    );
  }
}

class _GroupTile extends StatelessWidget {
  const _GroupTile({required this.group, required this.onOpen});

  final ChatGroup group;
  final void Function(ChatGroup) onOpen;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final when = group.lastMessageAt;
    return ListTile(
      leading: CircleAvatar(
        backgroundColor:
            group.isEventRoom ? cs.tertiaryContainer : cs.primaryContainer,
        child: Icon(
          group.isEventRoom ? Icons.local_activity_outlined : Icons.group_outlined,
          color: group.isEventRoom ? cs.onTertiaryContainer : cs.onPrimaryContainer,
        ),
      ),
      title: Text(group.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: Text(
        group.lastMessage.isEmpty ? 'No messages yet' : group.lastMessage,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      trailing: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          if (when != null)
            Text(
              _relative(when),
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    color: cs.onSurfaceVariant,
                  ),
            ),
          const SizedBox(height: 4),
          Text(
            '${group.memberCount} ${group.memberCount == 1 ? 'member' : 'members'}',
            style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: cs.onSurfaceVariant,
                ),
          ),
        ],
      ),
      onTap: () => onOpen(group),
    );
  }

  String _relative(DateTime t) {
    final now = DateTime.now();
    final diff = now.difference(t);
    if (diff.inMinutes < 1) return 'now';
    if (diff.inHours < 1) return '${diff.inMinutes}m';
    if (diff.inDays < 1) return '${diff.inHours}h';
    if (diff.inDays < 7) return '${diff.inDays}d';
    return DateFormat.Md().format(t);
  }
}

/// Forces invite-code input to uppercase as the user types.
class UpperCaseTextFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(
      TextEditingValue oldValue, TextEditingValue newValue) {
    return newValue.copyWith(text: newValue.text.toUpperCase());
  }
}
