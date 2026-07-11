import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/event.dart';
import '../state/saved_events.dart';

/// Bookmark toggle shared by the card image overlay and the detail screen.
/// Set [onImagery] when placed over a photo — it draws on a translucent
/// circle with a white icon so it stays legible on any background.
class SaveButton extends ConsumerWidget {
  const SaveButton({super.key, required this.event, this.onImagery = false});

  final Event event;
  final bool onImagery;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final saved = ref.watch(isSavedProvider(event.id));
    final cs = Theme.of(context).colorScheme;

    final button = IconButton(
      visualDensity: VisualDensity.compact,
      tooltip: saved ? 'Remove from agenda' : 'Save to agenda',
      icon: Icon(
        saved ? Icons.bookmark : Icons.bookmark_border,
        color: onImagery ? Colors.white : (saved ? cs.primary : null),
      ),
      onPressed: () {
        ref.read(savedEventsProvider.notifier).toggle(event);
        ScaffoldMessenger.of(context)
          ..clearSnackBars()
          ..showSnackBar(
            SnackBar(
              duration: const Duration(seconds: 2),
              content: Text(
                saved ? 'Removed from your agenda' : 'Saved to your agenda',
              ),
            ),
          );
      },
    );

    if (!onImagery) return button;
    return DecoratedBox(
      decoration: const BoxDecoration(
        color: Colors.black45,
        shape: BoxShape.circle,
      ),
      child: button,
    );
  }
}
