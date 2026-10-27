// Makes the web build cache-safe: points flutter_bootstrap.js at main.dart.js?v=<build id>.
// Usage: dart run tool/cache_bust.dart [build/web]
import 'dart:io';

import 'package:crypto/crypto.dart';

const _plain = '"mainJsPath":"main.dart.js"';

/// First 12 hex characters of the SHA-256 of [bytes] (changes exactly when the code does).
String buildId(List<int> bytes) =>
    sha256.convert(bytes).toString().substring(0, 12);

/// Returns [bootstrap] with every `"mainJsPath":"main.dart.js"` replaced by the
/// versioned one. Throws [FormatException] when the pattern is absent.
String bustMainJs(String bootstrap, String id) {
  if (!bootstrap.contains(_plain)) {
    throw const FormatException(
      'flutter_bootstrap.js has no "mainJsPath":"main.dart.js"; '
      'did the Flutter version change its format?',
    );
  }
  return bootstrap.replaceAll(_plain, '"mainJsPath":"main.dart.js?v=$id"');
}

void main(List<String> args) {
  final dir = args.isEmpty ? 'build/web' : args.first;
  final js = File('$dir/main.dart.js');
  final bootstrap = File('$dir/flutter_bootstrap.js');
  if (!js.existsSync() || !bootstrap.existsSync()) {
    stderr.writeln(
      'cache_bust: $dir/main.dart.js or flutter_bootstrap.js not found',
    );
    exit(1);
  }
  final id = buildId(js.readAsBytesSync());
  try {
    bootstrap.writeAsStringSync(bustMainJs(bootstrap.readAsStringSync(), id));
  } on FormatException catch (e) {
    stderr.writeln('cache_bust: ${e.message}');
    exit(1);
  }
  stdout.writeln(id);
}
