import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:google_nav_bar/google_nav_bar.dart';

import '../state/unread.dart';

/// Which HomeShell tab is active (0 Feed, 1 Map, 2 Groups). Lives in a
/// provider (not shell-local state) so screens hosting the nav bar outside
/// the shell — like the group chat screen — can switch tabs too.
final shellTabProvider = StateProvider<int>((ref) => 0);

/// The app's bottom navigation: a full-width white bar with rounded top
/// corners above the footer, high-contrast black icons, and a seed-purple
/// active pill. Shared by HomeShell and any screen where the menu must stay
/// visible (group chat).
class AppNavBar extends ConsumerWidget {
  const AppNavBar({super.key, this.onSelected});

  /// Called after the tab provider updates — HomeShell uses it to mark the
  /// map visited; other hosts use it to navigate back to the shell.
  final void Function(int index)? onSelected;

  static const seedPurple = Color(0xFF6750A4);

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final index = ref.watch(shellTabProvider);
    final unread = ref.watch(hasAnyUnreadProvider);

    return Container(
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: const BorderRadius.vertical(top: Radius.circular(24)),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.35),
            blurRadius: 18,
            offset: const Offset(0, -4),
          ),
        ],
      ),
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
      child: SafeArea(
        top: false,
        child: GNav(
          selectedIndex: index,
          onTabChange: (i) {
            ref.read(shellTabProvider.notifier).state = i;
            onSelected?.call(i);
          },
          gap: 8,
          // High contrast on the white bar: near-black inactive icons,
          // white-on-purple active pill.
          color: Colors.black87,
          activeColor: Colors.white,
          rippleColor: Colors.black12,
          tabBackgroundColor: seedPurple,
          tabBorderRadius: 18,
          iconSize: 24,
          duration: const Duration(milliseconds: 250),
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
          tabs: [
            const GButton(icon: Icons.view_agenda_outlined, text: 'Feed'),
            const GButton(icon: Icons.map_outlined, text: 'Map'),
            GButton(
              icon: Icons.forum_outlined,
              text: 'Groups',
              // leading replaces the icon, so mirror the active-state
              // colors GNav would apply itself.
              leading: Badge(
                isLabelVisible: unread,
                smallSize: 8,
                child: Icon(
                  Icons.forum_outlined,
                  size: 24,
                  color: index == 2 ? Colors.white : Colors.black87,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
