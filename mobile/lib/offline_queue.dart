import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart';

/// An operation entered by hand that the server has not confirmed yet. [id] is the
/// operation id generated on the phone, so sending it again cannot post it twice.
/// [serverUrl] and [accessCode] are the server and tenant it was made for: the
/// access code selects the ledger, so an item is never sent anywhere else.
class QueuedOperation {
  const QueuedOperation({
    required this.id,
    required this.kind,
    required this.skuId,
    required this.locationId,
    this.toLocationId,
    required this.qty,
    required this.operator,
    required this.source,
    this.transcript = '',
    required this.skuName,
    required this.skuCode,
    required this.unit,
    required this.locationCode,
    this.toLocationCode = '',
    required this.serverUrl,
    required this.accessCode,
    required this.queuedAt,
    this.rejection,
  });

  factory QueuedOperation.fromJson(Map<String, dynamic> j) => QueuedOperation(
        id: j['id'] as String,
        kind: j['kind'] as String,
        skuId: j['sku_id'] as int,
        locationId: j['location_id'] as int,
        toLocationId: j['to_location_id'] as int?,
        qty: (j['qty'] as num).toDouble(),
        operator: j['operator'] as String,
        source: j['source'] as String,
        transcript: j['transcript'] as String? ?? '',
        skuName: j['sku_name'] as String? ?? '',
        skuCode: j['sku_code'] as String? ?? '',
        unit: j['unit'] as String? ?? '',
        locationCode: j['location_code'] as String? ?? '',
        toLocationCode: j['to_location_code'] as String? ?? '',
        serverUrl: j['server_url'] as String? ?? '',
        accessCode: j['access_code'] as String? ?? '',
        queuedAt: DateTime.parse(j['queued_at'] as String),
        rejection: j['rejection'] as String?,
      );

  final String id;
  final String kind;
  final int skuId;
  final int locationId;
  final int? toLocationId;
  final double qty;
  final String operator;
  final String source;
  final String transcript;

  /// Shown in the queue list, so it renders without the catalog.
  final String skuName;
  final String skuCode;
  final String unit;
  final String locationCode;
  final String toLocationCode;

  final String serverUrl;
  final String accessCode;
  final DateTime queuedAt;

  /// Why the server refused it; null while it is still waiting to be sent.
  final String? rejection;

  Map<String, dynamic> toJson() => {
        'id': id,
        'kind': kind,
        'sku_id': skuId,
        'location_id': locationId,
        'to_location_id': toLocationId,
        'qty': qty,
        'operator': operator,
        'source': source,
        'transcript': transcript,
        'sku_name': skuName,
        'sku_code': skuCode,
        'unit': unit,
        'location_code': locationCode,
        'to_location_code': toLocationCode,
        'server_url': serverUrl,
        'access_code': accessCode,
        'queued_at': queuedAt.toIso8601String(),
        'rejection': rejection,
      };

  QueuedOperation withRejection(String? reason) => QueuedOperation(
        id: id,
        kind: kind,
        skuId: skuId,
        locationId: locationId,
        toLocationId: toLocationId,
        qty: qty,
        operator: operator,
        source: source,
        transcript: transcript,
        skuName: skuName,
        skuCode: skuCode,
        unit: unit,
        locationCode: locationCode,
        toLocationCode: toLocationCode,
        serverUrl: serverUrl,
        accessCode: accessCode,
        queuedAt: queuedAt,
        rejection: reason,
      );
}

/// Operations kept on the phone while the server cannot be reached, sent oldest first
/// when it can. Stored in shared_preferences; an item leaves only after the server
/// confirmed it, or when the operator deletes a rejected one.
class OfflineQueue extends ChangeNotifier {
  static const prefsKey = 'offline_queue';
  static const cap = 200;

  List<QueuedOperation> _items = [];
  Future<void>? _running;

  List<QueuedOperation> get items => List.unmodifiable(_items);

