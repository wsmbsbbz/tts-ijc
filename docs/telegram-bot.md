# Telegram Bot

Telegram Bot 是 translation-combinator 的主要用户界面，通过 long-polling 接收消息，引导用户完成翻译任务。

## 架构概览

```
┌──────────────┐  long-poll   ┌──────────────┐
│  Telegram    │◄────────────►│  BotServer   │
│  Bot API     │  callbacks   │  (bot.go)    │
└──────────────┘              └──────┬───────┘
                                     │
                 ┌───────────────────┼───────────────────┐
                 │                   │                   │
          ┌──────▼──────┐    ┌──────▼──────┐    ┌──────▼──────┐
          │ stateStore  │    │  notifier   │    │ user_mapper │
          │ (state.go)  │    │(notifier.go)│    │(user_mapper │
          │             │    │             │    │        .go) │
          └─────────────┘    └──────┬──────┘    └─────────────┘
                                    │
                             ┌──────▼──────┐
                             │  JobService │
                             │  (app 层)   │
                             └──────┬──────┘
                                    │
                          ┌─────────▼─────────┐
                          │  WorkerService    │
                          │  下载 R2 文件       │
                          │  调用 Python CLI   │
                          │  上传结果到 R2      │
                          └───────────────────┘
```

## 文件结构

| 文件 | 职责 |
|------|------|
| `bot.go` | BotServer 主体：long-poll 循环、命令分发、上传工作流、配置步骤、回调路由 |
| `rj_handlers.go` | asmr.one RJ 工作流：绑定 token、浏览文件树、选择音频、下载并创建 job |
| `state.go` | 会话状态机定义（`session` 结构、`stateStore`） |
| `notifier.go` | Job 完成通知：轮询 job 状态、推送进度、交付结果文件 |
| `tgapi.go` | Telegram Bot API 封装（类型定义 + HTTP 调用） |
| `user_mapper.go` | Telegram 用户 → domain.User 映射（自动注册 / binding 查找） |

## 依赖

### 外部服务
- **Telegram Bot API** — long-polling (`getUpdates`)，消息/回调/文件收发
- **asmr.one API** — 作品元数据 (`/api/workInfo/:id`) 和文件树 (`/api/tracks/:id`)
- **Cloudflare R2** (或兼容 S3) — 文件存储（上传/下载/签名 URL）

### 内部依赖 (通过 `BotConfig` 注入)
- `JobService` — 创建 job、查询 job 列表/状态
- `AuthService` — `/bind` 命令的账号验证
- `FileStorage` (domain 接口) — R2 上传/下载/删除/签名 URL
- `UserRepository` — 用户 CRUD、配额计数
- `TelegramBindingRepository` — Telegram ID ↔ 用户绑定、asmr.one token 存储

## 会话状态机

```
stateIdle
  │
  ├─ /new ──► stateWaitingAudio ──► stateWaitingVTT ──► stateWaitingConfig ──► stateConfirming
  │                                                           │                      │
  │                                                     (provider →                  │
  │                                                      volume →               confirm/cancel
  │                                                      speedup)
  │
  └─ /rj ──► stateRJWaitingID ──► stateRJBrowse ──► stateWaitingConfig ──► stateConfirming
              (可跳过，若             (浏览/选择         (同上)                    │
               /rj 后直接             音频文件)                              handleRJConfirm
               带 RJ号)
```

### 配置子步骤 (`stateWaitingConfig`)
1. `configStepProvider` — 选择 TTS 提供方
2. `configStepVolume` — 选择 TTS 音量
3. `configStepSpeedup` — 是否启用语速加速
4. `configStepOnomatopoeia` — 仅 `/rj` 流程：是否过滤仅拟声词字幕行（默认开启）

## 工作流详解

### 1. 上传工作流 (`/new`)

```
用户 /new
  → bot 进入 stateWaitingAudio
  → 用户发送音频文件 (≤20MB)
  → bot 进入 stateWaitingVTT
  → 用户发送 .vtt 字幕文件
  → 进入配置步骤 (provider → volume → speedup)
  → 用户确认
  → bot 下载 Telegram 文件 → 上传到 R2 → 创建 Job
  → notifier 轮询 job 状态，推送进度，完成后发送结果
```

