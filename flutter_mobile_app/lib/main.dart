import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'models/chat.dart';
import 'screens/add_event_screen.dart';
import 'screens/agenda_screen.dart';
import 'screens/event_detail.dart';
import 'screens/group_chat_screen.dart';
import 'screens/home_shell.dart';
import 'screens/join_invite_screen.dart';

void main() {
  runApp(const ProviderScope(child: EventScraperApp()));
}

final _router = GoRouter(
  routes: [
    GoRoute(path: '/', builder: (_, __) => const HomeShell()),
    GoRoute(path: '/agenda', builder: (_, __) => const AgendaScreen()),
    GoRoute(path: '/add', builder: (_, __) => const AddEventScreen()),
    GoRoute(
      path: '/event/:id',
      builder: (_, state) =>
          EventDetailScreen(eventId: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/group/:id',
      builder: (_, state) => GroupChatScreen(
        groupId: state.pathParameters['id']!,
        group: state.extra as ChatGroup?,
      ),
    ),
    // Deep-link target for shared invites (eventscraper://app/join/CODE).
    GoRoute(
      path: '/join/:code',
      builder: (_, state) =>
          JoinInviteScreen(code: state.pathParameters['code']!),
    ),
  ],
);

class EventScraperApp extends StatelessWidget {
  const EventScraperApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      title: 'Worldwide Events',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF6750A4)),
        useMaterial3: true,
      ),
      darkTheme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF6750A4),
          brightness: Brightness.dark,
        ),
        useMaterial3: true,
      ),
      routerConfig: _router,
    );
  }
}
