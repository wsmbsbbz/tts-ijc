"""Audio mixing: build a timed TTS track and merge it with the original audio."""

import os
import re
import subprocess
import tempfile
from pathlib import Path
from typing import List, Tuple

from parser import VTTSegment


def _get_audio_duration_ms(path: str) -> int:
    """Return duration of an audio file in milliseconds via ffprobe."""
    result = subprocess.run(
        [
            "ffprobe",
            "-v", "error",
            "-show_entries", "format=duration",
            "-of", "default=noprint_wrappers=1:nokey=1",
            path,
        ],
        capture_output=True,
        text=True,
        check=True,
    )
    return int(float(result.stdout.strip()) * 1000)


def _adjust_speed_ffmpeg(input_path: str, output_path: str, factor: float) -> None:
    """Re-encode audio at *factor* speed using ffmpeg's atempo filter.

    atempo is clamped to [0.5, 2.0], so we chain filters for factors > 2.
    """
    if factor <= 2.0:
        atempo = f"atempo={factor:.4f}"
    else:
        # Two-stage chaining: each stage max 2.0x
        f1 = min(factor ** 0.5, 2.0)
        f2 = factor / f1
        atempo = f"atempo={f1:.4f},atempo={f2:.4f}"

    subprocess.run(
        ["ffmpeg", "-y", "-i", input_path, "-af", atempo, output_path],
        check=True,
        capture_output=True,
    )


def _channel_mean_volume_db(
    audio_path: str,
    start_ms: int,
    duration_s: float,
    channel_idx: int,
) -> float:
    """Return mean volume (dB) of a single channel (0=left, 1=right).

    Extracts the channel with the pan filter so the result is unambiguous.
    Returns -inf on silence or any error.
    """
    result = subprocess.run(
        [
            "ffmpeg",
            "-ss", f"{start_ms / 1000:.3f}",
            "-t", f"{duration_s:.3f}",
            "-i", audio_path,
            "-af", f"pan=mono|c0=c{channel_idx},volumedetect",
            "-f", "null", "-",
        ],
        capture_output=True,
        text=True,
    )
    m = re.search(r"mean_volume:\s*([-\d.]+)\s*dB", result.stderr)
    if not m:
        return float("-inf")
    return float(m.group(1))


def detect_voice_channel(
    audio_path: str,
    start_ms: int,
    end_ms: int,
    threshold_db: float = 3.0,
) -> str:
    """Detect which stereo channel carries more energy in the given time range.

    Returns "left", "right", or "center" (balanced / undetectable).
    Falls back to "center" on any error so panning degrades gracefully.
    """
    duration_s = (end_ms - start_ms) / 1000
    if duration_s <= 0:
        return "center"

    left_db = _channel_mean_volume_db(audio_path, start_ms, duration_s, 0)
    right_db = _channel_mean_volume_db(audio_path, start_ms, duration_s, 1)

    diff = left_db - right_db  # positive → left is louder
    if diff > threshold_db:
        return "left"
    if diff < -threshold_db:
        return "right"
    return "center"


def _pan_mono_to_stereo(
    input_path: str,
    output_path: str,
    channel: str,
    tts_volume: float,
    bleed: float = 0.3,
) -> None:
    """Convert a mono TTS clip to stereo with directional panning.

    *channel* is where the original voice was detected:
      "left"   → TTS goes right (full), left gets *bleed* fraction
      "right"  → TTS goes left (full), right gets *bleed* fraction
      "center" → TTS plays equally on both sides

    *tts_volume* is the overall amplitude scale (0–1) applied here so
    the final amix step can use weight=1.
    """
    vol = tts_volume
    bl = tts_volume * bleed

    if channel == "left":
        pan = f"pan=stereo|c0={bl:.4f}*c0|c1={vol:.4f}*c0"
    elif channel == "right":
        pan = f"pan=stereo|c0={vol:.4f}*c0|c1={bl:.4f}*c0"
    else:
        pan = f"pan=stereo|c0={vol:.4f}*c0|c1={vol:.4f}*c0"

    subprocess.run(
        ["ffmpeg", "-y", "-i", input_path, "-af", pan, output_path],
        check=True,
        capture_output=True,
    )


