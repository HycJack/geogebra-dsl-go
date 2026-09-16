# ggbcheck — GeoGebra 指令校验器

校验 AI 生成的 GeoGebra 指令**是否正确、能否被执行**。单二进制的命令行工具，纯标准库（Go ≥ 1.21）。

> 名字：**ggbcheck** = **G**eo**G**ebra（ggb）+ **check**，即「GeoGebra 指令校验器」。它只做"能不能建立"的校验，不做数值求坐标、不产 `.ggb` 文件、不内置绘图。

输入两种形态，走同一套判定链，**支持命令嵌套**：

| 形态 | 示例 |
|---|---|
| 文本脚本 | `A = Point(0, 2)` / `c = Circle(Midpoint(A, B), 2)` |
| IR JSON | `{ "objects": [{"id":"A","cmd":"Point","args":["0","2"]}], "goals": ["l"] }` |

> 你的目的就是这一个：**人多眼杂地挑出 AI 输出里的错，一条命令看全部**。所以它只做"能不能建立"，不做数值求坐标、不产 `.ggb` 文件、不内置绘图——绘图交给 GeoGebra 自己。

## 构建与运行

```bash
go build -o ggbcheck.exe ./cmd/ggbcheck

ggbcheck check path/to/script.txt        # 人类可读收据
ggbcheck check --json path/to/file       # 结构化收据（管道/脚本用）
ggbcheck check --input text|ir <file>    # 强制输入形态（默认自动识别）
ggbcheck check /dev/stdin                # Unix 管道/重定向（不支持 `-` 简写）
```

退出码：`0` 全部通过 / `1` 构造不可建立 / `2` 用法或输入错误。

## 判定链（fail-closed：只在必要时停）

```
  文本 ──▶ 解析(括号/逗号/等号) ──▶ 构建对象图(重复定义/未定义引用/命令嵌套物化)
  IR  ──▶ 解析 JSON ────────────────┘
                                        │
                     同一个对象图 DAG
                                        │
      [语义] 命令存在 + 参数签名（含嵌套命令的合成对象）
      [数值化] 裸表达式后置判定为 KNumber / KFunction（数据驱动类型补齐后）
      [无环] Kahn 拓扑 + Tarjan SCC（有环 → dep/cycle，只列真环成员）
      [退化] 半径≤0 / 重合点直线 / 零半径圆（big.Rat + π/e 精确求值 + 科学计数法）
      [可达] goals（IR）或顶层对象（文本）都能建立
                                        │
                                      收据
```

**报告策略**：只要结构能解析，语义/无环/退化/可达会把**所有**独立错误一次全报出来（不是报第一个就停）——方便你对照 AI 输出逐条改。只有输入结构损坏（如一行无法解析、IR 缺 objects）才 fail-closed 直接停。

### 命令嵌套

参数里出现子命令会**物化为合成对象**参与全链校验，例如
`Circle(Midpoint(A, B), 2)` 会生成一个 `c.Midpoint1` 对象，自动校验
`Midpoint` 的签名（比如参数个数不够会报 `cmd/arg`）、建立依赖边、参与无环与退化判定，最终排进可执行顺序。嵌套可任意加深。

### 文本脚本的边界特性

- **科学计数法**：`1e3`、`2.5E-2`、`1E-16` 都被接受为数字字面量，与 GeoGebra 一致；`e - 3`（保留字 `e` 减 3）不会被误读成 `e-3` 的科学计数法，由 `number` 包的 `mantissaEndsWithE` 显式区分。
- **字符串感知**：`Text("a, b")` 里的逗号不算参数分隔符，`Point(0, "x, y")` 也不会被误切——`splitArgs` 与 `stripComment` 共用同一套引号感知逻辑。
- **注释**：`# ...` 与 `// ...` 行注释、`/* ... */` 跨行块注释（在切行前剥离）；UTF-8 BOM 自动剥离。
- **裸语句**：仅**官方 67 条 Scripting 命令**（`SetColor`、`StartAnimation`、`TurtleForward` 等）可写成 `Cmd(args)` 无赋值号；`Circle(A, B)` 这类必须报语法错误——这是"打错赋值"的典型形态，不应静默接受。
- **代数表达式后置数值化**：`r = d + 1` 在 build 阶段先按 `KFunction` 物化，`catalog.ApplyKinds` 把 `d = Distance(...)` 标成 `KNumber` 后，`text.ReclassifyNumericExprs` 再把 `r` 升级为 `KNumber`；函数定义 `f(x) = 2x+1` 保持 `KFunction`。

## 错误码