  /// The server address as ApiClient uses it, so scopes compare equal.
  static String normalizeUrl(String url) => url.trim().replaceAll(RegExp(r'/+$'), '');

  /// The server is not reachable or failing, or the access code is wrong: the operation
  /// itself was not judged, so it stays queued. (storeError in the server maps any
  /// database failure to 500, which is temporary.)
  static bool isTemporary(ApiException e) => const {0, 401, 500, 502, 503, 504}.contains(e.status);

  bool _inScope(QueuedOperation o, String url, String code) =>
      o.serverUrl.isNotEmpty && o.serverUrl == normalizeUrl(url) && o.accessCode == code;

  /// Items still to be sent to this server and tenant.
  int waiting(String url, String code) => _items.where((o) => o.rejection == null && _inScope(o, url, code)).length;

  /// Items the server refused (of this server and tenant).
  List<QueuedOperation> rejected(String url, String code) =>
      _items.where((o) => o.rejection != null && _inScope(o, url, code)).toList();

  /// Items made for another server or access code: never sent from here.
  List<QueuedOperation> foreign(String url, String code) => _items.where((o) => !_inScope(o, url, code)).toList();

  QueuedOperation? byId(String id) {
    for (final o in _items) {
      if (o.id == id) return o;
    }
    return null;
  }

  Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    _items = _decode(prefs.getString(prefsKey));
    notifyListeners();
  }

  static List<QueuedOperation> _decode(String? raw) {
    if (raw == null) return [];
    try {
      return [for (final j in jsonDecode(raw) as List) QueuedOperation.fromJson(j as Map<String, dynamic>)];
    } catch (_) {
      return [];
    }
  }

  Future<void> _save() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(prefsKey, jsonEncode([for (final o in _items) o.toJson()]));
    notifyListeners();
  }

  /// False when the queue is full: the operation is not kept and the caller shows the error.
  Future<bool> enqueue(QueuedOperation op) async {
    if (_items.length >= cap) return false;
    if (byId(op.id) != null) return true;
    _items.add(op);
    await _save();
    return true;
  }

  /// Removes an item (a rejected or foreign one the operator gave up on).
  Future<void> dismiss(String id) async {
    _items.removeWhere((o) => o.id == id);
    await _save();
  }

  /// Makes a rejected item sendable again.
  Future<void> retry(String id) async {
    final i = _items.indexWhere((o) => o.id == id);
    if (i < 0) return;
    _items[i] = _items[i].withRejection(null);
    await _save();
  }

  /// Sends the waiting items of this server and tenant, oldest first, until none is left.
  /// A call while a flush runs returns that flush. A temporary failure stops it with the
  /// rest kept; a refusal marks that item as rejected and goes on with the next.
  Future<void> flush(ApiClient api, {required String serverUrl, required String accessCode}) =>
      _running ??= _flush(api, serverUrl, accessCode).whenComplete(() => _running = null);

  Future<void> _flush(ApiClient api, String url, String code) async {
    while (true) {
      QueuedOperation? next;
      for (final o in _items) {
        if (o.rejection == null && _inScope(o, url, code)) {
          next = o;
          break;
        }
      }
      if (next == null) return;
      try {
        await api.post(
          id: next.id,
          kind: next.kind,
          skuId: next.skuId,
          locationId: next.locationId,
          toLocationId: next.toLocationId,
          qty: next.qty,
          operator: next.operator,
          source: next.source,
          transcript: next.transcript,
        );
      } on ApiException catch (e) {
        if (isTemporary(e)) return;
        final i = _items.indexWhere((o) => o.id == next!.id);
        if (i >= 0) _items[i] = _items[i].withRejection(e.message);
        await _save();
        continue;
      } catch (_) {
        return;
      }
      // Removed only after the confirmation, so a lost response re-sends the same id.
      await dismiss(next.id);
    }
  }
}
