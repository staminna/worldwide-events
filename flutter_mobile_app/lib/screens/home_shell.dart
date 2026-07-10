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
  /// The feed's first fetch waits behind [initialCityResolvedProvider], so
  /// it fires exactly once with the resolved city: the city persisted from
  /// the last launch opens the gate immediately, otherwise the locate
  /// attempt (bounded, with a last-known-position fallback inside
  /// [LocationNotifier]) does. Deliberately silent on every failure — no
  /// permission, no fix, backend down, plugins missing in tests — the app
  /// just stays on the saved or global feed.
  Future<void> _autoLocate() async {
    String? saved;
    try {
      if (ref.read(filtersProvider).cityId != null) return;

      // Seed from the previous launch so the first (only) feed fetch already
      // has the right city; the locate below just corrects a real move.
      final prefs = await SharedPreferences.getInstance();
      saved = prefs.getString(lastCityPrefKey);
      if (saved != null) {
        ref.read(filtersProvider.notifier).setCity(saved);
        _openFeedGate();
      }

      final perm = await Geolocator.checkPermission();
      if (perm == LocationPermission.deniedForever) return;
      if (perm == LocationPermission.denied) {
        // Ask exactly once, on first launch. After that the button in the
        // app bar is the opt-in path — no nagging on every start.
        if (prefs.getBool(_promptedPrefKey) ?? false) return;
        await prefs.setBool(_promptedPrefKey, true);
      }

      // Short fix timeout so the last-known-position fallback still fits
      // inside the overall cap the feed may be waiting behind.
      final nearest = await ref
          .read(locationProvider.notifier)
          .locate(minEvents: 3, fixTimeout: const Duration(seconds: 6))
          .timeout(const Duration(seconds: 12));
      if (!mounted) return;
      final current = ref.read(filtersProvider).cityId;
      // Already showing the located city — a redundant setCity would still
      // rebuild the feed, which is exactly the flicker this avoids.
      if (current == nearest.city.id) return;
      // The user picked a different city while we were locating.
      if (current != null && current != saved) return;
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
    } finally {
      _openFeedGate();
    }
  }

  void _openFeedGate() {
    if (!mounted) return;
    ref.read(initialCityResolvedProvider.notifier).state = true;
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