def generate_tts_clips(
    segments: List[VTTSegment],
    provider,
    tmp_dir: str,
    original_path: str,
    tts_volume: float,
    speed_up: bool = True,
) -> List[Tuple[int, str]]:
    """Generate TTS audio clips for each segment.

    Each clip is panned to stereo based on which channel the original voice
    occupies in that time window. Volume is baked into the pan filter so the
    final amix step can use weight=1.

    Returns a list of (start_ms, stereo_clip_path) tuples.
    Only segments with successfully generated audio are included.
    """
    clips: List[Tuple[int, str]] = []
    total = len(segments)

    for seg in segments:
        raw_path = os.path.join(tmp_dir, f"tts_raw_{seg.index}.mp3")
        final_path = os.path.join(tmp_dir, f"tts_{seg.index}.mp3")

        print(
            f"  [{seg.index + 1}/{total}] {seg.start_ms / 1000:.1f}s  {seg.text[:50]}"
        )

        max_attempts = 3
        last_exc: Exception | None = None
        for attempt in range(1, max_attempts + 1):
            try:
                provider.generate(seg.text, raw_path)
                last_exc = None
                break
            except Exception as exc:
                last_exc = exc
                print(f"    attempt {attempt}/{max_attempts} failed: {exc}")
        if last_exc is not None:
            print(f"    WARNING: skipping segment {seg.index} after {max_attempts} attempts")
            continue

        if not os.path.exists(raw_path) or os.path.getsize(raw_path) == 0:
            print(f"    WARNING: TTS produced no output for segment {seg.index}")
            continue

        # Optionally speed up if the clip is longer than the subtitle window
        if speed_up and seg.duration_ms > 0:
            try:
                clip_ms = _get_audio_duration_ms(raw_path)
                if clip_ms > seg.duration_ms:
                    factor = clip_ms / seg.duration_ms
                    if factor <= 4.0:
                        _adjust_speed_ffmpeg(raw_path, final_path, factor)
                    else:
                        # Too extreme — truncate instead
                        subprocess.run(
                            [
                                "ffmpeg", "-y",
                                "-i", raw_path,
                                "-t", str(seg.duration_ms / 1000),
                                final_path,
                            ],
                            check=True,
                            capture_output=True,
                        )
                else:
                    os.rename(raw_path, final_path)
            except Exception as exc:
                print(f"    WARNING: Speed adjustment failed: {exc}. Using raw clip.")
                os.rename(raw_path, final_path)
        else:
            os.rename(raw_path, final_path)

        # Detect voice channel and pan TTS to the opposite side
        panned_path = os.path.join(tmp_dir, f"tts_panned_{seg.index}.mp3")
        try:
            ch = detect_voice_channel(original_path, seg.start_ms, seg.end_ms)
            _pan_mono_to_stereo(final_path, panned_path, ch, tts_volume)
            print(f"    voice channel: {ch}")
        except Exception as exc:
            print(f"    WARNING: panning failed ({exc}), using mono fallback")
            _pan_mono_to_stereo(final_path, panned_path, "center", tts_volume)

        clips.append((seg.start_ms, panned_path))

    return clips


def build_tts_track(
    clips: List[Tuple[int, str]],
    total_duration_ms: int,
    tmp_dir: str,
) -> str:
    """Combine all timed TTS clips into a single audio track using ffmpeg.

    Uses adelay to position each clip, then amix to merge them all.
    Returns path to the combined TTS track file.
    """
    if not clips:
        raise ValueError("No TTS clips to combine.")

    output_path = os.path.join(tmp_dir, "tts_track.mp3")

    if len(clips) == 1:
        start_ms, clip_path = clips[0]
        # Single clip: apply delay and pad to full duration
        subprocess.run(
            [
                "ffmpeg", "-y",
                "-i", clip_path,
                "-af",
                f"adelay={start_ms}|{start_ms},apad=whole_dur={total_duration_ms / 1000:.3f}",
                output_path,
            ],
            check=True,
            capture_output=True,
        )
        return output_path

    # Build filter_complex for multiple clips
    inputs = []
    filter_parts = []
    labels = []

    for i, (start_ms, clip_path) in enumerate(clips):
        inputs += ["-i", clip_path]
        label = f"[a{i}]"
        filter_parts.append(
            f"[{i}:a]adelay={start_ms}|{start_ms},apad=whole_dur={total_duration_ms / 1000:.3f}{label}"
        )
        labels.append(label)

    n = len(clips)
    mix_inputs = "".join(labels)
    filter_parts.append(
        f"{mix_inputs}amix=inputs={n}:normalize=0:dropout_transition=0[out]"
    )

    filter_complex = ";".join(filter_parts)

    subprocess.run(
        ["ffmpeg", "-y"]
        + inputs
        + ["-filter_complex", filter_complex, "-map", "[out]", output_path],
        check=True,
        capture_output=True,
    )
    return output_path


def merge_with_original(
    original_path: str,
    tts_track_path: str,
    output_path: str,
    tts_volume: float = 1.0,
) -> None:
    """Mix the TTS track on top of the original audio and write *output_path*.

    *tts_volume* is a linear amplitude multiplier for the TTS track (0–1).
    """
    ext = Path(output_path).suffix.lower().lstrip(".")
    codec_args: List[str]

    if ext == "mp3":
        codec_args = ["-c:a", "libmp3lame", "-q:a", "2"]
    elif ext in ("m4a", "aac"):
        codec_args = ["-c:a", "aac", "-b:a", "192k"]
    elif ext == "ogg":
        codec_args = ["-c:a", "libvorbis", "-q:a", "6"]
    else:
        codec_args = ["-c:a", "pcm_s16le"]  # wav fallback

    # amix weights: original=1.0, tts=tts_volume
    subprocess.run(
        [
            "ffmpeg", "-y",
            "-i", original_path,
            "-i", tts_track_path,
            "-filter_complex",
            f"[0:a][1:a]amix=inputs=2:weights=1 {tts_volume:.3f}:normalize=0[out]",
            "-map", "[out]",
        ]
        + codec_args
        + [output_path],
        check=True,
    )