### 2. asmr.one 工作流 (`/rj`)

```
用户 /rj RJ299717
  → bot 调用 GetWorkInfo 检查 has_subtitle
  → 调用 GetTracks 获取文件树
  → 进入 stateRJBrowse（仅展示有配对字幕的音频）
  → 用户浏览文件夹、勾选音频（支持全选）
  → 用户点击 Done
  → 进入配置步骤 (provider → volume → speedup → onomatopoeia)
  → 用户确认
  → bot 逐个从 asmr.one 下载音频+配对VTT → 上传到 R2 → 创建 Job
  → notifier 轮询并通知
```

**字幕配对规则**：音频 `audio_name.mp3` 的配对字幕为同目录下的 `audio_name.mp3.vtt`。
只有存在配对字幕的音频才会展示给用户。空文件夹（无任何有字幕音频）自动隐藏。

### 3. 文件下载流程

无论哪种工作流，文件处理路径一致：

```
源文件 (Telegram / asmr.one)
  → HTTP GET 下载到本地 /tmp 临时文件
  → Storage.Upload() 上传到 R2（key = uploads/{userID}/{uuid}/{filename}）
  → 删除本地临时文件
  → JobService.CreateJob() 记录 R2 key
  → Worker 从 R2 下载 → 调用 Python CLI 处理 → 结果上传 R2
  → Notifier 生成签名 URL 发送给用户（或直接通过 Telegram 发文件 ≤50MB）
```

### 4. Job 完成通知

`notifier.watch()` 每 5 秒轮询 job 状态：
- **processing** → 转发进度消息给用户
- **completed** → 生成 24h 签名下载 URL；≤50MB 直接通过 Telegram `sendDocument` 发送
- **failed** → 发送错误信息

## 命令列表

| 命令 | 说明 |
|------|------|
| `/new` | 上传工作流：发送音频 + VTT 创建任务 |
| `/rj <RJxxxxxx>` | asmr.one 工作流：浏览并选择音频 |
| `/asmr_bind <token>` | 保存 asmr.one JWT token |
| `/asmr_unbind` | 移除 asmr.one token |
| `/jobs` | 列出已完成的 job（带下载按钮） |
| `/status [job_id]` | 查看最近 job 状态 |
| `/me` | 显示账户信息和配额 |
| `/bind <user> <pass>` | 绑定已有账户 |
| `/unbind` | 解除账户绑定 |
| `/cancel` | 取消当前操作 |
| `/help` | 显示帮助 |

## 回调路由 (Inline Keyboard)

| Prefix | 处理函数 | 说明 |
|--------|----------|------|
| `provider:` | `handleProviderCallback` | TTS 提供方选择 |
| `volume:` | `handleVolumeCallback` | 音量选择 |
| `speedup:` | `handleSpeedupCallback` | 加速开关 |
| `ono:` | `handleOnomatopoeiaCallback` | 拟声词过滤开关（仅 /rj） |
| `dl:` | `handleDownloadCallback` | 下载已完成 job |
| `confirm` | `handleConfirm` | 确认创建 job |
| `cancel_job` | (inline) | 取消当前操作 |
| `rj:t:<idx>` | `handleRJToggle` | 切换音频选中状态 |
| `rj:e:<idx>` | `handleRJEnter` | 进入文件夹 |
| `rj:back` | `handleRJBack` | 返回上级文件夹 |
| `rj:done` | `handleRJDoneAudio` | 完成音频选择 |
| `rj:all` | `handleRJSelectAll` | 全选/取消全选当前目录音频 |

## 用户身份映射

`user_mapper.go` 中的 `findOrCreateUser()`:

1. 检查 `TelegramBindingRepository` 是否有绑定 → 返回绑定的用户
2. 无绑定 → 查找 `tg_<telegramID>` 用户名
3. 不存在 → 自动注册（随机密码，10 年有效期）

## 配额管理

- **上传配额**：每次上传 Telegram 文件后 `IncrementUploadBytes`
- **下载配额**：每次下载 job 结果后 `IncrementDownloadBytes`
- 配额上限通过 `BotConfig.UploadLimit` / `DownloadLimit` 配置
