# Phase 4 — TTS 质量增强 + /rj 拟声词过滤 + 重复台词去重

本文档说明本次需求的代码改动与使用方法（参数、环境变量、流程）。

## 1. 本次改动概览

### 1.1 核心能力

1. TTS 质量增强（线上优先）
- OpenAI provider 新增模型选项：`gpt-4o-mini-tts`
- Azure provider 增加 SSML 高级可控能力（style/role/styledegree、phoneme、lexicon）

2. 字幕预处理增强
- 在 Python 主流程中新增预处理模块：`cli/vtt_preprocessor.py`
- 先做“同一个 cue 内重复台词去重”，避免多人同句重复生成 TTS
- 可选开启“仅拟声词句子过滤”（通过 OpenRouter 判定）

3. /rj 流程新增开关（默认开启）
- 配置步骤从 `provider -> volume -> speedup` 扩展为
  `provider -> volume -> speedup -> onomatopoeia`
- 该开关仅在 `/rj` 流程出现；`/new` 与 Web 前端配置不变

4. 后端配置与存储打通
- `domain.JobConfig` 增加 `FilterOnomatopoeia bool`
- SQLite/Postgres `jobs` 表增加 `filter_onomatopoeia` 字段（带兼容迁移）
- HTTP `CreateJobRequest` 增加可选 `filter_onomatopoeia` 字段

### 1.2 兼容性

- 旧任务与旧数据可继续读取（新字段有默认值 `false`）
- OpenRouter 调用失败时会自动降级为“不过滤继续处理”，不会让任务失败

---

## 2. 使用方法

### 2.1 Telegram `/rj` 使用

1. 绑定 token：`/asmr_bind <your_jwt>`
2. 进入作品：`/rj RJxxxxxx`
3. 选择音频后进入配置向导：
- 选择 TTS provider
- 选择音量
- 选择是否加速
- 选择是否过滤拟声词（推荐开启，默认开启）
4. 确认任务

### 2.2 CLI 使用

基础：

```bash
python cli/main.py <audio> <vtt> <output>
```

开启拟声词过滤：

```bash
python cli/main.py audio.mp3 audio.vtt output.mp3 --filter-onomatopoeia
```

OpenAI 使用 `gpt-4o-mini-tts`：

```bash
python cli/main.py audio.mp3 audio.vtt output.mp3 \
  --tts openai \
  --openai-key "$OPENAI_API_KEY" \
  --openai-model gpt-4o-mini-tts
```

Azure 高级可控示例：

```bash
export AZURE_TTS_KEY=xxx
export AZURE_TTS_REGION=eastus
export AZURE_TTS_STYLE=whispering
export AZURE_TTS_ROLE=YoungAdultFemale
export AZURE_TTS_STYLE_DEGREE=1.2
export AZURE_TTS_PHONEME_MAP_JSON='{"重":"chong2"}'
export AZURE_TTS_LEXICON_URI='https://example.com/zh-lexicon.pls'

python cli/main.py audio.mp3 audio.vtt output.mp3 --tts azure
```

---

## 3. 新增/变更参数

### 3.1 `cli/main.py` 新参数

- `--filter-onomatopoeia`
  - 启用“仅拟声词字幕行过滤”
- `--openrouter-api-key KEY`
  - OpenRouter API Key（可不传，改用环境变量）
- `--openrouter-model MODEL`
  - 拟声词判定模型（默认 `openai/gpt-4o-mini`）
- `--openrouter-base-url URL`
  - OpenRouter Base URL（默认 `https://openrouter.ai/api/v1`）

### 3.2 `cli/main.py` 现有参数增强

- `--openai-model` 新增可选值：`gpt-4o-mini-tts`

### 3.3 HTTP API 参数

`POST /api/jobs` 请求体新增可选字段：

- `filter_onomatopoeia: boolean`（默认 `false`）

---

## 4. 环境变量配置

### 4.1 OpenRouter（拟声词过滤）

- `OPENROUTER_API_KEY`
- `OPENROUTER_MODEL`（默认：`openai/gpt-4o-mini`）
- `OPENROUTER_BASE_URL`（默认：`https://openrouter.ai/api/v1`）

### 4.2 Azure（可选高级控制）

- `AZURE_TTS_KEY`
- `AZURE_TTS_REGION`
- `AZURE_TTS_STYLE`
- `AZURE_TTS_ROLE`
- `AZURE_TTS_STYLE_DEGREE`
- `AZURE_TTS_PHONEME_MAP_JSON`
- `AZURE_TTS_PHONEME_MAP_FILE`
- `AZURE_TTS_LEXICON_URI`

---

## 5. 字幕预处理规则

### 5.1 重复台词去重

- 作用范围：同一个 cue 内
- 规则：文本归一化后完全相同的行仅保留第一行
- 目标：避免多人同句时重复生成 TTS（减少强行加速）

### 5.2 拟声词过滤

- 作用范围：开启 `--filter-onomatopoeia` 或 `/rj` 开关为开启时
- 规则：通过 OpenRouter 批量判定每行是否“仅拟声词”
- 结果：
  - 仅拟声词行：跳过
  - 含语义台词行：保留
  - 若一个 cue 全部被过滤：该 cue 整体跳过

---

## 6. 主要改动文件

- `cli/vtt_preprocessor.py`
- `cli/parser.py`
- `cli/main.py`
- `cli/tts.py`
- `server/domain/job.go`
- `server/infrastructure/persistence/sqlite_job_repo.go`
- `server/infrastructure/persistence/postgres_job_repo.go`
- `server/interfaces/telegram/bot.go`
- `server/interfaces/telegram/rj_handlers.go`
- `server/interfaces/http/dto.go`
- `server/infrastructure/translator/python_translator.go`
- `server/infrastructure/config/config.go`
