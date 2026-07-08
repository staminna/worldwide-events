import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';

import 'package:eventscraper_app/models/event.dart';

Event _fullEvent() => Event(
  id: 'e1',
  source: EventSource.viralagenda,
  sourceId: 'va-1',
  title: 'Feira de Antiguidades',
  description: 'Antiques market.',
  category: EventCategory.arts,
  startsAt: DateTime.utc(2026, 7, 11, 8),
  endsAt: DateTime.utc(2026, 7, 11, 18),
  venue: const Venue(
    name: 'Mercado Municipal',
    address: 'Av. Cidade de Maringá, Leiria',
    lat: 39.7443,
    lon: -8.8072,
  ),
  city: 'Leiria',
  country: 'PT',
  url: 'https://example.com/feira',
  imageUrl: 'https://cdn.example.com/feira.jpg',
  price: const Price(min: 0, max: 5, currency: 'EUR', free: false),
  scrapedAt: DateTime.utc(2026, 7, 1, 3, 30),
);

void main() {
  group('Event JSON round trip (agenda persistence)', () {
    test('every field survives toJson → fromJson', () {
      final original = _fullEvent();
      // Through a real encode/decode cycle, exactly like shared_preferences.
      final restored = Event.fromJson(
        jsonDecode(jsonEncode(original.toJson())) as Map<String, dynamic>,
      );

      expect(restored.id, original.id);
      expect(restored.source, original.source);
      expect(restored.sourceId, original.sourceId);
      expect(restored.title, original.title);
      expect(restored.description, original.description);
      expect(restored.category, original.category);
      expect(restored.startsAt, original.startsAt);
      expect(restored.endsAt, original.endsAt);
      expect(restored.venue.name, original.venue.name);
      expect(restored.venue.address, original.venue.address);
      expect(restored.venue.lat, original.venue.lat);
      expect(restored.venue.lon, original.venue.lon);
      expect(restored.city, original.city);
      expect(restored.country, original.country);
      expect(restored.url, original.url);
      expect(restored.imageUrl, original.imageUrl);
      expect(restored.price!.min, original.price!.min);
      expect(restored.price!.max, original.price!.max);
      expect(restored.price!.currency, original.price!.currency);
      expect(restored.price!.free, original.price!.free);
      expect(restored.scrapedAt, original.scrapedAt);
    });

    test('null endsAt and null price survive the round trip', () {
      final sparse = Event(
        id: 'e2',
        source: EventSource.manual,
        sourceId: '',
        title: 'Open mic',
        description: '',
        category: EventCategory.unknown,
        startsAt: DateTime.utc(2026, 8, 1, 21),
        endsAt: null,
        venue: const Venue(), // all defaults: empty name, 0/0 coords
        city: '',
        country: '',
        url: '',
        imageUrl: '',
        price: null,
        scrapedAt: DateTime.utc(2026, 7, 1),
      );
      final restored = Event.fromJson(
        jsonDecode(jsonEncode(sparse.toJson())) as Map<String, dynamic>,
      );
      expect(restored.endsAt, isNull);
      expect(restored.price, isNull);
      expect(restored.venue.lat, 0);
      expect(restored.venue.lon, 0);
      expect(restored.category, EventCategory.unknown);
      expect(restored.source, EventSource.manual);
    });

    test('a free-price event stays free', () {
      final free = _fullEvent();
      final json = free.toJson();
      (json['price'] as Map<String, dynamic>)['free'] = true;
      final restored = Event.fromJson(json);
      expect(restored.price!.free, isTrue);
    });
  });
}
