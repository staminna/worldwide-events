import 'package:url_launcher/url_launcher.dart';

import '../models/event.dart';

/// Opens the Google Calendar "create event" screen pre-filled from [event].
///
/// Uses a template URL (via url_launcher) instead of a native calendar plugin
/// so it works on Android and web with zero extra platform config. Falls back
/// to a 2-hour duration when the event has no end time.
Future<bool> addEventToCalendar(Event event) async {
  final start = event.startsAt.toUtc();
  final end = (event.endsAt ?? event.startsAt.add(const Duration(hours: 2)))
      .toUtc();

  String stamp(DateTime d) {
    String p(int n, [int w = 2]) => n.toString().padLeft(w, '0');
    return '${p(d.year, 4)}${p(d.month)}${p(d.day)}'
        'T${p(d.hour)}${p(d.minute)}${p(d.second)}Z';
  }

  final location = [
    if (event.venue.name.isNotEmpty) event.venue.name,
    if (event.venue.address.isNotEmpty) event.venue.address,
    if (event.city.isNotEmpty) event.city,
  ].join(', ');

  final details = [
    if (event.description.isNotEmpty) event.description,
    if (event.url.isNotEmpty) event.url,
  ].join('\n\n');

  final uri = Uri.https('calendar.google.com', '/calendar/render', {
    'action': 'TEMPLATE',
    'text': event.title,
    'dates': '${stamp(start)}/${stamp(end)}',
    if (location.isNotEmpty) 'location': location,
    if (details.isNotEmpty) 'details': details,
  });

  return launchUrl(uri, mode: LaunchMode.externalApplication);
}
