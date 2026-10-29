/// What the web page's setup QR carries: `qoimeks://setup?url=<enc>[&code=<enc>]`.
class SetupPayload {
  const SetupPayload({required this.url, this.code});

  final String url;
  final String? code;

  /// The access code is never printed, so logging a payload cannot leak it.
  @override
  String toString() =>
      'SetupPayload(url: $url, code: ${code == null ? 'none' : '***'})';
}

/// Parses a scanned QR text. Returns null for anything that is not a setup payload; never throws.
SetupPayload? parseSetupQr(String raw) {
  final uri = Uri.tryParse(raw.trim());
  if (uri == null ||
      uri.scheme.toLowerCase() != 'qoimeks' ||
      uri.host.toLowerCase() != 'setup') {
    return null;
  }
  final Map<String, String> params;
  try {
    params = uri.queryParameters;
  } on FormatException {
    return null;
  }
  final url = (params['url'] ?? '').trim();
  final target = Uri.tryParse(url);
  if (target == null ||
      !(target.scheme == 'http' || target.scheme == 'https') ||
      target.host.isEmpty) {
    return null;
  }
  final code = params['code'] ?? '';
  return SetupPayload(url: url, code: code.isEmpty ? null : code);
}

/// What a pairing QR carries: `qoimeks://pair?url=<enc>&pair=<enc code>`.
class PairPayload {
  const PairPayload({required this.url, required this.code});

  final String url;
  final String code;

  /// The pairing code is never printed, so logging a payload cannot leak it.
  @override
  String toString() => 'PairPayload(url: $url, code: ***)';
}

/// Parses a scanned pairing QR. Returns null for anything else, and when the code is missing; never throws.
PairPayload? parsePairQr(String raw) {
  final uri = Uri.tryParse(raw.trim());
  if (uri == null ||
      uri.scheme.toLowerCase() != 'qoimeks' ||
      uri.host.toLowerCase() != 'pair') {
    return null;
  }
  final Map<String, String> params;
  try {
    params = uri.queryParameters;
  } on FormatException {
    return null;
  }
  final url = (params['url'] ?? '').trim();
  final target = Uri.tryParse(url);
  if (target == null ||
      !(target.scheme == 'http' || target.scheme == 'https') ||
      target.host.isEmpty) {
    return null;
  }
  final code = (params['pair'] ?? '').trim();
  if (code.isEmpty) return null;
  return PairPayload(url: url, code: code);
}

/// One entry for the scanner: a [SetupPayload], a [PairPayload], or null.
Object? parseScannedQr(String raw) => parsePairQr(raw) ?? parseSetupQr(raw);
