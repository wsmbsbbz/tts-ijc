# Roadmap

## Phase 1 — 文件名体验优化 ✅

**目标**：前端上传、任务列表、下载按钮均使用原始文件名，避免显示 UUID 路径。

### 背景

目前上传文件后，R2 object key 格式为 `uploads/{uuid}/{filename}`，但任务创建和展示时均未保留原始文件名，导致任务列表和下载文件名对用户无意义。

### 涉及改动

**后端**

- `domain/job.go`：`Job` 结构体增加 `AudioName`、`VTTName` 字段（原始文件名）
- `infrastructure/persistence`：SQLite / Postgres 表增加 `audio_name`、`vtt_name` 列
- `interfaces/http/dto.go`：`CreateJobRequest` 增加 `audio_name`、`vtt_name` 字段
- `interfaces/http/job_handler.go`：`JobResponse` 增加文件名字段并透传

**前端**

- `app.js`：`uploadFile()` 返回原始文件名，提交任务时带上 `audio_name`、`vtt_name`
- 任务列表展示原始音频文件名替代 job ID
- 下载时用原始文件名作为 `download` 属性（`<a download="xxx.mp3">`）

---

## Phase 2 — 用户名 / 密码注册登录 ✅

**目标**：引入用户体系，支持用户名 + 密码注册登录，账号有有效期管控，为后续限流和个人任务历史奠定基础。

### 设计决策

- 认证方式：**Opaque Session Token**（有状态，存库）。相比 JWT 更易在账号到期时立即失效。
- 账号有效期：可配置（默认 24 小时），到期后自动停用并踢出会话，但数据保留。
- 最大有效账号数：可配置（默认 100），注册时实时检查。

### 涉及改动

**后端**

- `domain/user.go`：`User` 实体（id、username、password_hash、created_at、expires_at、is_active）；`Session` 实体（token、user_id、expires_at）
- `domain/user_repository.go`：`UserRepository` + `SessionRepository` 接口
- `infrastructure/persistence`：`users`、`sessions` 表（Postgres + SQLite 双实现）
- `application/auth_service.go`：注册（bcrypt hash）、登录、注销、session 校验、账号定期过期
- `interfaces/http/auth_handler.go`：`POST /api/auth/register`、`POST /api/auth/login`、`POST /api/auth/logout`
- `interfaces/http/middleware.go`：`SessionAuth` 中间件，挂载到 `/api/jobs`、`/api/upload-url`；支持 `?token=` 参数用于下载链接
- `domain/job.go`：`Job` 增加 `UserID` 字段；`ListRecent` 按 user 过滤
- `infrastructure/config/config.go`：增加 `MAX_ACTIVE_ACCOUNTS`、`ACCOUNT_TTL_HOURS`、`SESSION_TTL_HOURS`

**前端**

- 新增登录 / 注册卡片（Tab 切换）
- 登录后 token 存 `localStorage`，请求时带 `Authorization: Bearer <token>`
- 未登录时显示 Auth 页，隐藏主界面；401 时自动退出
- 下载链接附带 `?token=` 参数

**新增环境变量**

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `MAX_ACTIVE_ACCOUNTS` | 同时有效账号上限 | `100` |
| `ACCOUNT_TTL_HOURS` | 账号有效期（小时） | `24` |
| `SESSION_TTL_HOURS` | Session 有效期（小时） | `24` |

---

## Phase 3 — 限流

**目标**：控制早期试用阶段的资源消耗，防止滥用。限流规则存储于数据库，支持运行时调整。

### 限流维度

| 维度 | 默认上限 | 配置方式 |
|------|---------|---------|
| 每日全局新注册用户数 | 50 | 环境变量 `DAILY_REGISTER_LIMIT` |
| 每用户每日任务数 | 5 | 环境变量 `USER_DAILY_JOB_LIMIT` |
| 每用户每日上传总流量 | 500 MB | 环境变量 `USER_DAILY_BYTES_LIMIT` |

### 涉及改动

**后端**

- `domain/user.go`：增加 `daily_job_count`、`daily_bytes_used`、`quota_reset_at` 字段
- `infrastructure/persistence`：`users` 表增加对应列；`daily_registrations` 表记录每日注册数
- `application/auth_service.go`：注册前检查全局日注册配额
- `application/job_service.go`：创建任务前检查用户日任务配额
- `application/upload_service.go`：生成 presigned URL 前检查用户日流量配额（依赖前端上报文件大小）；任务完成后累加实际用量
- 每日零点（UTC）重置用户配额（cleanup loop 中追加）

**新增环境变量**

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DAILY_REGISTER_LIMIT` | 每日全局注册上限 | `50` |
| `USER_DAILY_JOB_LIMIT` | 每用户每日任务上限 | `5` |
| `USER_DAILY_BYTES_LIMIT` | 每用户每日流量上限（字节） | `524288000`（500 MB）|
