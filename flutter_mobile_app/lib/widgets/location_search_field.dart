import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/event.dart';
import '../state/providers.dart';

/// A debounced text field that forward-geocodes what the user types (via the
/// backend /geo/search) and surfaces the matches inline. Shared by the map
/// search bar and the add-event venue picker.
class LocationSearchField extends ConsumerStatefulWidget {
  const LocationSearchField({
    super.key,
    required this.onSelected,
    this.hintText = 'Search for a place…',
    this.autofocus = false,
  });

  final ValueChanged<LocationResult> onSelected;
  final String hintText;
  final bool autofocus;

  @override
  ConsumerState<LocationSearchField> createState() =>
      _LocationSearchFieldState();
}

class _LocationSearchFieldState extends ConsumerState<LocationSearchField> {
  final _controller = TextEditingController();
  Timer? _debounce;
  List<LocationResult> _results = const [];
  bool _loading = false;
  int _queryToken = 0;

  @override
  void dispose() {
    _debounce?.cancel();
    _controller.dispose();
    super.dispose();
  }

  void _onChanged(String value) {
    _debounce?.cancel();
    final trimmed = value.trim();
    if (trimmed.length < 3) {
      setState(() {
        _results = const [];
        _loading = false;
      });
      return;
    }
    setState(() => _loading = true);
    _debounce = Timer(const Duration(milliseconds: 350), () => _search(trimmed));
  }

  Future<void> _search(String query) async {
    final token = ++_queryToken;
    final results = await ref.read(apiProvider).searchLocation(query);
    // Ignore a slow response that lost the race to a newer keystroke.
    if (!mounted || token != _queryToken) return;
    setState(() {
      _results = results;
      _loading = false;
    });
  }

  void _pick(LocationResult r) {
    widget.onSelected(r);
    _controller.text = r.displayName;
    setState(() => _results = const []);
    FocusScope.of(context).unfocus();
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        TextField(
          controller: _controller,
          autofocus: widget.autofocus,
          textInputAction: TextInputAction.search,
          onChanged: _onChanged,
          decoration: InputDecoration(
            hintText: widget.hintText,
            prefixIcon: const Icon(Icons.search),
            suffixIcon: _loading
                ? const Padding(
                    padding: EdgeInsets.all(12),
                    child: SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  )
                : (_controller.text.isEmpty
                      ? null
                      : IconButton(
                          icon: const Icon(Icons.close),
                          onPressed: () {
                            _controller.clear();
                            _onChanged('');
                          },
                        )),
            border: OutlineInputBorder(
              borderRadius: BorderRadius.circular(12),
            ),
            isDense: true,
          ),
        ),
        if (_results.isNotEmpty)
          Container(
            margin: const EdgeInsets.only(top: 4),
            constraints: const BoxConstraints(maxHeight: 240),
            // Material (not a plain Container) so the ListTiles have a
            // Material ancestor to paint ink/selection on.
            child: Material(
              elevation: 2,
              clipBehavior: Clip.antiAlias,
              color: cs.surfaceContainerHigh,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(12),
                side: BorderSide(color: cs.outlineVariant),
              ),
              child: ListView.builder(
                shrinkWrap: true,
                padding: EdgeInsets.zero,
                itemCount: _results.length,
                itemBuilder: (_, i) {
                  final r = _results[i];
                  return ListTile(
                    dense: true,
                    leading: const Icon(Icons.place_outlined, size: 20),
                    title: Text(
                      r.displayName,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.bodyMedium,
                    ),
                    onTap: () => _pick(r),
                  );
                },
              ),
            ),
          ),
      ],
    );
  }
}
