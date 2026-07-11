import 'package:flutter_local_notifications/flutter_local_notifications.dart';

final _plugin = FlutterLocalNotificationsPlugin();
bool _inited = false;

/// Initializes local notifications and asks for the runtime permission
/// (Android 13+/iOS). Safe to call more than once. Best-effort — never throws
/// into the caller.
Future<void> initNotifications() async {
  if (_inited) return;
  try {
    const settings = InitializationSettings(
      android: AndroidInitializationSettings('@mipmap/ic_launcher'),
      iOS: DarwinInitializationSettings(),
    );
    await _plugin.initialize(settings);
    await _plugin
        .resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin
        >()
        ?.requestNotificationsPermission();
    await _plugin
        .resolvePlatformSpecificImplementation<
          IOSFlutterLocalNotificationsPlugin
        >()
        ?.requestPermissions(alert: true, badge: true, sound: true);
    _inited = true;
  } catch (_) {
    // Notifications are a nice-to-have; a failed init must not break launch.
  }
}

/// Posts a single local notification about followed events.
Future<void> showFollowNotification({
  required int id,
  required String title,
  required String body,
}) async {
  try {
    const details = NotificationDetails(
      android: AndroidNotificationDetails(
        'follows',
        'Followed events',
        channelDescription: 'New events matching things you follow',
        importance: Importance.defaultImportance,
        priority: Priority.defaultPriority,
      ),
      iOS: DarwinNotificationDetails(),
    );
    await _plugin.show(id, title, body, details);
  } catch (_) {
    // Ignore — e.g. permission denied.
  }
}
