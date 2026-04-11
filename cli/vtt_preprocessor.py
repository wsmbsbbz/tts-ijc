"""Subtitle preprocessor for TTS generation.

Pipeline:
1) Remove duplicate lines inside one cue (keeps the first line only).
2) Optionally filter lines that are onomatopoeia-only via OpenRouter.
"""

import json
import os
import re
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Dict, List, Sequence

from parser import VTTSegment


DEFAULT_OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1"
DEFAULT_OPENROUTER_MODEL = "openai/gpt-4o-mini"


@dataclass
class PreprocessStats:
    input_segments: int
    output_segments: int
    duplicate_lines_removed: int
    onomatopoeia_lines_removed: int


def _normalize_line_for_dedupe(text: str) -> str:
    """Normalize subtitle line for duplicate checks inside one cue."""
    s = text.strip()
    s = re.sub(r"^[\-–—・•]+", "", s).strip()
    s = s.replace("…", "...")
    s = re.sub(r"\s+", "", s)
    return s.lower()


def _dedupe_lines(lines: Sequence[str]) -> tuple[List[str], int]:
    seen = set()
    out: List[str] = []
    removed = 0
    for line in lines:
        key = _normalize_line_for_dedupe(line)
        if not key:
            continue
        if key in seen:
            removed += 1
            continue
        seen.add(key)
        out.append(line)
    return out, removed


def _strip_json_fence(text: str) -> str:
    t = text.strip()
    if t.startswith("```"):
        t = re.sub(r"^```[a-zA-Z]*\n?", "", t)
        if t.endswith("```"):
            t = t[:-3]
    return t.strip()


def _extract_message_text(content) -> str:
    if isinstance(content, str):
        return content
    # Some providers return an array of typed chunks.
    if isinstance(content, list):
        parts: List[str] = []
        for item in content:
            if isinstance(item, dict):
                value = item.get("text")
                if isinstance(value, str):
                    parts.append(value)
        return "".join(parts)
    return ""


def _classify_onomatopoeia_batch(
    lines: Sequence[str],
    api_key: str,
    model: str,
    base_url: str,
    timeout_s: int,
) -> List[bool]:
    prompt_lines = "\n".join(f"{idx + 1}. {line}" for idx, line in enumerate(lines))
    user_prompt = (
        "Classify each subtitle line as onomatopoeia-only or not.\n"
        "Onomatopoeia-only means the line has no semantic dialogue content, only sound words/repetitions like "
        "'转...转...转', '嗯嗯', '哈啊', '揉揉挠挠'.\n"
        "Return JSON only with exact shape: {\"is_onomatopoeia\":[true,false,...]} "
        "and same length/order as input.\n\n"
        f"Input lines:\n{prompt_lines}"
    )
    payload = {
        "model": model,
        "temperature": 0,
        "messages": [
            {
                "role": "system",
                "content": "You are a strict JSON classifier. Output valid JSON only.",
            },
            {
                "role": "user",
                "content": user_prompt,
            },
        ],
    }

    endpoint = base_url.rstrip("/") + "/chat/completions"
    req = urllib.request.Request(
        endpoint,
        method="POST",
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
    )

    try:
        with urllib.request.urlopen(req, timeout=timeout_s) as resp:
            body = resp.read().decode("utf-8")
    except urllib.error.URLError as exc:
        raise RuntimeError(f"OpenRouter request failed: {exc}") from exc

    data = json.loads(body)
    choices = data.get("choices")
    if not choices:
        raise RuntimeError("OpenRouter returned no choices")
    message = choices[0].get("message", {})
    content = _extract_message_text(message.get("content"))
    if not content:
        raise RuntimeError("OpenRouter returned empty content")
    parsed = json.loads(_strip_json_fence(content))

    flags = parsed.get("is_onomatopoeia")
    if not isinstance(flags, list):
        raise RuntimeError("OpenRouter response missing is_onomatopoeia list")
    if len(flags) != len(lines):
        raise RuntimeError("OpenRouter response length mismatch")
    out: List[bool] = []
    for item in flags:
        out.append(bool(item))
    return out


