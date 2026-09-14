# AI 对话流程设计 — 题目 → GeoGebra 动态几何指令

> 目标：用户把一道**数学/几何题目**（以**图片**或**纯文本**给出）发给一个对话式服务，服务借助 **OpenAI 兼容接口**的大模型，生成可用于**教学演示**的 **GeoGebra 动态几何指令**文本，并交给现有 `ggcm` 校验器做质量门控，必要时自动多轮修复，最终把"可直接执行的指令 + 可执行顺序 + 诊断"返回给用户。
>
> 复用：本仓库已有的 `internal/check.Check`（文本→对象图→语义/无环/退化/可达全链校验）作为**生成后校验**的唯一权威。

---

## 1. 目标与非目标

| 做 | 不做 |
|---|---|
| 接收题目图片或题目文本 | 不内置渲染 GeoGebra（前端/教师端自行粘贴进 GeoGebra） |
| 用多模态/文本 LLM 把题目解析成**教学意图**（已知/未知、目标对象） | 不做精确数值求解（沿用 ggcm 的"判可建立"哲学） |
| 生成 GeoGebra 指令脚本（本仓库 grammar，单行一条指令） | 不做细粒度 curriculum 编排（一次一道题的对话式生成） |
| 用 `ggcm` 校验生成结果，错误码反馈给 LLM 自动重试（有限轮） | 不接入真实 GeoGebra 内核执行 |
| 多轮对话：用户可追加"再构造一个高"、"改成半径/位置"等修正指令 | 不自动授课/不评判学生答案对错 |

> 教学演示侧重点：输出**稳定、可解释、可执行**的指令，而不是"最简"。生成器应倾向使用**命名对象**（`A = Point(...)` → `c = Circle(C, T)`），使其教学语义清楚，也让校验错误定位到对象名。

---

## 2. 总体流程（单轮生成 = 一个 mini-agent 循环）

```
 用户
  │  题目图片 或 题目文本
  ▼
┌─────────────────────────────────┐
│  HTTP 服务 (Go)                 │
│  ┌───────────┐  ┌─────────────┐ │
│  │ 会话管理   │  │ 提示词构建器 │ │
│  │ (对话历史) │  │ (sys+user)  │ │
│  └─────┬─────┘  └──────┬──────┘ │
│        ▼                ▼        │
│  ┌─────────────────────────────┐ │
│  │  OpenAI 兼容客户端 (LLM)     │ │
│  │  多模态图片 || 文本          │ │
│  └─────────────┬───────────────┘ │
│                ▼                 │
│        生成 指令脚本 draft        │
│                ▼                 │
│  ┌─────────────────────────────┐ │
│  │  ggcm 校验门 (internal/check)│ │
│  │  通过? ──是──▶ 直接返回      │ │
│  │   否                        │ │
│  │   └─▶ 错误码序列化 ──▶ 提示词  │ │
│  │       追加"上一版+诊断+请修"  │ │
│  │       (最多重试 N 次)         │ │
│  └─────────────┬───────────────┘ │
│                ▼                 │
│        最终收据(指令/可执行序/诊断)│
└─────────────────────────────────┘
  │
  ▼
 用户（粘贴进 GeoGebra 演示 / 继续对话）
```

要点：
- **一次"生成"不是单次 LLM 调用**，而是一个**有限重试的修正循环**：`generate → ggcm.Check → (fail? 构造修复提示 → regenerate) → success`。
- **对话**由独立的会话（session）承载：历史上下文跨轮保留，用户追加指令时把上一版脚本与校验结果一并喂回 LLM。
- LLM 每次只输出**纯指令脚本**（结构化区块），服务端负责切片：把脚本抽出来喂 ggcm，把 LLM 的文字说明单独透传。

---

## 3. 组件与模块划分（Go，纯 stdlib + HTTP；外部仅 LLM 后端）

