# eventscraper_app

Flutter client for the eventscraper Go backend.

## Backend URL

The API base URL is compiled in via `--dart-define=API_BASE=...`
(see `lib/api/event_api.dart`). It defaults to the production backend at
`https://api.iamjorgenunes.com/eventscraper`, so a plain `flutter run`
uses production.

Run against a local backend (`go run ./cmd/eventscraper serve`):

```sh
flutter run --dart-define=API_BASE=http://localhost:8080
```

Release build (production is already the default):

```sh
flutter build apk
```
