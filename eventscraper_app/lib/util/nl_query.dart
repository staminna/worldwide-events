import '../models/event.dart';

/// Result of interpreting a free-text search box as filters.
class ParsedQuery {
  final EventCategory? category;
  final String? cityId;
  final String? cityName;
  final bool free;
  final bool tonight;
  final bool weekend;
  final bool nearMe;

  /// Whatever text is left after removing recognised keywords — used as the
  /// server's full-text `search` so titles still match.
  final String residual;

  const ParsedQuery({
    this.category,
    this.cityId,
    this.cityName,
    this.free = false,
    this.tonight = false,
    this.weekend = false,
    this.nearMe = false,
    this.residual = '',
  });

  /// True when at least one structured filter was recognised.
  bool get matchedAnything =>
      category != null ||
      cityId != null ||
      free ||
      tonight ||
      weekend ||
      nearMe;

  /// A short human summary of what was applied, for a confirmation snackbar.
  String get summary {
    final parts = <String>[
      if (category != null) categoryLabel(category!),
      if (free) 'Free',
      if (tonight) 'tonight',
      if (weekend) 'this weekend',
      if (nearMe) 'near me',
      if (cityName != null) 'in $cityName',
    ];
    return parts.join(' • ');
  }
}

const _categoryKeywords = <EventCategory, List<String>>{
  EventCategory.music: [
    'music',
    'concert',
    'concerts',
    'gig',
    'gigs',
    'jazz',
    'rock',
    'dj',
    'band',
    'festival',
    'rave',
    'techno',
    'hip hop',
    'rap',
    'live music',
    'show',
    'música',
    'concerto',
  ],
  EventCategory.tech: [
    'tech',
    'technology',
    'startup',
    'startups',
    'hackathon',
    'coding',
    'developer',
    'software',
    'data',
    'tecnologia',
  ],
  EventCategory.arts: [
    'art',
    'arts',
    'exhibition',
    'gallery',
    'theatre',
    'theater',
    'dance',
    'cinema',
    'film',
    'museum',
    'opera',
    'culture',
    'cultural',
    'arte',
    'exposição',
    'teatro',
  ],
  EventCategory.business: [
    'business',
    'networking',
    'conference',
    'workshop',
    'seminar',
    'meetup',
    'entrepreneur',
    'career',
    'negócios',
    'conferência',
  ],
};

const _freeWords = ['free', 'grátis', 'gratis', 'gratuito'];
const _nearWords = ['near me', 'nearby', 'perto de mim', 'perto'];

/// Interprets [raw] as a set of filters using keyword heuristics (no LLM). City
/// names are matched against the provided [cities] catalog. Anything not
/// recognised is returned as [ParsedQuery.residual] for full-text search.
///
/// Deliberately simple and deterministic — a backend LLM parser can replace
/// this later without changing the call sites.
ParsedQuery parseQuery(String raw, List<City> cities) {
  var text = ' ${raw.toLowerCase().trim()} ';
  if (text.trim().isEmpty) return const ParsedQuery();

  // Removes every whole-word occurrence of [phrase]. Loops because adjacent
  // repeats share their boundary space, which a single replaceAll pass
  // cannot match ("free free free" would otherwise leave one behind).
  bool consume(String phrase) {
    final p = ' $phrase ';
    var found = false;
    while (text.contains(p)) {
      text = text.replaceAll(p, ' ');
      found = true;
    }
    return found;
  }

  // Eager: every phrase is consumed, not just the first hit — otherwise a
  // query like "free grátis" would leak the synonym into the residual.
  bool consumeAny(List<String> phrases) {
    var any = false;
    for (final p in phrases) {
      if (consume(p)) any = true;
    }
    return any;
  }

  final free = consumeAny(_freeWords);
  final nearMe = consumeAny(_nearWords);

  // Longest phrase first, so "weekend" doesn't strand a leading "this".
  final weekend = consumeAny(['this weekend', 'fim de semana', 'weekend']);
  final tonight = consumeAny(['tonight', 'today', 'hoje']);

  EventCategory? category;
  for (final entry in _categoryKeywords.entries) {
    for (final kw in entry.value) {
      if (consume(kw)) {
        category ??= entry.key;
      }
    }
  }

  // Longest city-name match wins (so "Vila Nova de Gaia" beats "Vila").
  String? cityId;
  String? cityName;
  var bestLen = 0;
  for (final c in cities) {
    final name = c.name.toLowerCase();
    if (name.length > 2 &&
        name.length > bestLen &&
        text.contains(' $name ')) {
      cityId = c.id;
      cityName = c.name;
      bestLen = name.length;
    }
  }
  if (cityName != null) consume(cityName.toLowerCase());

  // Drop filler connective words from the residual.
  for (final w in ['in', 'near', 'at', 'this', 'em', 'no', 'na']) {
    consume(w);
  }

  return ParsedQuery(
    category: category,
    cityId: cityId,
    cityName: cityName,
    free: free,
    tonight: tonight,
    weekend: weekend,
    nearMe: nearMe,
    residual: text.replaceAll(RegExp(r'\s+'), ' ').trim(),
  );
}