```
cmd/ai-server/main.go        # HTTP 入口：路由 /api/chat、/api/health、/api/config、Web UI、-log
cmd/ai-server/ui.html        # 嵌入式单文件 Web 界面（/go:embed，GeoGebra 渲染 + AI 对话 + 配置面板）
cmd/ai-server/static/        # 本地 GeoGebra 加载器 deployggb.js（同源，避免依赖 CDN）
internal/ai/
  session.go                 # 会话：消息历史持久化(内存/可选文件)
  client.go                  # OpenAI 兼容 client（chat/completions，支持 vision）
  prompt.go                  # 提示词模板：system + user + 修复反馈(repair scaffold)
  generate.go                # 单轮生成：LLM 调用 + 脚本抽取
  gate.go                    # ggcm 校验门：调 internal/check，错误码→结构化反馈
  repair.go                  # 修正循环：gate 未过 → 组装修复提示 → 重生成
  respond.go                 # 收据 → 面向用户的响应(脚本/可执行序/诊断/streaming)
  config.go                  # 配置：endpoint/model/api-key/temperature/图片开关/重试上限
  log.go                     # 结构化日志钩子(LogFunc)：LLM/脚本/HTTP 事件，安全脱敏
internal/check/...           # 复用现有校验器(不改，只扩一个"可编程入口")
```

**接缝原则（延续 DESIGN.md §6）**：`ggcm` 的 `check.Check(input, Options)` 已经是可编程入口，`ai/gate.go` 只依赖它返回的 `*diag.Receipt`（`OK` / `Errors[]Code+Msg+Obj` / `Executable`）。`ai` 层不碰 `ir`/`catalog`/`geo` 内部。

---

## 4. OpenAI 兼容接口（client.go）

仅依赖 OpenAI 的 `POST /chat/completions`（OpenAI 本尊、OpenRouter、DeepSeek、通义千问等兼容实现）。用 `net/http` + `encoding/json`，不引第三方 SDK。

### 4.1 请求体

```jsonc
{
  "model": "<cfg.Model>",
  "messages": [
    { "role": "system",   "content": "<系统提示>" },
    { "role": "user",     "content": [
        { "type": "text", "text": "<题目文本或解析指引>" },
        { "type": "image_url",
          "image_url": { "url": "data:image/png;base64,...." } } ] },   // 仅图片输入
    // …… 历史对话轮 + 最近一次修复反馈(见 §6.3)
  ],
  "temperature": 0.2,               // 生成脚本要稳定，温度偏低
  "max_tokens": 128000,
  "stream": true                    // 服务端用 SSE 把脚本/说明流式返回
}
```

- **图片**：传入时按 `data:` URI（base64）内联到 `image_url`，要求后端支持多模态视觉。若 `cfg.DisableVision` 或第一次调用返回"不支持图像"错误，则回退：提示用户"请改用文本输入"（不强行本地 OCR，作为可选项留到 §8 扩展）。

### 4.2 响应解析

标准 `choices[0].message.content`；当 stream 时解析 `choices[0].delta.content` 增量。

**瞬态错误自动指数退避重试**：遇到网络错误或可重试状态码（`408`/`409`/`429`/`5xx`）时，在 `client.go` 内部对**同一份消息/请求体**做指数退避重发（初始退避 `GGCM_AI_HTTP_RETRY_BASE_MS`=500ms，每轮翻倍，最多重试 `GGCM_AI_HTTP_RETRIES`=5 次）。重试全程复用构建好的请求体，消息内容不被丢失或改写；`4xx` 以外的永久错误（`400`/`401`/`403`/`404`）不重试，直接返回用户。

### 4.3 配置 env

