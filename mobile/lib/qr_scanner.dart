import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import 'l10n.dart';

sealed class ScanResult {
  const ScanResult();
}

class ScanText extends ScanResult {
  const ScanText(this.text);
  final String text;

  /// The scanned text may hold the access code: never print it.
  @override
  String toString() => 'ScanText(***)';
}

class ScanCancelled extends ScanResult {
  const ScanCancelled();
}

class ScanPermissionDenied extends ScanResult {
  const ScanPermissionDenied();
}

class ScanNoCamera extends ScanResult {
  const ScanNoCamera();
}

/// Seam between the settings screen and the camera, so tests need no camera.
abstract class QrScanner {
  Future<ScanResult> scan(BuildContext context, Strings s);
}

class CameraQrScanner implements QrScanner {
  const CameraQrScanner();

  @override
  Future<ScanResult> scan(BuildContext context, Strings s) async {
    final result = await Navigator.of(context).push<ScanResult>(
      MaterialPageRoute(builder: (_) => ScannerPage(title: s('qr_scan_title'))),
    );
    return result ?? const ScanCancelled();
  }
}

class ScannerPage extends StatefulWidget {
  const ScannerPage({super.key, required this.title});

  final String title;

  @override
  State<ScannerPage> createState() => _ScannerPageState();
}

class _ScannerPageState extends State<ScannerPage> {
  final _controller = MobileScannerController(
    formats: const [BarcodeFormat.qrCode],
  );
  bool _done = false;

  void _finish(ScanResult result) {
    if (_done || !mounted) return;
    _done = true;
    Navigator.of(context).pop(result);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(widget.title)),
      body: MobileScanner(
        controller: _controller,
        onDetect: (capture) {
          for (final code in capture.barcodes) {
            final text = code.rawValue;
            if (text != null && text.isNotEmpty) {
              _finish(ScanText(text));
              return;
            }
          }
        },
        // The builder runs during build: leave the route after the frame.
        errorBuilder: (context, error) {
          final result =
              error.errorCode == MobileScannerErrorCode.permissionDenied
              ? const ScanPermissionDenied()
              : const ScanNoCamera();
          WidgetsBinding.instance.addPostFrameCallback((_) => _finish(result));
          return const SizedBox.shrink();
        },
      ),
    );
  }
}
