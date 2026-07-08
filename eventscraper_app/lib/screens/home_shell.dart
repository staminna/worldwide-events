import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../state/follows.dart';
import '../state/location.dart';
import '../state/providers.dart';
import '../util/notifications.dart';
import 'home_screen.dart';
import 'map_screen.dart';

class HomeShell extends ConsumerStatefulWidget {
  const HomeShell({super.key});

  @override
  ConsumerState<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends ConsumerState<HomeShell>
    with WidgetsBindingObserver {
  int _index = 0;
  // The map hosts a native GL PlatformView — don't spin it up at app boot
  // (or in widget tests); build it the first time the tab is opened, then
  // keep it alive so camera/selection survive tab switches.
  bool _mapVisited = false;

  static const _promptedPrefKey = 'auto_locate_prompted';

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _autoLocate();
      _initAndCheckFollows();
    });
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Re-check follows when returning to the app (best-effort, foreground).
    if (state == AppLifecycleState.resumed) _checkFollows();
  }

  Future<void> _initAndCheckFollows() async {
    await initNotifications();
    await _checkFollows();
  }

  Future<void> _checkFollows() async {
    final follows = ref.read(followsProvider);
    if (follows.isEmpty) return;
    await checkFollowsAndNotify(ref.read(apiProvider), follows);
  }

  /// One-shot automatic "near me" on app launch: if we can know where the
  /// user is, open the feed on the nearest city that actually has events.
  /// Deliberately silent on every failure — no permission, no fix, backend
  /// down, plugins missing in tests — the app just stays on the global feed.
  Future<void> _autoLocate() async {
    try {
      if (ref.read(filtersProvider).cityId != null) return;

      final perm = await Geolocator.checkPermission();
      if (perm == LocationPermission.deniedForever) return;
      if (perm == LocationPermission.denied) {
        // Ask exactly once, on first launch. After that the button in the
        // app bar is the opt-in path — no nagging on every start.
        final prefs = await SharedPreferences.getInstance();
        if (prefs.getBool(_promptedPrefKey) ?? false) return;
        await prefs.setBool(_promptedPrefKey, true);
      }

      final nearest = await ref
          .read(locationProvider.notifier)
          .locate(minEvents: 3);
      if (!mounted) return;
      // The user may have picked a city while we were locating.
      if (ref.read(filtersProvider).cityId != null) return;
      ref.read(filtersProvider.notifier).setCity(nearest.city.id);
      final km = nearest.distanceKm.round();
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            km <= 30
                ? 'Showing events in ${nearest.city.name}'
                : 'Showing events in ${nearest.city.name} — '
                      'the closest covered city, $km km away',
          ),
        ),
      );
    } catch (_) {
      // Auto-locate is best-effort by design.
    }
  }

  @override
  Widget build(BuildContext context) {
    // The map's immersive fullscreen hides the bottom nav.
    final fullscreen = ref.watch(mapFullscreenProvider);
    return Scaffold(
      body: IndexedStack(
        index: _index,
        children: [
          const HomeScreen(),
          if (_mapVisited) const MapScreen() else const SizedBox.shrink(),
        ],
      ),
      bottomNavigationBar: fullscreen
          ? null
          : NavigationBar(
              selectedIndex: _index,
              onDestinationSelected: (i) => setState(() {
                _index = i;
                if (i == 1) _mapVisited = true;
              }),
              destinations: const [
                NavigationDestination(
                  icon: Icon(Icons.view_agenda_outlined),
                  selectedIcon: Icon(Icons.view_agenda),
                  label: 'Feed',
                ),
                NavigationDestination(
                  icon: Icon(Icons.map_outlined),
                  selectedIcon: Icon(Icons.map),
                  label: 'Map',
                ),
              ],
            ),
    );
  }
}
