import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:path_provider/path_provider.dart';
import 'package:record/record.dart';

class Recording {
  const Recording(this.bytes, this.filename);

  final Uint8List bytes;
  final String filename;
}

abstract class VoiceCapture {
  /// Starts recording. Returns false when the microphone may not be used.
  Future<bool> start();

  /// Stops recording and returns what was captured, or null if nothing was.
  Future<Recording?> stop();

  void dispose();
}

class MicrophoneCapture implements VoiceCapture {
  final _recorder = AudioRecorder();

  @override
  Future<bool> start() async {
    if (!await _recorder.hasPermission()) return false;
    // Browsers record Opus in WebM and ignore the path; phones record AAC to a temp file.
    var path = '';
    if (!kIsWeb) {
      path = '${(await getTemporaryDirectory()).path}/phrase.m4a';
    }
    await _recorder.start(
      RecordConfig(encoder: kIsWeb ? AudioEncoder.opus : AudioEncoder.aacLc, numChannels: 1),
      path: path,
    );
    return true;
  }

  @override
  Future<Recording?> stop() async {
    final path = await _recorder.stop();
    if (path == null) return null;
    // In a browser the recording is a blob: URL, on a phone it is a file.
    final bytes = kIsWeb ? await http.readBytes(Uri.parse(path)) : await File(path).readAsBytes();
    if (bytes.isEmpty) return null;
    return Recording(bytes, kIsWeb ? 'phrase.webm' : 'phrase.m4a');
  }

  @override
  void dispose() => _recorder.dispose();
}
