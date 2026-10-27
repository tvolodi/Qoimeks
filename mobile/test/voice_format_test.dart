import 'package:flutter_test/flutter_test.dart';
import 'package:qoimeks/voice.dart';
import 'package:record/record.dart';

void main() {
  test('web with Opus support records Opus in WebM', () {
    final f = chooseVoiceFormat(web: true, opusSupported: true);
    expect(f.encoder, AudioEncoder.opus);
    expect(f.filename, 'phrase.webm');
  });

  test('web without Opus support (Safari) records AAC in M4A', () {
    final f = chooseVoiceFormat(web: true, opusSupported: false);
    expect(f.encoder, AudioEncoder.aacLc);
    expect(f.filename, 'phrase.m4a');
  });

  test('phones always record AAC in M4A', () {
    for (final opus in [true, false]) {
      final f = chooseVoiceFormat(web: false, opusSupported: opus);
      expect(f.encoder, AudioEncoder.aacLc);
      expect(f.filename, 'phrase.m4a');
    }
  });
}