```
GGCM_AI_ENDPOINT    # 默认 https://token.sensenova.cn/v1/
GGCM_AI_MODEL       # 如 sensenova-6.8-flash-lite / deepseek-chat
GGCM_AI_API_KEY     # 缺省时：如果 endpoint 是本地兼容服务可留空
GGCM_AI_TEMP        # 默认 0.2
GGCM_AI_MAX_REPAIR  # 默认 3（修复重试上限）
GGCM_AI_MAX_TOKENS  # 默认 128000（推理 token + 输出共用；sensenova 推理模型建议给足）
GGCM_AI_DISABLE_VISION  # 默认 false
GGCM_AI_HTTP_RETRIES     # 默认 5（瞬态错误最多重试次数；0 则不发重试）
GGCM_AI_HTTP_RETRY_BASE_MS  # 默认 500（首次退避毫秒，每轮翻倍）
GGCM_AI_HTTP_TIMEOUT_S     # 默认 300（单次 LLM 调用超时秒数；max_tokens 大时需同步调大）
```

### 4.4 结构化日志（调试/审计）

`ai-server` 提供 `-log <file>` 参数把每次调用记录为 **JSON Lines**（缺省写 stdout）。事件类型（`event` 字段）：

- `http.request` / `http.result`：HTTP 层，含 session_id、input_type、文本/图片长度、最终脚本预览与诊断。
- `llm.request` / `llm.response`：单次大模型调用，含 model、endpoint、temperature、max_tokens、消息摘要（role + 字符数 + 图片数，**不含图片 base64 与 API Key**）、返回文本预览（上限 500 字符）、延迟。
- `llm.retry`：瞬态失败（429/5xx/断网）即将进行指数退避重发，含退避毫秒、重试序号、错误信息；重试用同一份请求体，消息不丢失。
- `generate.attempt.start` / `.error` / `.done`：脚本处理循环，每次尝试的脚本、gate 结果（`gate_ok`）、可执行对象序、诊断与教学说明。

日志不会写入 API Key，也不会把图片明文 payload 落盘；图片仅记录张数与大小。

---

## 5. 提示词构建（prompt.go）

### 5.1 系统提示（稳定，固定）— 核心是把"教学意图→指令"讲清楚

```
你是 GeoGebra 教学构造助手。你会收到一道数学/几何题目（图片或文字）。

任务：把它解析成一份**可直接执行的 GeoGebra 指令脚本**，用于教学演示。

硬性规则：
1. 每条指令一行，形如  `对象名 = 命令(参数)`。命令名用英文（Point/Line/Circle/…），与 GeoGebra 一致。
2. 先构造**显式命名**的对象作为已知量，再构造需要的未知对象；对象名尽量教学的（A、B、C、O、l、c、P 等）。
3. 只用下面这些基本命令构造，除非题目必要：Point、Segment、Line、Ray、Circle、Midpoint、Polygon、Intersect、PerpendicularLine、ParallelLine、Angle、Distance、Length、Area、Sequence。
4. 依赖关系必须无环：不要用还没定义的对象去定义另一个对象。
5. 不要输出任何不在脚本里的解释文字；脚本外可单独给一小段"教学说明"（用 `<!-- 说明： -->` 标注）。
6. 若有退化（重合点直线、零半径圆）避免；若题目不同构，直接说明无法构造。

输出格式：只输出脚本。脚本可以这样包裹，便于我解析：
```
<gg>
A = Point(0, 2)
B = Point(4, 2)
c = Circle(C, T)
</gg>
```
```

### 5.2 用户消息

- **文本题目**：原文照传。
- **图片题目**：用多模态图片消息；文本部分写"请根据这张图片中的几何题目，生成教学用 GeoGebra 指令脚本"。

### 5.3 修复反馈（repair scaffold，非首轮才追加）

当 ggcm 校验失败，把下列结构化反馈作为一条新的 `user`（或 `system`-adjacent）消息喂回：

```
你上一版脚本校验失败，诊断如下（逐条）：
- [dep/undefined] 引用了未定义对象：X     位于对象 l
- [cmd/unknown] 命令 Foobar 不在命令表里
请按诊断修正后重发完整脚本（不要只输出补丁）。
```

