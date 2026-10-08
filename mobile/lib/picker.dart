import 'package:flutter/material.dart';

/// A form field that shows the chosen value and opens a search page on tap.
class PickerField extends StatelessWidget {
  const PickerField({super.key, required this.label, required this.text, required this.onTap});

  final String label;
  final String? text;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(4),
      child: InputDecorator(
        isEmpty: text == null,
        decoration: InputDecoration(
          labelText: label,
          border: const OutlineInputBorder(),
          suffixIcon: const Icon(Icons.search),
        ),
        child: Text(text ?? '', style: Theme.of(context).textTheme.titleMedium),
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
  });

  final String title;
  final String searchHint;
  final String emptyText;
  final List<T> items;
  final String Function(T) titleOf;
  final String Function(T) subtitleOf;

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
            padding: const EdgeInsets.all(12),
            child: TextField(
              key: const Key('picker_search'),
              autofocus: true,
              decoration: InputDecoration(
                hintText: widget.searchHint,
                prefixIcon: const Icon(Icons.search),
                border: const OutlineInputBorder(),
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
                      return ListTile(
                        title: Text(widget.titleOf(found[i])),
                        subtitle: subtitle.isEmpty ? null : Text(subtitle),
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
