import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:eventscraper_app/main.dart';

void main() {
  testWidgets('app boots and shows home title', (tester) async {
    await tester.pumpWidget(const ProviderScope(child: EventScraperApp()));
    await tester.pump();
    expect(find.text('Upcoming Events'), findsOneWidget);
  });
}
