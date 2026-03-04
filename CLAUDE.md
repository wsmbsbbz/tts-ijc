# CLAUDE.md

## Project
`translation-combinator` 用于把 VTT 字幕转成 TTS 语音，并按时间轴混入原始音频，输出带“翻译旁白”的音频文件。

## Core Flow
1. `main.py` 解析参数并校验输入/`ffmpeg`。
2. `parser.py` 解析 `.vtt` 为时间段文本（`VTTSegment`）。
3. `tts.py` 根据 `--tts` 构建提供方（edge/gtts/azure/openai/gcloud）。
4. `mixer.py` 并发生成片段、必要时加速/截断、按声道做声像处理、最终与原音频混音。

## Run
```bash
python main.py <input_audio> <input_vtt> <output_audio>
# example
python main.py audio.mp3 audio.vtt output.mp3 --tts edge
```

## Dependencies
- 必需：`ffmpeg` / `ffprobe`（系统命令）
- Python：`edge-tts`（默认）
- 可选：`gTTS`、`azure-cognitiveservices-speech`、`openai`、`google-cloud-texttospeech`

## Notes for Editing
- 默认入口是 `main.py`，尽量保持 CLI 参数向后兼容。
- 时间与混音逻辑集中在 `mixer.py`，修改时优先保留 `start_ms` 对齐语义。
- 新增 TTS 提供方时，在 `tts.py` 增加 Provider 类并接入 `build_provider()`。
