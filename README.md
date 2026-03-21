# translation-combinator

把翻译好的 VTT 字幕转成 TTS 语音，并按时间轴混入原始音频，输出带"翻译旁白"的音频文件。

不懂原始语言的人无需阅读字幕，直接通过叠加的语音旁白就能听懂内容。

## 工作原理

```
原始音频 + 翻译字幕(.vtt)
         │
         ▼
   1. 解析 VTT 时间轴
         │
         ▼
   2. 并发调用 TTS API 生成各段语音
      ├── 自动检测原声道（左/右/中）
      ├── 必要时自动加速/截断，使语音契合字幕窗口
      └── 声像处理：TTS 语音偏移到对侧声道
         │
         ▼
   3. 按时间轴拼合 TTS 音轨
         │
         ▼
   4. 与原始音频混音，输出最终文件
```

## 依赖

**系统依赖（必需）**

```bash
# macOS
brew install ffmpeg

# Ubuntu/Debian
sudo apt install ffmpeg

# Windows: https://ffmpeg.org/download.html
```

**Python 依赖**

```bash
# 默认 TTS（免费，无需 API key）
pip install edge-tts

# 可选 TTS 提供方
pip install gTTS                              # gTTS
pip install azure-cognitiveservices-speech    # Azure
pip install openai                            # OpenAI
pip install google-cloud-texttospeech         # Google Cloud
```

## 快速开始

```bash
python main.py audio.mp3 audio.vtt output.mp3
```

## 用法

```
python main.py <input_audio> <input_vtt> <output_audio> [选项]
```

### 基本示例

```bash
# 使用默认 Edge TTS（免费，推荐）
python main.py audio.mp3 audio.vtt output.mp3

# 指定 Edge TTS 语音
python main.py audio.mp3 audio.vtt output.mp3 --tts edge --edge-voice zh-TW-HsiaoChenNeural

# 使用 gTTS（免费）
python main.py audio.mp3 audio.vtt output.mp3 --tts gtts

# 调整 TTS 音量（默认 0.08，范围 0–1）
python main.py audio.mp3 audio.vtt output.mp3 --tts-volume 0.12

# 禁用自动加速（保持 TTS 原速）
python main.py audio.mp3 audio.vtt output.mp3 --no-speedup
```

### TTS 提供方对比

| 提供方 | 免费额度 | 音质 | 需要 API Key |
|--------|----------|------|--------------|
| `edge` | 完全免费 | ★★★★★ | 否 |
| `gtts` | 完全免费 | ★★★ | 否 |
| `azure` | 500K 字/月 | ★★★★★ | 是 |
| `openai` | 按量付费 | ★★★★★ | 是 |
| `gcloud` | 1M 字/月 | ★★★★ | 是 |

### 付费 TTS 示例

```bash
# Azure（推荐付费选项，有免费额度）
python main.py audio.mp3 audio.vtt output.mp3 --tts azure \
    --azure-key YOUR_KEY --azure-region eastus

# 或通过环境变量传入 key
export AZURE_TTS_KEY=YOUR_KEY
export AZURE_TTS_REGION=eastus
python main.py audio.mp3 audio.vtt output.mp3 --tts azure

# OpenAI
python main.py audio.mp3 audio.vtt output.mp3 --tts openai --openai-key YOUR_KEY

# Google Cloud
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
python main.py audio.mp3 audio.vtt output.mp3 --tts gcloud
```

## 完整参数说明

```
位置参数:
  audio                   输入音频文件（mp3、m4a、wav、ogg 等）
  vtt                     翻译好的 VTT 字幕文件
  output                  输出文件路径

TTS 选项:
  --tts PROVIDER          TTS 提供方: edge（默认）, gtts, azure, openai, gcloud

Edge TTS 选项:
  --edge-voice VOICE      语音名称（默认: zh-CN-XiaoxiaoNeural）
                          可运行 'edge-tts --list-voices' 查看所有语音

gTTS 选项:
  --gtts-lang LANG        语言代码（默认: zh-CN）

Azure TTS 选项:
  --azure-key KEY         Azure Speech API Key（或设置 AZURE_TTS_KEY）
  --azure-region REGION   Azure 区域，如 eastus（或设置 AZURE_TTS_REGION）
  --azure-voice VOICE     语音名称（默认: zh-CN-XiaoxiaoNeural）

OpenAI TTS 选项:
  --openai-key KEY        OpenAI API Key（或设置 OPENAI_API_KEY）
  --openai-voice VOICE    语音: alloy（默认）, echo, fable, onyx, nova, shimmer
  --openai-model MODEL    模型: tts-1（默认）, tts-1-hd

Google Cloud TTS 选项:
  --gcloud-key KEY        API Key（或设置 GOOGLE_APPLICATION_CREDENTIALS）
  --gcloud-voice VOICE    语音名称（默认: cmn-CN-Wavenet-A）

混音选项:
  --tts-volume N          TTS 音量，范围 0–1（默认: 0.08）
  --concurrency N         最大并发 TTS 请求数（默认: 5）
  --no-speedup            禁用自动加速
  --keep-tmp              保留临时 TTS 片段文件
```

## VTT 字幕格式

标准 WebVTT 格式，示例：

```
WEBVTT

00:00:01.000 --> 00:00:04.000
Hello, welcome to this video.

00:00:05.500 --> 00:00:09.000
Today we'll discuss an important topic.
```

## 特性说明

**自动加速** — TTS 生成的语音如果比字幕窗口长，会自动加速以适配时间轴（最大 4x），超出则截断。

**声像处理** — 自动检测每段原声的主声道（左/右），TTS 语音偏移到对侧，使旁白与原声更易区分。

**并发生成** — 所有字幕段的 TTS 请求并发执行（默认 5 路），显著减少等待时间。

**WAV 自动压缩** — 若输入和输出均为 WAV，输出自动转为 MP3（体积约缩小 10x）。

## 项目结构

```
translation-combinator/
├── main.py     # CLI 入口，参数解析与主流程
├── parser.py   # VTT 字幕解析
├── tts.py      # TTS 提供方实现（edge/gtts/azure/openai/gcloud）
└── mixer.py    # TTS 片段生成、时间轴对齐、音频混音
```
