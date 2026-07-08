import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

/// Deep-link buttons into external navigation apps for a destination.
///
/// All links are plain https universal links, launched without `canLaunchUrl`,
/// so no Android `<queries>` / iOS `LSApplicationQueriesSchemes` config is
/// needed: each installed app claims its own domain, otherwise the browser
/// handles it.
class DirectionsButtons extends StatelessWidget {
  const DirectionsButtons({
    super.key,
    required this.lat,
    required this.lon,
    required this.label,
    this.compact = false,
  });

  final double lat;
  final double lon;

  /// Destination name, shown by apps that support one (Uber).
  final String label;

  /// Tighter chips for overlay cards (the map's selected-event card).
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final isIos = !kIsWeb && defaultTargetPlatform == TargetPlatform.iOS;
    final dest = '$lat,$lon';
    final targets = <({String name, IconData icon, Uri uri})>[
      (
        name: 'Google Maps',
        icon: Icons.map_outlined,
        uri: Uri.https('www.google.com', '/maps/dir/', {
          'api': '1',
          'destination': dest,
          'travelmode': 'walking',
        }),
      ),
      if (isIos)
        (
          name: 'Apple Maps',
          icon: Icons.map,
          uri: Uri.https('maps.apple.com', '/', {'daddr': dest, 'dirflg': 'w'}),
        ),
      (
        name: 'Waze',
        icon: Icons.navigation_outlined,
        uri: Uri.https('waze.com', '/ul', {'ll': dest, 'navigate': 'yes'}),
      ),
      (
        name: 'Uber',
        icon: Icons.local_taxi_outlined,
        uri: Uri.https('m.uber.com', '/ul/', {
          'action': 'setPickup',
          'pickup': 'my_location',
          'dropoff[latitude]': '$lat',
          'dropoff[longitude]': '$lon',
          'dropoff[nickname]': label,
        }),
      ),
    ];
    return Wrap(
      spacing: 8,
      runSpacing: compact ? 0 : 4,
      children: [
        for (final t in targets)
          ActionChip(
            avatar: Icon(t.icon, size: 16),
            label: Text(t.name),
            visualDensity: compact ? VisualDensity.compact : null,
            onPressed: () => _open(context, t.name, t.uri),
          ),
      ],
    );
  }

  Future<void> _open(BuildContext context, String name, Uri uri) async {
    final messenger = ScaffoldMessenger.of(context);
    var ok = false;
    try {
      ok = await launchUrl(uri, mode: LaunchMode.externalApplication);
    } catch (_) {}
    if (!ok) {
      messenger.showSnackBar(SnackBar(content: Text('Could not open $name')));
    }
  }
}
