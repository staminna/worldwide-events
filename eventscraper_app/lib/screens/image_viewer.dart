import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';

import '../api/event_api.dart';

class ImageViewerScreen extends StatelessWidget {
  const ImageViewerScreen({super.key, required this.url, this.heroTag});

  final String url;
  final Object? heroTag;

  @override
  Widget build(BuildContext context) {
    // Request the hi-res CDN variant (same URL the detail hero already cached,
    // so it shows instantly), and only fall back to the small thumbnail if that
    // variant 404s on this CDN.
    final image = CachedNetworkImage(
      imageUrl: proxiedImage(hiResImage(url)),
      fit: BoxFit.contain,
      placeholder: (_, __) => const Center(
        child: CircularProgressIndicator(color: Colors.white),
      ),
      errorWidget: (_, __, ___) => CachedNetworkImage(
        imageUrl: proxiedImage(url),
        fit: BoxFit.contain,
        placeholder: (_, __) => const Center(
          child: CircularProgressIndicator(color: Colors.white),
        ),
        errorWidget: (_, __, ___) => const Icon(
          Icons.broken_image,
          color: Colors.white54,
          size: 64,
        ),
      ),
    );
    return Scaffold(
      backgroundColor: Colors.black,
      extendBodyBehindAppBar: true,
      appBar: AppBar(
        backgroundColor: Colors.transparent,
        elevation: 0,
        iconTheme: const IconThemeData(color: Colors.white),
      ),
      body: GestureDetector(
        onTap: () => Navigator.of(context).maybePop(),
        child: InteractiveViewer(
          maxScale: 5,
          minScale: 0.8,
          child: Center(
            child: heroTag != null
                ? Hero(tag: heroTag!, child: image)
                : image,
          ),
        ),
      ),
    );
  }
}
