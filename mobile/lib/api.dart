import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import 'feedback.dart' show DeviceInfoReader;

class Sku {
  const Sku({
    required this.id,
    required this.code,
    required this.name,
    required this.unit,
    this.locationId,
    this.altLocationId,
  });

  factory Sku.fromJson(Map<String, dynamic> j) => Sku(
        id: j['id'],
        code: j['code'],
        name: j['name'],
        unit: j['unit'],
        locationId: j['location_id'],
        altLocationId: j['alt_location_id'],
      );

  final int id;
  final String code;
  final String name;
  final String unit;

  /// Where the SKU normally lives, and a second place to look.
  final int? locationId;
  final int? altLocationId;

  @override
  bool operator ==(Object other) => other is Sku && other.id == id;

  @override
  int get hashCode => id.hashCode;
}

class Location {
  const Location({required this.id, required this.code, required this.name});

  factory Location.fromJson(Map<String, dynamic> j) =>
      Location(id: j['id'], code: j['code'], name: j['name'] ?? '');

  final int id;
  final String code;
  final String name;

  @override
  bool operator ==(Object other) => other is Location && other.id == id;

  @override
  int get hashCode => id.hashCode;
}

class Catalog {
  const Catalog({required this.skus, required this.locations});

  final List<Sku> skus;
  final List<Location> locations;

  Location? location(int? id) {
    for (final l in locations) {
      if (l.id == id) return l;
    }
    return null;
  }
}

class BalanceRow {
  const BalanceRow({required this.locationId, required this.locationCode, required this.qty});

  factory BalanceRow.fromJson(Map<String, dynamic> j) => BalanceRow(
        locationId: j['location_id'],
        locationCode: j['location_code'],
        qty: (j['qty'] as num).toDouble(),
      );

  final int locationId;
  final String locationCode;
  final double qty;
}

class Movement {
  Movement.fromJson(Map<String, dynamic> j)
      : id = j['id'],
        kind = j['kind'],
        skuCode = j['sku_code'],
        skuName = j['sku_name'],
        unit = j['unit'],
        locationCode = j['location_code'],
        toLocationCode = j['to_location_code'] ?? '',
        qty = (j['qty'] as num).toDouble(),
        operator = j['operator'],
        isReversal = j['reverses'] != null,
        isReversed = j['reversed_by'] != null,
        createdAt = DateTime.parse(j['created_at']).toLocal(),
        balances = [for (final b in j['balances'] as List) BalanceRow.fromJson(b)];

  final String id;
  final String kind;
  final String skuCode;
  final String skuName;
  final String unit;
  final String locationCode;
  final String toLocationCode;
  final double qty;
  final String operator;
  final bool isReversal;
  final bool isReversed;
  final DateTime createdAt;

  /// Balance after this movement at each location it touched.
  final List<BalanceRow> balances;
}

/// What the server understood from a spoken phrase. Nothing is saved yet.
class Draft {
  Draft.fromJson(Map<String, dynamic> j)
      : transcript = j['transcript'] ?? '',
        kind = j['kind'],
        skus = [for (final s in j['skus'] as List) Sku.fromJson(s)],
        location = j['location'] == null ? null : Location.fromJson(j['location']),
        toLocation = j['to_location'] == null ? null : Location.fromJson(j['to_location']),
        qty = (j['qty'] as num?)?.toDouble();

  final String transcript;
  final String? kind;

  /// Best match first. More than one means the operator has to choose.
  final List<Sku> skus;
  final Location? location;
  final Location? toLocation;
  final double? qty;
}

class ApiException implements Exception {
  const ApiException(this.status, this.message);

  /// HTTP status, or 0 when the server could not be reached.
  final int status;
  final String message;

  @override
  String toString() => message;
}

class ApiClient {
  ApiClient(
    String baseUrl, {
    String accessCode = '',
    http.Client? client,
    this.onSuccess,
    this.onError,
    this.operator = '',
    DeviceInfoReader? deviceInfo,
  })  : _base = baseUrl.trim().replaceAll(RegExp(r'/+$'), ''),
        _auth = accessCode.isEmpty ? const {} : {'Authorization': 'Bearer $accessCode'},
        _http = client ?? http.Client(),
        _deviceInfo = deviceInfo;

  final String _base;
  final Map<String, String> _auth;
  final http.Client _http;
  final DeviceInfoReader? _deviceInfo;

  /// The operator's name, sent as X-Operator so the server can say who met an error.
  final String operator;
  Future<Map<String, String>>? _headers;

