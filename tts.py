"""TTS provider implementations.

Supported providers
-------------------
edge    Microsoft Edge TTS — free, no API key, high quality
gtts    Google Translate TTS — free, no API key
azure   Azure Cognitive Services TTS — needs AZURE_TTS_KEY + AZURE_TTS_REGION
openai  OpenAI TTS — needs OPENAI_API_KEY
gcloud  Google Cloud TTS — needs GOOGLE_APPLICATION_CREDENTIALS or GCLOUD_TTS_KEY
"""

import asyncio
import os
import subprocess
from abc import ABC, abstractmethod
from pathlib import Path
from typing import Optional


class TTSError(Exception):
    """Raised when TTS generation fails."""


class TTSProvider(ABC):
    """Abstract base for all TTS providers."""

    @abstractmethod
    def generate(self, text: str, output_path: str) -> None:
        """Generate speech for *text* and write it to *output_path* (sync)."""

    async def async_generate(self, text: str, output_path: str) -> None:
        """Async version — default wraps sync generate in a thread pool."""
        await asyncio.to_thread(self.generate, text, output_path)

    @property
    @abstractmethod
    def name(self) -> str:
        """Human-readable provider name."""


# ---------------------------------------------------------------------------
# Edge TTS  (free, no API key)
# ---------------------------------------------------------------------------

class EdgeTTSProvider(TTSProvider):
    """Uses the edge-tts package (Microsoft Edge read-aloud service)."""

    DEFAULT_VOICE = "zh-CN-XiaoxiaoNeural"

    def __init__(self, voice: Optional[str] = None):
        self._voice = voice or self.DEFAULT_VOICE

    @property
    def name(self) -> str:
        return f"edge ({self._voice})"

    async def async_generate(self, text: str, output_path: str) -> None:
        try:
            import edge_tts  # type: ignore
        except ImportError as exc:
            raise TTSError(
                "edge-tts not installed. Run: pip install edge-tts"
            ) from exc
        communicate = edge_tts.Communicate(text, self._voice)
        await communicate.save(output_path)

    def generate(self, text: str, output_path: str) -> None:
        asyncio.run(self.async_generate(text, output_path))


# ---------------------------------------------------------------------------
# gTTS  (free, no API key — uses Google Translate)
# ---------------------------------------------------------------------------

class GTTSProvider(TTSProvider):
    """Uses the gTTS package (Google Translate text-to-speech)."""

    def __init__(self, lang: str = "zh-CN", tld: str = "com"):
        self._lang = lang.lower()
        self._tld = tld

    @property
    def name(self) -> str:
        return f"gtts (lang={self._lang})"

    def generate(self, text: str, output_path: str) -> None:
        try:
            from gtts import gTTS  # type: ignore
        except ImportError as exc:
            raise TTSError(
                "gTTS not installed. Run: pip install gTTS"
            ) from exc

        tts = gTTS(text=text, lang=self._lang, tld=self._tld, slow=False)
        tts.save(output_path)


# ---------------------------------------------------------------------------
# Azure Cognitive Services TTS
# ---------------------------------------------------------------------------

class AzureTTSProvider(TTSProvider):
    """Uses Azure Cognitive Services Speech SDK."""

    DEFAULT_VOICE = "zh-CN-XiaoxiaoNeural"

    def __init__(
        self,
        api_key: Optional[str] = None,
        region: Optional[str] = None,
        voice: Optional[str] = None,
    ):
        self._api_key = api_key or os.environ.get("AZURE_TTS_KEY", "")
        self._region = region or os.environ.get("AZURE_TTS_REGION", "")
        self._voice = voice or self.DEFAULT_VOICE

        if not self._api_key:
            raise TTSError(
                "Azure TTS requires an API key. "
                "Set --azure-key or AZURE_TTS_KEY environment variable."
            )
        if not self._region:
            raise TTSError(
                "Azure TTS requires a region. "
                "Set --azure-region or AZURE_TTS_REGION environment variable."
            )

    @property
    def name(self) -> str:
        return f"azure ({self._voice})"

    def generate(self, text: str, output_path: str) -> None:
        try:
            import azure.cognitiveservices.speech as speechsdk  # type: ignore
        except ImportError as exc:
            raise TTSError(
                "Azure Speech SDK not installed. "
                "Run: pip install azure-cognitiveservices-speech"
            ) from exc

        speech_config = speechsdk.SpeechConfig(
            subscription=self._api_key, region=self._region
        )
        speech_config.speech_synthesis_voice_name = self._voice
        audio_config = speechsdk.audio.AudioOutputConfig(filename=output_path)
        synthesizer = speechsdk.SpeechSynthesizer(
            speech_config=speech_config, audio_config=audio_config
        )
        result = synthesizer.speak_text_async(text).get()

        if result.reason != speechsdk.ResultReason.SynthesizingAudioCompleted:
            raise TTSError(f"Azure TTS failed: {result.reason}")


