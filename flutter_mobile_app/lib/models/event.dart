class City {
  final String id;
  final String name;
  final String country;
  final double lat;
  final double lon;

  const City({
    required this.id,
    required this.name,
    required this.country,
    required this.lat,
    required this.lon,
  });

  factory City.fromJson(Map<String, dynamic> json) => City(
    id: json['id'] as String,
    name: json['name'] as String,
    country: json['country'] as String,
    lat: (json['lat'] as num?)?.toDouble() ?? 0,
    lon: (json['lon'] as num?)?.toDouble() ?? 0,
  );
}

/// Result of reverse-geocoding a coordinate against the backend's city
/// catalog: the closest supported city and how far away it is.
/// [locatedEvents] is only present when the lookup used min_events.
class NearestCity {
  final City city;
  final double distanceKm;
  final int? locatedEvents;

  const NearestCity({
    required this.city,
    required this.distanceKm,
    this.locatedEvents,
  });

  factory NearestCity.fromJson(Map<String, dynamic> json) => NearestCity(
    city: City.fromJson(json['city'] as Map<String, dynamic>),
    distanceKm: (json['distanceKm'] as num?)?.toDouble() ?? 0,
    locatedEvents: (json['locatedEvents'] as num?)?.toInt(),
  );
}

/// A candidate place returned by forward geocoding (GET /geo/search),
/// used by the map search bar and the add-event venue picker.
class LocationResult {
  final String displayName;
  final double lat;
  final double lon;

  const LocationResult({
    required this.displayName,
    required this.lat,
    required this.lon,
  });

  factory LocationResult.fromJson(Map<String, dynamic> json) => LocationResult(
    displayName: json['displayName'] as String? ?? '',
    lat: (json['lat'] as num?)?.toDouble() ?? 0,
    lon: (json['lon'] as num?)?.toDouble() ?? 0,
  );
}

class Venue {
  final String name;
  final String address;
  final double lat;
  final double lon;

  const Venue({this.name = '', this.address = '', this.lat = 0, this.lon = 0});

  factory Venue.fromJson(Map<String, dynamic>? json) {
    if (json == null) return const Venue();
    return Venue(
      name: json['name'] as String? ?? '',
      address: json['address'] as String? ?? '',
      lat: (json['lat'] as num?)?.toDouble() ?? 0,
      lon: (json['lon'] as num?)?.toDouble() ?? 0,
    );
  }

  Map<String, dynamic> toJson() => {
    'name': name,
    'address': address,
    'lat': lat,
    'lon': lon,
  };
}

class Price {
  final double min;
  final double max;
  final String currency;
  final bool free;

  const Price({
    this.min = 0,
    this.max = 0,
    this.currency = '',
    this.free = false,
  });

  factory Price.fromJson(Map<String, dynamic> json) => Price(
    min: (json['min'] as num?)?.toDouble() ?? 0,
    max: (json['max'] as num?)?.toDouble() ?? 0,
    currency: json['currency'] as String? ?? '',
    free: json['free'] as bool? ?? false,
  );

  Map<String, dynamic> toJson() => {
    'min': min,
    'max': max,
    'currency': currency,
    'free': free,
  };
}

enum EventSource {
  eventbrite,
  songkick,
  luma,
  ticketmaster,
  meetup,
  viralagenda,
  manual,
  unknown,
}

EventSource sourceFromString(String s) {
  switch (s) {
    case 'eventbrite':
      return EventSource.eventbrite;
    case 'songkick':
      return EventSource.songkick;
    case 'luma':
      return EventSource.luma;
    case 'ticketmaster':
      return EventSource.ticketmaster;
    case 'meetup':
      return EventSource.meetup;
    case 'viralagenda':
      return EventSource.viralagenda;
    case 'manual':
      return EventSource.manual;
  }
  return EventSource.unknown;
}

enum EventCategory { tech, music, arts, business, unknown }

EventCategory categoryFromString(String? s) {
  switch (s) {
    case 'tech':
      return EventCategory.tech;
    case 'music':
      return EventCategory.music;
    case 'arts':
      return EventCategory.arts;
    case 'business':
      return EventCategory.business;
  }
  return EventCategory.unknown;
}

String categoryLabel(EventCategory c) {
  switch (c) {
    case EventCategory.tech:
      return 'Tech';
    case EventCategory.music:
      return 'Music';
    case EventCategory.arts:
      return 'Arts';
    case EventCategory.business:
      return 'Business';
    case EventCategory.unknown:
      return 'Other';
  }
}

class Event {
  final String id;
  final EventSource source;
  final String sourceId;
  final String title;
  final String description;
  final EventCategory category;
  final DateTime startsAt;
  final DateTime? endsAt;
  final Venue venue;
  final String city;
  final String country;
  final String url;
  final String imageUrl;
  final Price? price;
  final DateTime scrapedAt;

  const Event({
    required this.id,
    required this.source,
    required this.sourceId,
    required this.title,
    required this.description,
    required this.category,
    required this.startsAt,
    required this.endsAt,
    required this.venue,
    required this.city,
    required this.country,
    required this.url,
    required this.imageUrl,
    required this.price,
    required this.scrapedAt,
  });

  factory Event.fromJson(Map<String, dynamic> json) => Event(
    id: json['id'] as String,
    source: sourceFromString(json['source'] as String? ?? ''),
    sourceId: json['sourceId'] as String? ?? '',
    title: json['title'] as String? ?? '',
    description: json['description'] as String? ?? '',
    category: categoryFromString(json['category'] as String?),
    startsAt: DateTime.parse(json['startsAt'] as String),
    endsAt: json['endsAt'] != null
        ? DateTime.tryParse(json['endsAt'] as String)
        : null,
    venue: Venue.fromJson(json['venue'] as Map<String, dynamic>?),
    city: json['city'] as String? ?? '',
    country: json['country'] as String? ?? '',
    url: json['url'] as String? ?? '',
    imageUrl: json['imageUrl'] as String? ?? '',
    price: json['price'] != null
        ? Price.fromJson(json['price'] as Map<String, dynamic>)
        : null,
    scrapedAt:
        DateTime.tryParse(json['scrapedAt'] as String? ?? '') ?? DateTime.now(),
  );

  /// Round-trips through [Event.fromJson] — used to persist saved events to
  /// shared_preferences so the agenda renders offline without re-fetching.
  Map<String, dynamic> toJson() => {
    'id': id,
    'source': source.name,
    'sourceId': sourceId,
    'title': title,
    'description': description,
    'category': category.name,
    'startsAt': startsAt.toIso8601String(),
    'endsAt': endsAt?.toIso8601String(),
    'venue': venue.toJson(),
    'city': city,
    'country': country,
    'url': url,
    'imageUrl': imageUrl,
    'price': price?.toJson(),
    'scrapedAt': scrapedAt.toIso8601String(),
  };
}

class SourceInfo {
  final EventSource id;
  final bool configured;
  const SourceInfo({required this.id, required this.configured});

  factory SourceInfo.fromJson(Map<String, dynamic> json) => SourceInfo(
    id: sourceFromString(json['id'] as String? ?? ''),
    configured: json['configured'] as bool? ?? false,
  );
}

class EventList {
  final List<Event> events;
  final int total;
  final bool cached;
  final String age;
  final int limit;
  final int offset;

  const EventList({
    required this.events,
    required this.total,
    required this.cached,
    required this.age,
    required this.limit,
    required this.offset,
  });
}
