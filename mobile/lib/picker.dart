import 'package:flutter/material.dart';

import 'design/theme.dart';
import 'design/tokens.dart';

/// A form field that shows the chosen value and opens a search page on tap.
class PickerField extends StatelessWidget {
  const PickerField({super.key, required this.label, required this.text, required this.onTap, this.code});

  final String label;

  /// The chosen value. Without [code] it is itself a code (a location) and is shown in monospace.
  final String? text;

  /// A code shown under [text] in the monospace font.
  final String? code;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.md),
      child: InputDecorator(
        isEmpty: text == null,
        decoration: InputDecoration(
          labelText: label,
          suffixIcon: const Icon(Icons.search),
        ),
        child: code == null
            ? Text(text ?? '', style: monoStyle(Theme.of(context).textTheme.titleMedium))
            : Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(text ?? '', style: Theme.of(context).textTheme.titleMedium),
                  Text(code!, style: monoStyle(Theme.of(context).textTheme.bodyMedium)),
                ],
              ),
      ),
    );
  }
}

/// Opens a full-screen searchable list and returns the tapped item.
Future<T?> pick<T>(
  BuildContext context, {
  required String title,
  required String searchHint,
  required String emptyText,
  required List<T> items,
  required String Function(T) titleOf,
  required String Function(T) subtitleOf,
  bool monoTitle = false,
  bool monoSubtitle = false,
}) {
  return Navigator.of(context).push<T>(MaterialPageRoute(
    fullscreenDialog: true,
    builder: (_) => _PickerPage<T>(
      title: title,
      searchHint: searchHint,
      emptyText: emptyText,
      items: items,
      titleOf: titleOf,
      subtitleOf: subtitleOf,
      monoTitle: monoTitle,
      monoSubtitle: monoSubtitle,
    ),
  ));
}

class _PickerPage<T> extends StatefulWidget {
  const _PickerPage({
    required this.title,
    required this.searchHint,
    required this.emptyText,
    required this.items,
    required this.titleOf,
    required this.subtitleOf,
    required this.monoTitle,
    required this.monoSubtitle,
  });

  final String title;
  final String searchHint;
  final String emptyText;
  final List<T> items;
  final String Function(T) titleOf;
  final String Function(T) subtitleOf;
  final bool monoTitle;
  final bool monoSubtitle;

  @override
  State<_PickerPage<T>> createState() => _PickerPageState<T>();
}

class _PickerPageState<T> extends State<_PickerPage<T>> {
  String _query = '';

  // "a01" finds "A-01": punctuation and case are ignored on both sides.
  static String _squash(String s) => s.toLowerCase().replaceAll(RegExp(r'[^\p{L}\p{N}]', unicode: true), '');

  bool _matches(T item) {
    final text = '${widget.titleOf(item)} ${widget.subtitleOf(item)}';
    final squashed = _squash(text);
    return _query
        .split(RegExp(r'\s+'))
        .where((w) => w.isNotEmpty)
        .every((w) => squashed.contains(_squash(w)));
  }

  @override
  Widget build(BuildContext context) {
    final found = widget.items.where(_matches).toList();
    return Scaffold(
      appBar: AppBar(title: Text(widget.title)),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(AppSpace.s3),
            child: TextField(
              key: const Key('picker_search'),
              autofocus: true,
              decoration: InputDecoration(
                labelText: widget.searchHint,
                prefixIcon: const Icon(Icons.search),
              ),
              onChanged: (v) => setState(() => _query = v),
            ),
          ),
          Expanded(
            child: found.isEmpty
                ? Center(child: Text(widget.emptyText))
                : ListView.builder(
                    itemCount: found.length,
                    itemBuilder: (context, i) {
                      final subtitle = widget.subtitleOf(found[i]);
                      final text = Theme.of(context).textTheme;
                      return ListTile(
                        minTileHeight: AppSize.touchMin,
                        title: Text(
                          widget.titleOf(found[i]),
                          style: widget.monoTitle ? monoStyle(text.bodyLarge) : null,
                        ),
                        subtitle: subtitle.isEmpty
                            ? null
                            : Text(
                                subtitle,
                                style: widget.monoSubtitle
                                    ? monoStyle(text.bodyMedium?.copyWith(color: LightColors.textSoft))
                                    : null,
                              ),
                        onTap: () => Navigator.of(context).pop(found[i]),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}
