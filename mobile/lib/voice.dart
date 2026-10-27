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

/// The recording format: encoder and file name go together.
typedef VoiceFormat = ({AudioEncoder encoder, String filename});

/// On the web the browser decides: Opus in WebM where the recorder supports it (Chrome,
/// Firefox), else AAC in MP4 (Safari on iPhone). Phones always record AAC to a .m4a file.
VoiceFormat chooseVoiceFormat({required bool web, required bool opusSupported}) {
  if (web && opusSupported) {
    return (encoder: AudioEncoder.opus, filename: 'phrase.webm');
  }
  return (encoder: AudioEncoder.aacLc, filename: 'phrase.m4a');
}

class MicrophoneCapture implements VoiceCapture {
  final _recorder = AudioRecorder();
  VoiceFormat _format = chooseVoiceFormat(web: false, opusSupported: false);

  @override
  Future<bool> start() async {
    if (!await _recorder.hasPermission()) return false;
    // Browsers ignore the path; phones record AAC to a temp file.
    final opus = kIsWeb && await _recorder.isEncoderSupported(AudioEncoder.opus);
    _format = chooseVoiceFormat(web: kIsWeb, opusSupported: opus);
    var path = '';
    if (!kIsWeb) {
      path = '${(await getTemporaryDirectory()).path}/${_format.filename}';
    }
    await _recorder.start(
      RecordConfig(encoder: _format.encoder, numChannels: 1),
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
    return Recording(bytes, _format.filename);
  }

  @override
  void dispose() => _recorder.dispose();
}