> 设计选择：**每轮重发完整脚本**而非 diff。LLM 上下文短、指令只有几行到几十行，整发最稳，且避免"只发补丁导致历史错位"。

---

## 6. 校验门与修复循环（gate.go、repair.go）

### 6.1 gate.go — 调用现有校验器

```go
// Generate 产出一份脚本后：
rc := check.Check([]byte(script), check.Options{})
if rc.OK {
    return Result{Script: script, Executable: rc.Executable, OK: true}
}
repairHint := serializeDiagnostics(rc) // 见 6.3
```

### 6.2 repair.go — 有限重试

```
for attempt := 1; attempt <= cfg.MaxRepair; attempt++ {
    script := generateOnce(session, prompt)   // 首次用正题提示，之后用修复提示
    if gate passes → return
    // 否则把诊断并进提示，continue
}
// 超过重试上限：返回"加人工"失败收据：bestScript + 未消诊断
```

- 每次重试把**上一版脚本 + 全部诊断**塞进上下文（会话历史保留给 LLM）。
- 诊断按 ggcm 的 `Errors[]` 原样透传（`Code`/`Msg`/`Obj`），LLM 据此针对性修。

### 6.3 serializeDiagnostics —— 错误码 → 修复建议（译文表，指导 LLM 也指导用户）

| ggcm Code | 含义 | 建议 |
|---|---|---|
| `cmd/unknown` | 命令不在表里 | 改用表内命令（Point/Line/Circle/…）或拼对命令名 |
| `cmd/arg` | 参数个数/类型不匹配 | 对照该命令 overload 的正确签名 |
| `dep/undefined` | 引用了未定义对象 | 先定义被引用对象，或取消该引用 |
| `dep/cycle` | 依赖成环 | 调整定义顺序，避免互相定义 |
| `dep/redefine` | 重复定义 | 保留一个定义，改名其他 |
| `geo/degenerate` | 退化（重合点直线/零半径圆） | 改坐标或半径，避免退化 |
| `goal/unreachable` | 目标对象不存在 | 确保所有被引用/目标 id 已定义 |
| `parse/syntax`、`parse/json`、`usage` | 结构/用法错误 | 通常是脚本整体格式问题，重写整版 |

---

## 7. 对话多轮（session.go）与 API

### 7.1 会话

- `POST /api/chat` 每次请求带 `session_id`（无则新建）。服务端按 session 保存消息历史（内存 map；可选落盘 `data/sessions/<id>.json`）。
- **每轮必存「用户消息 + 助手最终脚本」成对**：成功时 assistant 存最终 ggb 脚本，失败时也存（空脚本则存空 `<gg></gg>` 占位），保证历史里每轮都是 `user → assistant` 完整配对，让下一轮 LLM 看到"上一版做了什么、成功还是失败"。
- 属于同一轮、尚未完成的消息不会预追加到历史（当前 `userMsg` 通过 `GenerateRequest.UserMsg` 传给本次生成），避免在同一轮回车里重复。

### 7.1b 嵌入式 Web UI 与配置面板

- 服务根路径 `/` 返回 `ui.html`（`//go:embed`）：左侧题目输入 + 对话，右侧 GeoGebra 画布渲染，支持「追加修改」多轮。
- `GET /api/config` 返回服务端运行期配置（endpoint/model/vision_enabled/max_image_bytes/`has_api_key` 布尔）供配置面板预填；**绝不返回 API Key 明文**。
- 界面配置面板可逐请求覆盖 endpoint/model/API Key：Key 只存本机浏览器 `localStorage`，随 `POST /api/chat` 的 `api_key` 字段发送，服务端用后即弃、不落盘、不进日志。
- 参考 `/api/chat` 请求体：`endpoint?`、`model?`、`api_key?` 为可选逐次覆盖；留空则用服务端默认/环境变量。

