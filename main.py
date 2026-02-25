#!/usr/bin/env python3
"""translation-combinator — overlay TTS translations onto an audio file.

Usage examples
--------------
# Free providers (no API key required)
python main.py audio.mp3 audio.vtt output.mp3
python main.py audio.mp3 audio.vtt output.mp3 --tts gtts

# Edge TTS with a specific voice
python main.py audio.mp3 audio.vtt output.mp3 --tts edge --edge-voice zh-TW-HsiaoChenNeural

# Azure TTS (free 500K chars/month)
python main.py audio.mp3 audio.vtt output.mp3 --tts azure \\
    --azure-key YOUR_KEY --azure-region eastus

# OpenAI TTS
python main.py audio.mp3 audio.vtt output.mp3 --tts openai --openai-key YOUR_KEY

# Google Cloud TTS
python main.py audio.mp3 audio.vtt output.mp3 --tts gcloud

# Adjust volume and disable auto speed-up
python main.py audio.mp3 audio.vtt output.mp3 --tts-volume 0.7 --no-speedup

TTS provider reference
----------------------
Provider  Free?  Quality  API key?  Package
--------  -----  -------  --------  -------
edge      Yes    ★★★★★   No        edge-tts
gtts      Yes    ★★★      No        gTTS
azure     *500K  ★★★★★   Yes       azure-cognitiveservices-speech
openai    No     ★★★★★   Yes       openai
gcloud    *1M    ★★★★     Yes       google-cloud-texttospeech

* free tier character limits per month
"""

import argparse
import os
import subprocess
import sys
import tempfile
from pathlib import Path

from parser import parse_vtt
from tts import build_provider, TTSError
from mixer import (
    generate_tts_clips,
    build_tts_track,
    merge_with_original,
    _get_audio_duration_ms,
)


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

def build_arg_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="translation-combinator",
        description="Add TTS translation voice track to an audio file using VTT subtitles.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )

    # Positional
    p.add_argument("audio", help="Input audio file path (mp3, m4a, wav, ogg, …)")
    p.add_argument("vtt", help="VTT subtitle file with translated text")
    p.add_argument("output", help="Output file path (e.g. output.mp3)")

    # TTS provider selection
    p.add_argument(
        "--tts",
        choices=["edge", "gtts", "azure", "openai", "gcloud"],
        default="edge",
        metavar="PROVIDER",
        help="TTS provider: edge (default), gtts, azure, openai, gcloud",
    )

    # edge-tts options
    edge = p.add_argument_group("Edge TTS options (--tts edge)")
    edge.add_argument(
        "--edge-voice",
        default="zh-CN-XiaoxiaoNeural",
        metavar="VOICE",
        help=(
            "Edge TTS voice name (default: zh-CN-XiaoxiaoNeural). "
            "Run 'edge-tts --list-voices' to see all voices."
        ),
    )

    # gTTS options
    gtts = p.add_argument_group("gTTS options (--tts gtts)")
    gtts.add_argument(
        "--gtts-lang",
        default="zh-CN",
        metavar="LANG",
        help="Language code for gTTS (default: zh-CN)",
    )

    # Azure options
    az = p.add_argument_group("Azure TTS options (--tts azure)")
    az.add_argument("--azure-key", metavar="KEY", help="Azure Speech API key (or set AZURE_TTS_KEY)")
    az.add_argument("--azure-region", metavar="REGION", help="Azure region, e.g. eastus (or set AZURE_TTS_REGION)")
    az.add_argument(
        "--azure-voice",
        default="zh-CN-XiaoxiaoNeural",
        metavar="VOICE",
        help="Azure TTS voice name (default: zh-CN-XiaoxiaoNeural)",
    )

    # OpenAI options
    oai = p.add_argument_group("OpenAI TTS options (--tts openai)")
    oai.add_argument("--openai-key", metavar="KEY", help="OpenAI API key (or set OPENAI_API_KEY)")
    oai.add_argument(
        "--openai-voice",
        default="alloy",
        choices=["alloy", "echo", "fable", "onyx", "nova", "shimmer"],
        metavar="VOICE",
        help="OpenAI TTS voice (default: alloy)",
    )
    oai.add_argument(
        "--openai-model",
        default="tts-1",
        choices=["tts-1", "tts-1-hd"],
        metavar="MODEL",
        help="OpenAI TTS model (default: tts-1)",
    )

    # Google Cloud options
    gc = p.add_argument_group("Google Cloud TTS options (--tts gcloud)")
    gc.add_argument("--gcloud-key", metavar="KEY", help="Google Cloud API key (or set GOOGLE_APPLICATION_CREDENTIALS)")
    gc.add_argument(
        "--gcloud-voice",
        default="cmn-CN-Wavenet-A",
        metavar="VOICE",
        help="Google Cloud TTS voice name (default: cmn-CN-Wavenet-A)",
    )

    # Mixing options
    mix = p.add_argument_group("Mixing options")
    mix.add_argument(
        "--tts-volume",
        type=float,
        default=1.0,
        metavar="N",
        help="TTS track volume, range 0–1 (default: 1.0)",
    )
    mix.add_argument(
        "--no-speedup",
        action="store_true",
        help="Disable automatic speed-up when TTS is longer than the subtitle window",
    )
    mix.add_argument(
        "--keep-tmp",
        action="store_true",
        help="Keep temporary TTS clip files after processing",
    )

    return p