# ---------------------------------------------------------------------------
# OpenAI TTS
# ---------------------------------------------------------------------------

class OpenAITTSProvider(TTSProvider):
    """Uses the OpenAI TTS API."""

    VOICES = ("alloy", "echo", "fable", "onyx", "nova", "shimmer")

    def __init__(
        self,
        api_key: Optional[str] = None,
        voice: str = "alloy",
        model: str = "tts-1",
    ):
        self._api_key = api_key or os.environ.get("OPENAI_API_KEY", "")
        self._voice = voice
        self._model = model

        if not self._api_key:
            raise TTSError(
                "OpenAI TTS requires an API key. "
                "Set --openai-key or OPENAI_API_KEY environment variable."
            )

    @property
    def name(self) -> str:
        return f"openai ({self._voice}, {self._model})"

    def generate(self, text: str, output_path: str) -> None:
        try:
            from openai import OpenAI  # type: ignore
        except ImportError as exc:
            raise TTSError(
                "openai not installed. Run: pip install openai"
            ) from exc

        client = OpenAI(api_key=self._api_key)
        response = client.audio.speech.create(
            model=self._model,
            voice=self._voice,
            input=text,
        )
        response.stream_to_file(output_path)


# ---------------------------------------------------------------------------
# Google Cloud TTS
# ---------------------------------------------------------------------------

class GoogleCloudTTSProvider(TTSProvider):
    """Uses the Google Cloud Text-to-Speech API."""

    DEFAULT_VOICE = "cmn-CN-Wavenet-A"

    def __init__(
        self,
        api_key: Optional[str] = None,
        voice: Optional[str] = None,
        language_code: str = "cmn-CN",
    ):
        self._api_key = api_key or os.environ.get("GCLOUD_TTS_KEY", "")
        self._voice = voice or self.DEFAULT_VOICE
        self._language_code = language_code
        # If no explicit key is set, SDK uses GOOGLE_APPLICATION_CREDENTIALS

    @property
    def name(self) -> str:
        return f"gcloud ({self._voice})"

    def generate(self, text: str, output_path: str) -> None:
        try:
            from google.cloud import texttospeech  # type: ignore
        except ImportError as exc:
            raise TTSError(
                "google-cloud-texttospeech not installed. "
                "Run: pip install google-cloud-texttospeech"
            ) from exc

        if self._api_key:
            client = texttospeech.TextToSpeechClient(
                client_options={"api_key": self._api_key}
            )
        else:
            client = texttospeech.TextToSpeechClient()

        synthesis_input = texttospeech.SynthesisInput(text=text)
        voice_params = texttospeech.VoiceSelectionParams(
            language_code=self._language_code,
            name=self._voice,
        )
        audio_config = texttospeech.AudioConfig(
            audio_encoding=texttospeech.AudioEncoding.MP3
        )
        response = client.synthesize_speech(
            input=synthesis_input,
            voice=voice_params,
            audio_config=audio_config,
        )
        with open(output_path, "wb") as f:
            f.write(response.audio_content)


# ---------------------------------------------------------------------------
# Factory
# ---------------------------------------------------------------------------

def build_provider(args) -> TTSProvider:
    """Construct the appropriate TTSProvider from parsed CLI args."""
    provider = args.tts

    if provider == "edge":
        return EdgeTTSProvider(voice=args.edge_voice)

    if provider == "gtts":
        return GTTSProvider(lang=args.gtts_lang)

    if provider == "azure":
        return AzureTTSProvider(
            api_key=args.azure_key,
            region=args.azure_region,
            voice=args.azure_voice,
        )

    if provider == "openai":
        return OpenAITTSProvider(
            api_key=args.openai_key,
            voice=args.openai_voice,
            model=args.openai_model,
        )

    if provider == "gcloud":
        return GoogleCloudTTSProvider(
            api_key=args.gcloud_key,
            voice=args.gcloud_voice,
        )

    raise ValueError(f"Unknown TTS provider: {provider!r}")
