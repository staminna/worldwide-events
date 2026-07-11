import 'package:eventscraper_app/state/unread.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final t0 = DateTime(2026, 7, 11, 20, 0);

  test('live session count wins regardless of read marks', () {
    expect(
      groupHasUnread(
        groupId: 'g1',
        lastMessageAt: null,
        counts: const {'g1': 2},
        readMarks: {'g1': t0},
      ),
      isTrue,
    );
  });

  test('no messages at all is never unread', () {
    expect(
      groupHasUnread(
        groupId: 'g1',
        lastMessageAt: null,
        counts: const {},
        readMarks: const {},
      ),
      isFalse,
    );
  });

  test('never-opened group with messages shows unread', () {
    expect(
      groupHasUnread(
        groupId: 'g1',
        lastMessageAt: t0,
        counts: const {},
        readMarks: const {},
      ),
      isTrue,
    );
  });

  test('message newer than the read mark is unread; older is not', () {
    expect(
      groupHasUnread(
        groupId: 'g1',
        lastMessageAt: t0.add(const Duration(minutes: 1)),
        counts: const {},
        readMarks: {'g1': t0},
      ),
      isTrue,
    );
    expect(
      groupHasUnread(
        groupId: 'g1',
        lastMessageAt: t0.subtract(const Duration(minutes: 1)),
        counts: const {},
        readMarks: {'g1': t0},
      ),
      isFalse,
    );
  });
}
