import 'package:flutter_test/flutter_test.dart';

import 'package:eventscraper_app/api/event_api.dart';

void main() {
  group('hiResImage', () {
    test('empty input is returned unchanged', () {
      expect(hiResImage(''), '');
    });

    test('viralagenda ext -r copy is upgraded to the original', () {
      const r =
          'https://cdn.viralagenda.com/images/events/ext/1799283-18541b79d2cee892ab757769cc9d1383-r.jpg';
      expect(
        hiResImage(r),
        'https://cdn.viralagenda.com/images/events/ext/1799283-18541b79d2cee892ab757769cc9d1383.jpg',
      );
    });

    test('viralagenda -r works for .jpeg and .png too', () {
      expect(
        hiResImage('https://cdn.viralagenda.com/images/events/ext/1-a-r.jpeg'),
        'https://cdn.viralagenda.com/images/events/ext/1-a.jpeg',
      );
      expect(
        hiResImage('https://cdn.viralagenda.com/images/events/ext/2-b-r.png'),
        'https://cdn.viralagenda.com/images/events/ext/2-b.png',
      );
    });

    test('viralagenda hashed -large form has no larger variant, left as-is', () {
      const large =
          'https://cdn.viralagenda.com/images/events/25db5ab752e574964250173757bfd13f-large.jpg';
      expect(hiResImage(large), large);
    });

    test('viralagenda bare original (no -r) is left as-is', () {
      const bare =
          'https://cdn.viralagenda.com/images/events/ext/1660568-434fbbb51436d2877fbff7103f40f79b.png';
      expect(hiResImage(bare), bare);
    });

    test('eventbrite bumps width and strips crop', () {
      final out = hiResImage(
        'https://img.evbuc.com/x?w=200&h=100&rect=0,0,10,10&auto=format',
        width: 1600,
      );
      expect(out, contains('w=1600'));
      expect(out, isNot(contains('rect=')));
      expect(out, isNot(contains('h=100')));
    });

    test('luma rewrites the width segment', () {
      final out = hiResImage(
        'https://images.lumacdn.com/cdn-cgi/image/format=auto,width=400/abc',
        width: 1600,
      );
      expect(out, contains('width=1600'));
    });

    test('songkick upgrades avatar size prefix', () {
      expect(
        hiResImage('https://images.sk-static.com/images/media/large_avatar/1'),
        'https://images.sk-static.com/images/media/huge_avatar/1',
      );
    });

    test('unknown host is returned unchanged', () {
      const other = 'https://example.com/photo-r.jpg';
      expect(hiResImage(other), other);
    });
  });
}