  /// [deviceInfo] (the app passes readDeviceInfo) fills the app version and device.
  /// Authorization plus X-App-Version, X-Device and X-Operator (the last three only when
  /// known). Values are percent-encoded because HTTP headers cannot carry Cyrillic.
  Future<Map<String, String>> _requestHeaders() => _headers ??= () async {
        final info = await _deviceInfo?.call();
        return {
          ..._auth,
          if (info != null && info.appVersion.isNotEmpty) 'X-App-Version': Uri.encodeComponent(info.appVersion),
          if (info != null && info.device.isNotEmpty) 'X-Device': Uri.encodeComponent(info.device),
          if (operator.trim().isNotEmpty) 'X-Operator': Uri.encodeComponent(operator.trim()),
        };
      }();

  /// Called after a call succeeded / failed (not for feedback itself).
  final void Function()? onSuccess;
  final void Function(String path, ApiException error)? onError;

  Future<dynamic> _send(Future<http.Response> Function() request, {String path = '', bool observe = true}) async {
    final http.Response resp;
    try {
      resp = await request().timeout(const Duration(seconds: 90));
    } catch (e) {
      final err = ApiException(0, e.toString());
      if (observe) onError?.call(path, err);
      throw err;
    }
    dynamic body;
    try {
      body = jsonDecode(utf8.decode(resp.bodyBytes));
    } catch (_) {
      body = null;
    }
    if (resp.statusCode >= 400) {
      final err = ApiException(resp.statusCode, body is Map ? '${body['error']}' : 'HTTP ${resp.statusCode}');
      if (observe) onError?.call(path, err);
      throw err;
    }
    if (observe) onSuccess?.call();
    return body;
  }

  Future<dynamic> _get(String path) =>
      _send(() async => _http.get(Uri.parse('$_base$path'), headers: await _requestHeaders()), path: path);

  Future<dynamic> _post(String path, Map<String, dynamic> body, {bool observe = true}) => _send(
        () async => _http.post(
          Uri.parse('$_base$path'),
          headers: {'Content-Type': 'application/json', ...await _requestHeaders()},
          body: jsonEncode(body),
        ),
        path: path,
        observe: observe,
      );

  /// A problem report. Not observed: its own failure is neither logged nor a reason to retry.
  Future<void> postFeedback(Map<String, dynamic> report) => _post('/api/feedback', report, observe: false);

  Future<Catalog> catalog() async {
    final j = await _get('/api/catalog');
    return Catalog(
      skus: [for (final s in j['skus'] as List) Sku.fromJson(s)],
      locations: [for (final l in j['locations'] as List) Location.fromJson(l)],
    );
  }

  /// Current non-zero balances of one SKU, by location.
  Future<List<BalanceRow>> balances(int skuId) async {
    final j = await _get('/api/balances?sku_id=$skuId');
    return [for (final b in j as List) BalanceRow.fromJson(b)];
  }

  Future<List<Movement>> movements() async {
    final j = await _get('/api/movements?limit=100');
    return [for (final m in j as List) Movement.fromJson(m)];
  }

  /// Posts an operation. [id] is generated on the phone, so sending the same
  /// operation again after a lost response cannot post it twice.
  Future<Movement> post({
    required String id,
    required String kind,
    required int skuId,
    required int locationId,
    int? toLocationId,
    required double qty,
    required String operator,
    required String source,
    String transcript = '',
  }) async {
    return Movement.fromJson(await _post('/api/movements', {
      'id': id,
      'kind': kind,
      'sku_id': skuId,
      'location_id': locationId,
      'to_location_id': toLocationId,
      'qty': qty,
      'operator': operator,
      'source': source,
      'transcript': transcript,
    }));
  }

  Future<Movement> reverse({required String movementId, required String id, required String operator}) async {
    return Movement.fromJson(await _post('/api/movements/$movementId/reverse', {'id': id, 'operator': operator}));
  }

  /// [lang] ("ru" or "kk") tells the recogniser which alphabet to expect;
  /// without it Kazakh speech comes back in Latin letters.
  Future<Draft> voice(Uint8List audio, String filename, String lang) async {
    final j = await _send(() async {
      final req = http.MultipartRequest('POST', Uri.parse('$_base/api/voice'))
        ..headers.addAll(await _requestHeaders())
        ..fields['lang'] = lang
        ..files.add(http.MultipartFile.fromBytes('audio', audio, filename: filename));
      return http.Response.fromStream(await _http.send(req));
    }, path: '/api/voice');
    return Draft.fromJson(j);
  }
}
