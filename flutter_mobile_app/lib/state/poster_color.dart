import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:palette_generator/palette_generator.dart';

import '../api/event_api.dart';

/// A representative accent color pulled from an event's poster, used to tint
/// the card accent and the detail hero. Computed from a downscaled copy so
/// it's cheap, and cached per image URL for the session (family providers are
/// kept alive), so scrolling a card back into view doesn't recompute it.
///
/// Resolves to null when there's no image or no palette can be derived —
/// callers fall back to the event's category color.
final posterColorProvider = FutureProvider.family<Color?, String>((
  ref,
  imageUrl,
) async {
  if (imageUrl.isEmpty) return null;
  try {
    final palette = await PaletteGenerator.fromImageProvider(
      CachedNetworkImageProvider(proxiedImage(imageUrl)),
      size: const Size(120, 120),
      maximumColorCount: 8,
    );
    return palette.vibrantColor?.color ??
        palette.dominantColor?.color ??
        palette.mutedColor?.color;
  } catch (_) {
    // Decode/network failure — just fall back to the category color.
    return null;
  }
});
