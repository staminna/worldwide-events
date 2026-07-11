import 'package:flutter_test/flutter_test.dart';

import 'package:eventscraper_app/models/event.dart';
import 'package:eventscraper_app/util/nl_query.dart';

const _cities = [
  City(id: 'lisbon', name: 'Lisbon', country: 'PT', lat: 38.7, lon: -9.1),
  City(id: 'porto', name: 'Porto', country: 'PT', lat: 41.1, lon: -8.6),
  City(id: 'gaia', name: 'Vila Nova de Gaia', country: 'PT', lat: 41.1, lon: -8.6),
  // Deliberately short id-style name to prove <=2-char names never match.
  City(id: 'xx', name: 'PT', country: 'PT', lat: 0, lon: 0),
];

void main() {
  group('parseQuery', () {
    test('empty and whitespace-only input match nothing', () {
      expect(parseQuery('', _cities).matchedAnything, isFalse);
      expect(parseQuery('   ', _cities).matchedAnything, isFalse);
    });

    test('plain text stays as residual full-text search', () {
      final p = parseQuery('bruce springsteen', _cities);
      expect(p.matchedAnything, isFalse);
      expect(p.residual, 'bruce springsteen');
    });

    test('the kitchen sink: free jazz this weekend in Lisbon', () {
      final p = parseQuery('free jazz this weekend in Lisbon', _cities);
      expect(p.free, isTrue);
      expect(p.category, EventCategory.music); // jazz → music
      expect(p.weekend, isTrue);
      expect(p.cityId, 'lisbon');
      expect(p.cityName, 'Lisbon');
      expect(p.residual, isEmpty); // everything was recognised
      expect(p.summary, 'Music • Free • this weekend • in Lisbon');
    });

    test('is case-insensitive', () {
      final p = parseQuery('FREE Jazz THIS WEEKEND in LISBON', _cities);
      expect(p.free, isTrue);
      expect(p.category, EventCategory.music);
      expect(p.weekend, isTrue);
      expect(p.cityId, 'lisbon');
    });

    test('portuguese keywords: grátis, hoje, fim de semana, perto de mim', () {
      expect(parseQuery('concertos grátis', _cities).free, isTrue);
      expect(parseQuery('teatro hoje', _cities).tonight, isTrue);
      expect(parseQuery('música fim de semana', _cities).weekend, isTrue);
      expect(parseQuery('eventos perto de mim', _cities).nearMe, isTrue);
    });

    test('tonight and today both set tonight', () {
      expect(parseQuery('music tonight', _cities).tonight, isTrue);
      expect(parseQuery('music today', _cities).tonight, isTrue);
    });

    test('near me is recognised and does not leak into residual', () {
      final p = parseQuery('techno near me', _cities);
      expect(p.nearMe, isTrue);
      expect(p.category, EventCategory.music);
      expect(p.residual, isEmpty);
    });

    test('each category keyword family maps to its category', () {
      expect(parseQuery('hackathon', _cities).category, EventCategory.tech);
      expect(parseQuery('exhibition', _cities).category, EventCategory.arts);
      expect(parseQuery('networking', _cities).category, EventCategory.business);
      expect(parseQuery('gig', _cities).category, EventCategory.music);
    });

    test('first matched category wins over later ones', () {
      // "music" (music) appears alongside "startup" (tech) — map iteration
      // order makes music the first family checked.
      final p = parseQuery('music startup', _cities);
      expect(p.category, EventCategory.music);
    });

    test('longest city name wins over a substring city', () {
      // Nothing shorter ("Porto"?) should shadow the full multi-word name.
      final p = parseQuery('arts in vila nova de gaia', _cities);
      expect(p.cityId, 'gaia');
      expect(p.cityName, 'Vila Nova de Gaia');
    });

    test('city names of <=2 characters never match', () {
      final p = parseQuery('events in pt', _cities);
      expect(p.cityId, isNull);
    });

    test('city must match as whole words, not inside another word', () {
      final p = parseQuery('portobello market', _cities);
      expect(p.cityId, isNull); // "porto" inside "portobello" must not match
      expect(p.residual, 'portobello market');
    });

    test('unrecognised words survive as residual alongside matches', () {
      final p = parseQuery('free fado tonight', _cities);
      expect(p.free, isTrue);
      expect(p.tonight, isTrue);
      expect(p.category, isNull); // "fado" is not a known keyword
      expect(p.residual, 'fado');
    });

    test('filler connectives are stripped from the residual', () {
      final p = parseQuery('jazz in lisbon', _cities);
      expect(p.cityId, 'lisbon');
      expect(p.residual, isEmpty); // "in" must not survive
    });

    test('empty cities catalog still parses the rest', () {
      final p = parseQuery('free music tonight in lisbon', const <City>[]);
      expect(p.free, isTrue);
      expect(p.category, EventCategory.music);
      expect(p.tonight, isTrue);
      expect(p.cityId, isNull);
      expect(p.residual, 'lisbon'); // no catalog → city text left for search
    });

    test('repeated keyword occurrences are all consumed', () {
      final p = parseQuery('free free free', _cities);
      expect(p.free, isTrue);
      expect(p.residual, isEmpty);
    });

    test('summary omits unmatched parts', () {
      expect(parseQuery('music', _cities).summary, 'Music');
      expect(parseQuery('free tonight', _cities).summary, 'Free • tonight');
    });
  });
}
