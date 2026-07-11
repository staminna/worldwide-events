import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../state/chat.dart';
import '../state/chat_identity.dart';
import '../widgets/chat_name_prompt.dart';

/// Deep-link target for eventscraper://app/join/CODE (and /join/:code):
/// makes sure a chat identity exists (name prompt on first touch), joins the
/// group by invite code, and lands in its chat.
class JoinInviteScreen extends ConsumerStatefulWidget {
  const JoinInviteScreen({super.key, required this.code});

  final String code;

  @override
  ConsumerState<JoinInviteScreen> createState() => _JoinInviteScreenState();
}

class _JoinInviteScreenState extends ConsumerState<JoinInviteScreen> {
  String? _error;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _join());
  }

  Future<void> _join() async {
    // The identity may still be loading from prefs on a cold deep-link start.
    while (ref.read(chatIdentityProvider).loading) {
      await Future<void>.delayed(const Duration(milliseconds: 100));
      if (!mounted) return;
    }
    if (!await ensureChatIdentity(context, ref)) {
      if (mounted) context.go('/');
      return;
    }
    if (!mounted) return;
    try {
      final group = await ref
          .read(chatApiProvider)
          .joinByCode(widget.code.trim().toUpperCase());
      ref.invalidate(groupsProvider);
      ref.read(chatConnectionProvider)
        ..ensureConnected()
        ..subscribeGroup(group.id);
      if (mounted) {
        // Deep links replace the whole stack with this screen, so rebuild a
        // sane one: home shell underneath, the group chat on top — Android
        // back then returns into the app instead of exiting it.
        context.go('/');
        context.push('/group/${group.id}', extra: group);
      }
    } catch (_) {
      if (mounted) {
        setState(() => _error = 'No group found for code "${widget.code}" — '
            'it may have been deleted.');
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Join group')),
      body: Center(
        child: _error == null
            ? const Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  CircularProgressIndicator(),
                  SizedBox(height: 16),
                  Text('Joining…'),
                ],
              )
            : Padding(
                padding: const EdgeInsets.all(32),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(_error!, textAlign: TextAlign.center),
                    const SizedBox(height: 16),
                    FilledButton(
                      onPressed: () => context.go('/'),
                      child: const Text('Back to events'),
                    ),
                  ],
                ),
              ),
      ),
    );
  }
}