# ---------------------------------------------------------------------------
# Validation helpers
# ---------------------------------------------------------------------------

def _check_ffmpeg() -> None:
    try:
        subprocess.run(
            ["ffmpeg", "-version"],
            capture_output=True,
            check=True,
        )
    except (FileNotFoundError, subprocess.CalledProcessError):
        print("ERROR: ffmpeg is not installed or not in PATH.", file=sys.stderr)
        print("Install it from https://ffmpeg.org/download.html", file=sys.stderr)
        sys.exit(1)


def _validate_volume(v: float) -> None:
    if not (0.0 <= v <= 1.0):
        print(
            f"ERROR: --tts-volume must be between 0 and 1, got {v}",
            file=sys.stderr,
        )
        sys.exit(1)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> None:
    parser = build_arg_parser()
    args = parser.parse_args()

    # Validate inputs
    if not os.path.isfile(args.audio):
        print(f"ERROR: Audio file not found: {args.audio}", file=sys.stderr)
        sys.exit(1)
    if not os.path.isfile(args.vtt):
        print(f"ERROR: VTT file not found: {args.vtt}", file=sys.stderr)
        sys.exit(1)
    _validate_volume(args.tts_volume)
    _check_ffmpeg()

    output_path = args.output

    # Parse subtitles
    print(f"Parsing VTT: {args.vtt}")
    segments = parse_vtt(args.vtt)
    if not segments:
        print("ERROR: No subtitle segments found in VTT file.", file=sys.stderr)
        sys.exit(1)
    print(f"  Found {len(segments)} segments.")

    # Build TTS provider
    try:
        provider = build_provider(args)
    except (TTSError, ValueError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        sys.exit(1)

    print(f"TTS provider: {provider.name}")

    # Get original audio duration for the silent base track
    print(f"Analysing source audio: {args.audio}")
    try:
        original_duration_ms = _get_audio_duration_ms(args.audio)
    except subprocess.CalledProcessError as exc:
        print(f"ERROR: Could not read audio duration: {exc}", file=sys.stderr)
        sys.exit(1)
    print(f"  Duration: {original_duration_ms / 1000:.1f}s")

    with tempfile.TemporaryDirectory(prefix="transc_") as tmp_dir:
        # Generate TTS clips
        print(f"\nGenerating TTS clips ({len(segments)} segments)…")
        clips = generate_tts_clips(
            segments=segments,
            provider=provider,
            tmp_dir=tmp_dir,
            speed_up=not args.no_speedup,
        )

        if not clips:
            print("ERROR: All TTS generations failed.", file=sys.stderr)
            sys.exit(1)

        print(f"\nSuccessfully generated {len(clips)}/{len(segments)} clips.")

        # Build combined TTS track
        print("Building timed TTS track…")
        try:
            tts_track_path = build_tts_track(
                clips=clips,
                total_duration_ms=original_duration_ms,
                tmp_dir=tmp_dir,
            )
        except Exception as exc:
            print(f"ERROR: Could not build TTS track: {exc}", file=sys.stderr)
            sys.exit(1)

        # Merge with original
        print(f"Merging with original audio (TTS volume: {args.tts_volume})…")
        try:
            merge_with_original(
                original_path=args.audio,
                tts_track_path=tts_track_path,
                output_path=output_path,
                tts_volume=args.tts_volume,
            )
        except subprocess.CalledProcessError as exc:
            print(f"ERROR: ffmpeg merge failed:\n{exc.stderr}", file=sys.stderr)
            sys.exit(1)

        if args.keep_tmp:
            import shutil
            keep_dir = output_path + "_tmp_clips"
            shutil.copytree(tmp_dir, keep_dir)
            print(f"Temporary clips saved to: {keep_dir}")

    print(f"\nDone! Output: {output_path}")


if __name__ == "__main__":
    main()
