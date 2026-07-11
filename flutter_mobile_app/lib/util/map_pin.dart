import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';

/// Renders a teardrop map pin to PNG bytes for use as a MapLibre symbol image
/// (`controller.addImage` + a SymbolLayer with `iconAnchor: 'bottom'`). The
/// tip is at the bottom-center so the symbol points exactly at its coordinate.
/// [dpr] keeps it crisp on the GL surface.
Future<Uint8List> renderMapPin(
  Color color, {
  double size = 46,
  double dpr = 3,
}) async {
  final s = size * dpr;
  final w = s;
  final h = s * 1.3;
  final r = s * 0.42; // head radius
  final cx = w / 2;
  final cy = r + s * 0.06; // head center, small top margin

  final recorder = ui.PictureRecorder();
  final canvas = Canvas(recorder);

  final head = Path()
    ..addOval(Rect.fromCircle(center: Offset(cx, cy), radius: r));
  final tail = Path()
    ..moveTo(cx - r * 0.72, cy + r * 0.30)
    ..lineTo(cx, h)
    ..lineTo(cx + r * 0.72, cy + r * 0.30)
    ..close();
  final pin = Path.combine(PathOperation.union, head, tail);

  canvas.drawPath(pin, Paint()..color = color..isAntiAlias = true);
  canvas.drawPath(
    pin,
    Paint()
      ..color = Colors.white
      ..style = PaintingStyle.stroke
      ..strokeWidth = s * 0.07
      ..isAntiAlias = true,
  );
  canvas.drawCircle(Offset(cx, cy), r * 0.36, Paint()..color = Colors.white);

  final image = await recorder.endRecording().toImage(w.ceil(), h.ceil());
  final data = await image.toByteData(format: ui.ImageByteFormat.png);
  return data!.buffer.asUint8List();
}

/// Renders the nav-mode user puck: a blue disc with a white ring and a white
/// chevron pointing up (north at iconRotate 0), for a SymbolLayer with
/// `iconRotationAlignment: 'map'` so it rotates with the heading.
Future<Uint8List> renderNavArrow({double size = 34, double dpr = 3}) async {
  final s = size * dpr;
  final c = s / 2;
  final r = s * 0.44;

  final recorder = ui.PictureRecorder();
  final canvas = Canvas(recorder);

  canvas.drawCircle(
    Offset(c, c),
    r,
    Paint()
      ..color = const Color(0xFF1A73E8)
      ..isAntiAlias = true,
  );
  canvas.drawCircle(
    Offset(c, c),
    r,
    Paint()
      ..color = Colors.white
      ..style = PaintingStyle.stroke
      ..strokeWidth = s * 0.07
      ..isAntiAlias = true,
  );
  final chevron = Path()
    ..moveTo(c, c - r * 0.62) // tip
    ..lineTo(c + r * 0.48, c + r * 0.42)
    ..lineTo(c, c + r * 0.14) // notch
    ..lineTo(c - r * 0.48, c + r * 0.42)
    ..close();
  canvas.drawPath(chevron, Paint()..color = Colors.white..isAntiAlias = true);

  final image = await recorder.endRecording().toImage(s.ceil(), s.ceil());
  final data = await image.toByteData(format: ui.ImageByteFormat.png);
  return data!.buffer.asUint8List();
}