def _classify_lines_with_openrouter(
    lines: Sequence[str],
    api_key: str,
    model: str,
    base_url: str,
    timeout_s: int = 30,
    batch_size: int = 20,
) -> Dict[str, bool]:
    result: Dict[str, bool] = {}
    pending = [line for line in lines if line not in result]
    for i in range(0, len(pending), batch_size):
        batch = pending[i : i + batch_size]
        flags = _classify_onomatopoeia_batch(
            lines=batch,
            api_key=api_key,
            model=model,
            base_url=base_url,
            timeout_s=timeout_s,
        )
        for line, flag in zip(batch, flags):
            result[line] = flag
    return result


def preprocess_segments(
    segments: Sequence[VTTSegment],
    enable_onomatopoeia_filter: bool = False,
    openrouter_api_key: str | None = None,
    openrouter_model: str | None = None,
    openrouter_base_url: str | None = None,
) -> tuple[List[VTTSegment], PreprocessStats]:
    """Preprocess parsed VTT segments before TTS generation.

    Returns processed segments plus statistics.
    """
    deduped: List[VTTSegment] = []
    duplicate_lines_removed = 0

    for seg in segments:
        base_lines = list(seg.lines) if seg.lines else [seg.text]
        lines, removed = _dedupe_lines(base_lines)
        duplicate_lines_removed += removed
        if not lines:
            continue
        deduped.append(
            VTTSegment(
                index=seg.index,
                start_ms=seg.start_ms,
                end_ms=seg.end_ms,
                text=" ".join(lines),
                lines=lines,
            )
        )

    onomatopoeia_lines_removed = 0
    if not enable_onomatopoeia_filter:
        out = [
            VTTSegment(
                index=i,
                start_ms=seg.start_ms,
                end_ms=seg.end_ms,
                text=seg.text,
                lines=seg.lines,
            )
            for i, seg in enumerate(deduped)
        ]
        return out, PreprocessStats(
            input_segments=len(segments),
            output_segments=len(out),
            duplicate_lines_removed=duplicate_lines_removed,
            onomatopoeia_lines_removed=onomatopoeia_lines_removed,
        )

    api_key = openrouter_api_key or os.getenv("OPENROUTER_API_KEY", "")
    model = openrouter_model or os.getenv("OPENROUTER_MODEL", DEFAULT_OPENROUTER_MODEL)
    base_url = openrouter_base_url or os.getenv("OPENROUTER_BASE_URL", DEFAULT_OPENROUTER_BASE_URL)
    if not api_key:
        print("WARNING: OPENROUTER_API_KEY is not set, skip onomatopoeia filtering.")
        out = [
            VTTSegment(
                index=i,
                start_ms=seg.start_ms,
                end_ms=seg.end_ms,
                text=seg.text,
                lines=seg.lines,
            )
            for i, seg in enumerate(deduped)
        ]
        return out, PreprocessStats(
            input_segments=len(segments),
            output_segments=len(out),
            duplicate_lines_removed=duplicate_lines_removed,
            onomatopoeia_lines_removed=onomatopoeia_lines_removed,
        )

    unique_lines: List[str] = []
    seen = set()
    for seg in deduped:
        for line in seg.lines:
            if line not in seen:
                unique_lines.append(line)
                seen.add(line)

    line_flags: Dict[str, bool]
    try:
        line_flags = _classify_lines_with_openrouter(
            lines=unique_lines,
            api_key=api_key,
            model=model,
            base_url=base_url,
        )
    except Exception as exc:
        print(f"WARNING: onomatopoeia filter unavailable ({exc}), continue without filtering.")
        line_flags = {}

    filtered: List[VTTSegment] = []
    for seg in deduped:
        kept_lines: List[str] = []
        for line in seg.lines:
            is_onomatopoeia = line_flags.get(line, False)
            if is_onomatopoeia:
                onomatopoeia_lines_removed += 1
                continue
            kept_lines.append(line)

        if not kept_lines:
            continue
        filtered.append(
            VTTSegment(
                index=seg.index,
                start_ms=seg.start_ms,
                end_ms=seg.end_ms,
                text=" ".join(kept_lines),
                lines=kept_lines,
            )
        )

    out = [
        VTTSegment(
            index=i,
            start_ms=seg.start_ms,
            end_ms=seg.end_ms,
            text=seg.text,
            lines=seg.lines,
        )
        for i, seg in enumerate(filtered)
    ]
    return out, PreprocessStats(
        input_segments=len(segments),
        output_segments=len(out),
        duplicate_lines_removed=duplicate_lines_removed,
        onomatopoeia_lines_removed=onomatopoeia_lines_removed,
    )
