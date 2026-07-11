import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../state/chat_identity.dart';

/// Makes sure the user has a chat identity, prompting for a display name on
/// first touch. Returns true when an identity exists afterwards (either it
/// already did, or registration succeeded).
Future<bool> ensureChatIdentity(BuildContext context, WidgetRef ref) async {
  if (ref.read(chatIdentityProvider).registered) return true;
  final name = await showDialog<String>(
    context: context,
    builder: (_) => const _NamePromptDialog(),
  );
  if (name == null || name.trim().isEmpty) return false;
  try {
    await ref.read(chatIdentityProvider.notifier).ensureRegistered(name);
    return true;
  } catch (_) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Could not register — check your connection')),
      );
    }
    return false;
  }
}

class _NamePromptDialog extends StatefulWidget {
  const _NamePromptDialog();

  @override
  State<_NamePromptDialog> createState() => _NamePromptDialogState();
}

class _NamePromptDialogState extends State<_NamePromptDialog> {
  final _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _submit() {
    final name = _controller.text.trim();
    if (name.isEmpty) return;
    Navigator.of(context).pop(name);
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Pick a display name'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'This is how you appear in chats and on the map. '
            'No account needed — the name stays on this device.',
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _controller,
            autofocus: true,
            maxLength: 32,
            textCapitalization: TextCapitalization.words,
            decoration: const InputDecoration(
              labelText: 'Display name',
              border: OutlineInputBorder(),
            ),
            onSubmitted: (_) => _submit(),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(onPressed: _submit, child: const Text('Start chatting')),
      ],
    );
  }
}
