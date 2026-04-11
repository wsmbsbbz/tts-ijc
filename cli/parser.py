"""VTT subtitle file parser."""

import re
from dataclasses import dataclass
from typing import List


@dataclass
class VTTSegment:
    index: int
    start_ms: int
    end_ms: int
    text: str
    lines: List[str]

    @property
    def duration_ms(self) -> int:
        return self.end_ms - self.start_ms


def _timestamp_to_ms(ts: str) -> int:
    """Convert HH:MM:SS.mmm or MM:SS.mmm to milliseconds."""
    ts = ts.replace(",", ".")
    parts = ts.split(":")

    if len(parts) == 3:
        hours, minutes, sec_frac = int(parts[0]), int(parts[1]), parts[2]
    elif len(parts) == 2:
        hours, minutes, sec_frac = 0, int(parts[0]), parts[1]
    else:
        raise ValueError(f"Invalid timestamp format: {ts}")

    sec_parts = sec_frac.split(".")
    seconds = int(sec_parts[0])
    ms = int(sec_parts[1].ljust(3, "0")[:3]) if len(sec_parts) > 1 else 0

    return (hours * 3600 + minutes * 60 + seconds) * 1000 + ms


def _clean_line(raw: str) -> str:
    """Remove VTT markup and normalize whitespace for one subtitle line."""
    # Remove HTML-like tags (voice tags, bold, italic, etc.)
    text = re.sub(r"<[^>]+>", "", raw)
    # Collapse whitespace and trim
    text = re.sub(r"\s+", " ", text.strip())
    return text


def parse_vtt(path: str) -> List[VTTSegment]:
    """Parse a WebVTT file and return a list of subtitle segments."""
    with open(path, "r", encoding="utf-8-sig") as f:
        content = f.read()

    # Split on blank lines to get cue blocks
    blocks = re.split(r"\n[ \t]*\n", content.strip())

    segments: List[VTTSegment] = []
    segment_index = 0

    for block in blocks:
        lines = [ln.rstrip() for ln in block.strip().splitlines()]
        if not lines:
            continue

        # Skip the WEBVTT file header and NOTE/STYLE/REGION blocks
        first = lines[0]
        if first.startswith("WEBVTT") or first.startswith("NOTE") \
                or first.startswith("STYLE") or first.startswith("REGION"):
            continue

        # Find the timestamp line (may be preceded by a cue identifier)
        ts_line_idx = None
        for i, line in enumerate(lines):
            if "-->" in line:
                ts_line_idx = i
                break

        if ts_line_idx is None:
            continue

        ts_line = lines[ts_line_idx]
        # Timestamp line may have positioning metadata after the times
        ts_match = re.match(
            r"([\d:]+[.,]\d+)\s*-->\s*([\d:]+[.,]\d+)",
            ts_line,
        )
        if not ts_match:
            continue

        try:
            start_ms = _timestamp_to_ms(ts_match.group(1))
            end_ms = _timestamp_to_ms(ts_match.group(2))
        except ValueError:
            continue

        raw_text_lines = lines[ts_line_idx + 1 :]
        text_lines = [_clean_line(line) for line in raw_text_lines]
        text_lines = [line for line in text_lines if line]
        text = " ".join(text_lines)

        if text:
            segments.append(
                VTTSegment(
                    index=segment_index,
                    start_ms=start_ms,
                    end_ms=end_ms,
                    text=text,
                    lines=text_lines,
                )
            )
            segment_index += 1

    return segments