| 码 | 含义 |
|---|---|
| `parse/syntax` | 行无法解析（等号/括号/命令调用语法） |
| `parse/json` | IR JSON 结构损坏 |
| `dep/redefine` | 对象重复定义 |
| `dep/undefined` | 引用了未定义对象 |
| `dep/cycle` | 依赖成环，无法定序（**精确归因**：只列真环成员，下游被牵连对象单独报告） |
| `cmd/unknown` | 命令不在命令表里（拼写错误最常见；诊断附最近命令+官方 URL） |
| `cmd/arg` | 参数个数/类型不匹配任何签名（**诊断附该命令正确 overload 签名**，供 LLM 修复循环自纠） |
| `geo/degenerate` | 退化构造（零半径圆、重合点直线等） |
| `goal/unreachable` | goals 里有不存在的目标对象 |
| `usage` | 用法/命令表加载错误 |

## 命令表

命令表来自 GeoGebra 官方分类命令库 `geogebra-commands/`（取自
`ggb-gen-api/geogebra-commands`），内嵌全部 20 个分类 JSON 并合并成一份，
再补充 `supplement.json`（所有签名逐一对照 GeoGebra 内核源码
`org.geogebra.common.kernel.commands` 的 Cmd 处理器手动核验），当前 **549 条**。
命令 → 粗类型返回值与官方 67 条 Scripting 命令集合为数据驱动（`cmdmeta.json`），
命令表加载一次后进程级缓存（`catalog.Default()`），不随每次校验重复解析。

### 与内核源码对齐（覆盖核查）

拿 GeoGebra 内核源码里的权威命令枚举 `Commands.java`（548 个常量）逐一比对：

- **每一条 JSON 命令都能对到内核命令**，无错误/虚构条目（JSON 侧仅多收一个
  parser 函数名 `REAL`，无碍——它是保留字而非命令）。
- **内核 548 条命令已全部覆盖（缺失 0）**，含 3D 曲面体（`ConeInfinite`、
  `CylinderInfinite`、`Polyhedron`、`QuadricSide`）、统计（`PMCC`、
  `FitLineY`、`TableToChart`、`Q1/Q3`）、CAS（`Evaluate`、`TaylorSeries`）、
  变换（`Mirror`、`OrthogonalLine`、`Dilate`）等全部分类。
- 少数因内核已标 **deprecated**（`IntersectRegion`、`IntersectionPaths`）
  仍收录，便于兼容老脚本。

> 合并时每个分类文件的 `commands`（map 与 array 两种 shape 均支持）逐一并入；
> 同一条命令出现在多个分类时 overloads 全部累积，任何分类的签名都能命中。

### 保留值（reserved constants，源码核实）

GeoGebra 内核里被保留、不能用作变量名的常数（来自
`ParserFunctionsFactory.addReservedFunctions` + `Unicode.java`）：

| 写法 | 含义 | Unicode |
|---|---|---|
| `π` / `pi` | 圆周率 | `\u03C0` |
| `e` | 自然常数（欧拉数） | `\u212f` |
| `γ`（`ℯ_γ` / `EULER_GAMMA`） | 欧拉-马歇罗尼常数 | `\u212F_\u03B3` |
| `i` | 虚数单位 | `\u03af` |
| `freehand` | 保留词 | — |
| `deg` | 角度单位 | — |

其中 π 与 e 在脚本里直接写 `pi` / `e`（或希腊字母 `π`）即可当作数值用。

**退化判定已接入保留字与嵌套算式**：半径/坐标参数可用 `pi`、`e`（以及
`euler`/`gamma`）作为常数，并支持四则运算、乘方、括号的嵌套表达式——例如
`Circle(A, 2*pi)` 判定半径正值（通过），`Circle(A, 2*pi - 2*pi)` 判定半径
恰好为 0（退化拒绝）。这些常量按**高精度有理逼近**求符号，足够精确判定
正/零/负；`pi` 与 e 本为无理数，不做逐位精确相等比较。
（注：`pi` 是保留常量而非变量，脚本里写 `pi` 不会被当作"未定义对象引用"。）

## 目录结构

