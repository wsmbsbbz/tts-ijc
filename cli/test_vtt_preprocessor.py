import unittest
from unittest.mock import patch

from parser import VTTSegment
from vtt_preprocessor import preprocess_segments


class VTTPreprocessorTests(unittest.TestCase):
    def test_dedupes_identical_lines_in_same_cue(self):
        segs = [
            VTTSegment(
                index=0,
                start_ms=0,
                end_ms=1000,
                text="-好了 -好了",
                lines=["-好了", "-好了"],
            )
        ]
        out, stats = preprocess_segments(segs, enable_onomatopoeia_filter=False)
        self.assertEqual(len(out), 1)
        self.assertEqual(out[0].lines, ["-好了"])
        self.assertEqual(stats.duplicate_lines_removed, 1)

    @patch("vtt_preprocessor._classify_lines_with_openrouter")
    def test_keeps_dialogue_drops_onomatopoeia_when_filter_enabled(self, classify_mock):
        classify_mock.return_value = {
            "-一定能通过乳头高潮": False,
            "-转…转…转": True,
        }
        segs = [
            VTTSegment(
                index=0,
                start_ms=0,
                end_ms=1000,
                text="-一定能通过乳头高潮 -转…转…转",
                lines=["-一定能通过乳头高潮", "-转…转…转"],
            )
        ]
        out, stats = preprocess_segments(
            segs,
            enable_onomatopoeia_filter=True,
            openrouter_api_key="dummy",
        )
        self.assertEqual(len(out), 1)
        self.assertEqual(out[0].lines, ["-一定能通过乳头高潮"])
        self.assertEqual(stats.onomatopoeia_lines_removed, 1)

    @patch("vtt_preprocessor._classify_lines_with_openrouter")
    def test_drops_segment_when_all_lines_are_onomatopoeia(self, classify_mock):
        classify_mock.return_value = {"-揉揉…挠挠……": True}
        segs = [
            VTTSegment(
                index=0,
                start_ms=0,
                end_ms=1000,
                text="-揉揉…挠挠……",
                lines=["-揉揉…挠挠……"],
            )
        ]
        out, stats = preprocess_segments(
            segs,
            enable_onomatopoeia_filter=True,
            openrouter_api_key="dummy",
        )
        self.assertEqual(len(out), 0)
        self.assertEqual(stats.onomatopoeia_lines_removed, 1)

    @patch("vtt_preprocessor._classify_lines_with_openrouter")
    def test_openrouter_failure_falls_back_without_filtering(self, classify_mock):
        classify_mock.side_effect = RuntimeError("timeout")
        segs = [
            VTTSegment(
                index=0,
                start_ms=0,
                end_ms=1000,
                text="-台词 -转…转…转",
                lines=["-台词", "-转…转…转"],
            )
        ]
        out, stats = preprocess_segments(
            segs,
            enable_onomatopoeia_filter=True,
            openrouter_api_key="dummy",
        )
        self.assertEqual(len(out), 1)
        self.assertEqual(out[0].lines, ["-台词", "-转…转…转"])
        self.assertEqual(stats.onomatopoeia_lines_removed, 0)


if __name__ == "__main__":
    unittest.main()
