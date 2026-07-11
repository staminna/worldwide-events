import 'package:flutter/material.dart';

import '../models/event.dart';

/// Single source of truth for how a category is colored and iconed across
/// the app (feed chips, map pins, detail screen).
Color categoryColor(ColorScheme cs, EventCategory c) => switch (c) {
  EventCategory.tech => cs.primary,
  EventCategory.music => cs.secondary,
  EventCategory.arts => const Color(
    0xFFB0578D,
  ), // magenta, distinct from the scheme trio
  EventCategory.business => cs.tertiary,
  EventCategory.unknown => cs.outline,
};

IconData categoryIcon(EventCategory c) => switch (c) {
  EventCategory.tech => Icons.code,
  EventCategory.music => Icons.music_note,
  EventCategory.arts => Icons.palette,
  EventCategory.business => Icons.business_center,
  EventCategory.unknown => Icons.place,
};