```
cmd/ggbcheck         CLI 入口（check 子命令 → exit code；支持 /dev/stdin）
internal/
  text/          文本脚本 → 对象图（解析+命令嵌套物化+绑定变量+字符串感知+科学计数法）
  ir/            共享对象图 + IR JSON 解析（两条输入的汇合点）
  number/        精确算术求值器（big.Rat + π/e + 嵌套算式 + 科学计数法）
  catalog/       命令表（go:embed 全部分类 JSON + supplement + cmdmeta.json，合并 549 条，进程级缓存）
  sig/           签名匹配（命令名/参数个数/粗类型，含嵌套命令；诊断附正确签名）
  deps/          依赖无环：邻接表 Kahn + Tarjan SCC 真环归因 + blocked 下游
  geo/           退化判定（复用 number 精确求值）
  reach/         goals 可达 & 可执行清单
  check/         编排：输入 → 全部阶段 → 收据
  diag/          错误码 + 收据结构
```

## 测试

```bash
go test ./...
go vet ./...
```

集成用例在 `testdata/`：`exam/`（**18 道**中考/高考风格正例，覆盖注释、四心、
圆的切线、椭圆焦点、函数拟合、3D 等）与 `exam-bad/`（**13 道**应被拒绝的反例），
由 `internal/check` 的端到端测试统一驱动。

## AI 对话服务（题目 → GeoGebra 指令）

`cmd/ai-server` 是一个可选的 HTTP 服务：接收题目图片或题目文本，经 OpenAI
兼容接口生成 GeoGebra 教学指令，并用本校验器做质量门控 + 自动重试修复。
完整设计见 [`DESIGN-AI.md`](DESIGN-AI.md)。

服务内置一个**单文件 Web 界面**（`cmd/ai-server/ui.html`，`//go:embed` 打进二进制）：
浏览器打开服务根路径即可用，支持题目文字/图片输入、右侧 GeoGebra 实时渲染、
逐轮追加修改，以及界面上的服务配置面板（endpoint / model / API Key，Key 只存本机
浏览器 localStorage 并在请求时随调用发送，服务端不保存、不写日志）。

```bash
# 启动（缺省会用内置的 sensenova 端点；需配 GGCM_AI_API_KEY）
GGCM_AI_ENDPOINT=https://token.sensenova.cn/v1/ \
GGCM_AI_MODEL=sensenova-6.8-flash-lite \
GGCM_AI_API_KEY=... \
  go run ./cmd/ai-server -addr :8080

# 打开界面
#   http://localhost:8080/          Web UI（输入→生成→GeoGebra 渲染→追加修改）
#
# REST 接口
#   GET  /api/health               liveness
#   GET  /api/config               运行期配置（endpoint/model/vision/max_image_bytes/has_api_key；不含 key 明文）
#   POST /api/chat                 { input_type:"text|image", text, image_b64?, image_mime?,
#                                    session_id?, append?, stream?, endpoint?, model?, api_key? }
#   GET  /deployggb.js             GeoGebra 加载器（本地同源，避免依赖 CDN）
#
# 日志
#   -log out.jsonl                 把每次大模型调用/脚本处理写成结构化 JSON Lines（缺省写 stdout）
```

核心配置均可用 `GGCM_AI_*` 环境变量覆盖（见 `internal/ai/config.go`）。
默认端点/模型为 sensenova `https://token.sensenova.cn/v1/` + `sensenova-6.8-flash-lite`；
API Key 必须由 `GGCM_AI_API_KEY` 环境变量或界面配置面板提供（不会写入代码/仓库）。

生成中遇到瞬态错误（`429`/`5xx`/断网）会自动**指数退避重试**：默认首次
`500ms`、每轮翻倍、最多重试 5 次，且每次重试复用同一批消息内容不丢失
（可用 `GGCM_AI_HTTP_RETRIES` / `GGCM_AI_HTTP_RETRY_BASE_MS` 调整）。

`/api/chat` 用 `context.WithTimeout` 把整个修复循环包在**请求预算**（默认
`GGCM_AI_REQUEST_TIMEOUT_S`=900s）内，防止一个坏请求在
`(MaxRepair+1)×HTTPTimeoutS` 的最坏路径上无限占用 handler；HTTP 层另设
`ReadHeaderTimeout`（30s）防 slowloris（不设 Read/WriteTimeout，保住 SSE 长连接）。

**安全约束**：Web 配置面板可逐次覆盖 `endpoint`/`model`/`api_key`，但服务端
**不会**把 `GGCM_AI_API_KEY` 附带发往请求方指定的第三方端点——自定义
`endpoint` 时请求方必须同时提供自己的 `api_key`，否则拒绝（400）。API Key
不落盘、不进日志（见 [`DESIGN-AI.md`](DESIGN-AI.md) §8）。会话数是仅内存结构，
硬上限 `maxSessions=1000`，淘汰**最早创建**的会话腾出空间；空 `session_id`
时服务端生成 `sess-<unixnano>-<hex8>`（4 字节随机后缀）避免被猜到。