### 7.2 请求体

```jsonc
{
  "session_id": "opt" | "",
  "input_type": "text" | "image",     // 图片时必填
  "text": "已知三角形 ABC 与点 O，求作过 O 且垂直于 AB 的直线",
  "image_b64": "....",                // 仅 image 时
  "image_mime": "image/png",
  "append": "改成让它动起来：把点 A 做成滑块 0..10",   // 可选：对上一轮结果追加修正
  "endpoint": "opt",                  // 可选逐次覆盖 base_url（留空用服务端默认）
  "model": "opt",                     // 可选逐次覆盖 model
  "api_key": "opt"                    // 可选逐次覆盖 API Key（浏览器传，服务端用后即弃）
}
```

### 7.3 响应（stream：SSE 增量；非 stream：JSON）

```jsonc
{
  "ok": true,
  "script": "A = Point(...)\n…",
  "executable": ["A","B","C","l"],
  "teaching_note": "第一段为已知量…",
  "diagnostics": [],                // 空 = 通过 ggcm
  "attempts": 2                     // 重试次数（含首次）
}
```

失败（重试耗尽）返回 `ok:false` + `script`（最后可用版）+ `diagnostics`（未消诊断）+ `note: "建议人工检查"`。

---

## 8. 边界与错误处理

- **图片后端不支持**：调用报错 → 降级提示"改用文本输入"；`DisableVision=true` 时可提前跳过图片 branch。
- **LLM 弱：连续重试仍不过**：达到 `MaxRepair` 后不再空转，返回带诊断的失败收据（不吞错误）。
- **瞬时网络/限流**：429/5xx/超时 → 指数退避重试 ≤2 次。
- **脚本抽取失败**：LLM 输出里没有 `<gg>` 区块 → 视为一次失败重试；仍无则报 `format` 错误。
- **会话放大**：设 `MaxHistoryTurns`（默认 20）截断，防止上下文膨胀。

---

## 9. 测试策略（新增 internal/ai 单测）

- **gate_test**：用真实 ggcm 校验一段正确脚本 → OK；一段含 `dep/undefined` 的 → 正确 `serializeDiagnostics` 译文。
- **repair_test**：用**假 LLM 注入**（实现了 `client` 接口的 stub）：第一次输出坏脚本、第二次输出好脚本 → 断言恰好在重试 2 次内通过，且调用序列里第 2 条带诊断反馈。
- **prompt_test**：断言 system 含规则、修复反馈正确拼入。
- **多轮会话测试**：append 追加后历史长度/内容正确。
- **stream_test**：SSE 增量聚合得到完整脚本。

`internal/ai/client` 定义成接口，使测试不触网：

```go
type ChatClient interface {
    Complete(ctx, []Message, Opts) (string, error)   // 掩盖 stream 与多模态
}
```

---

## 10. 与 ggcm 的关系（明确边界）

- `ggcm` 校验器**不改**；`ai/gate.go` 是它在新场景的唯一新调用方。
- 生成器只输出 ggcm grammar **能解析**的指令（单行一条、`ID = Command(args)`、字面点/数字）。这与 DESIGN.md 的文本输入 grammar 完全一致。
- 若未来某命令确实需要但不在现有 549 条表内，属于 `catalog` 层扩展，不在本设计范围。

---

## 11. 扩展预留（不实现，仅记录）

1. **本地 OCR 降级**：图片后端不支持时，可选接入 OCR（如 tesseract）先抽文本。
2. **"动态化"增强**：给点加 `Value`/滑块构造提示，让演示可拖动（append 指令支持）。
3. **多轮授课序列**：一次题多步骤生成，逐步揭示。
4. **持久化会话到磁盘/DB**：当前内存 + 可选 JSON 落盘。
5. **前端可视化预览**：服务端接 WebSocket 把脚本发给内嵌 GeoGebra 实例实时预览。