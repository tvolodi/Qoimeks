import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import 'design/tokens.dart';
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
  /// [anyBarcode] reads every common barcode format (to find an item); the default reads QR codes only.
  const CameraQrScanner({this.anyBarcode = false});

  final bool anyBarcode;

  @override
  Future<ScanResult> scan(BuildContext context, Strings s) async {
    final result = await Navigator.of(context).push<ScanResult>(
      MaterialPageRoute(builder: (_) => ScannerPage(
          title: s(anyBarcode ? 'code_scan_title' : 'qr_scan_title'),
          anyBarcode: anyBarcode,
        ),),
    );
    return result ?? const ScanCancelled();
  }
}

class ScannerPage extends StatefulWidget {
  const ScannerPage({super.key, required this.title, this.anyBarcode = false});

  final String title;
  final bool anyBarcode;

  @override
  State<ScannerPage> createState() => _ScannerPageState();
}

class _ScannerPageState extends State<ScannerPage> {
  late final _controller = MobileScannerController(
    formats: widget.anyBarcode ? const [] : const [BarcodeFormat.qrCode],
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
      body: Stack(
        fit: StackFit.expand,
        children: [
          MobileScanner(
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
              WidgetsBinding.instance.addPostFrameCallback(
                (_) => _finish(result),
              );
              return const SizedBox.shrink();
            },
          ),
          const IgnorePointer(child: Center(child: ScannerFrame())),
        ],
      ),
    );
  }
}

/// The frame that shows where to hold the QR code: the one place besides the logo where amber is used.
class ScannerFrame extends StatelessWidget {
  const ScannerFrame({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: AppSpace.s16 * 4,
      height: AppSpace.s16 * 4,
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(AppRadius.lg),
        border: Border.all(color: LightColors.brand, width: AppSpace.s1),
      ),
    );
  }
}
