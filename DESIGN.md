# GeoGebra 指令校验器 (Go) — 设计文档

> 唯一目的：**校验 AI 生成的 GeoGebra 指令正确、可以执行。**
> 输入：两种形态，走同一条判定链——① 文本/多行指令，② IR JSON（`objects[]`+`goals[]`，备选输入）。
> 判定：构造可建立（命令存在且签名对、依赖无环、对象无退化、目标可达）。交付：只要收据/报告，不产文件、不算坐标。
> 命令表：内嵌 GeoGebra 官方分类命令库（20 个 JSON）+ 源码核验的 supplement，**549 条**（与内核 `Commands.java` 548 个命令常量全对齐）。

## 1. 目标与非目标

| 做 | 不做 |
|---|---|
| 解析文本指令（多行、指令/赋值/字面量、**命令嵌套**） | 计算坐标/几何数值（只判可建立，不数值求解） |
| 承接 IR JSON 输入（`objects[]`+`goals[]`，沿用现有 shape） | 生成 `.ggb` 脚本（不交付，只报告） |
| 校验命令存在性 + 签名（参数个数/粗类型/嵌套命令） | 内置绘图/可视化 |
| 校验构造可建立：依赖无环、无退化、目标可达 | 公开库 API（v1 单 CLI 内部实现） |
| 输出结构化收据：ok + 错误码 + 中文诊断 | 多语言/富 UI |
| 输出拓扑序可执行对象清单（证明"可执行"） | |

## 2. 一条收集式判定链（fail-closed 只在必要时）

两条输入都先建成**同一份对象图**（object DAG：`id → (cmd, kind, args, refs)`），此后的判定完全共享。

```
输入 A：文本 / 多行指令                输入 B：IR JSON
   │                                        │
[1a] text 包：按行+括号/逗号/等号切分      [1b] ir.ParseJSON
     命令嵌套 → 合成对象                         → objects[](id, kind|cmd, args, refs)
     行无法解析 → fail-closed 停                        → goals[]
   │                                        │
   └──────────────┬─────────────────────────┘
                  ▼
            同一份 ir.Graph DAG
                  │
                  ▼
[2] 语义   SetKindFromCmd 定种类 → 命令存在+签名(含嵌套合成对象)
[3] 无环   依赖拓扑序，有环 → dep/cycle
[4] 退化   big.Rat 精确求值（含 π/e 与嵌套算式）→ geo/degenerate
[5] 可达   goals[]（IR）或顶层对象（文本）都能建立
                  │
                  ▼
             收据(ok/errors/可执行清单/退出码)
```

**报告策略**：只要结构能解析，[2]–[5] 会把**全部**独立错误一次报出来（不是报第一个就停）——方便你对照 AI 输出逐条改。只有输入结构损坏（某行无法解析、IR 缺 `objects`、JSON 不合法）才真 fail-closed 直接停。

> 注：与"严格 fail-closed"的典型流水线不同，这里下游错误与上游错误多为**同一层对象的独立缺陷**，全量收集比"首个错误即停"对用户更有价值。

## 3. 输入形态

- **文本**：`ID = Command(args)`、`ID = (x, y)`（字面点）、`ID = 3.5`（数字）、`# 注释` / 行尾 `# 注释`、UTF-8 BOM 自动剥离。
- **命令嵌套**：参数里出现 `SubCmd(...)` 会被**物化为合成对象**（如 `c.Midpoint1`），带有自己的 cmd/args/refs，参与签名/依赖/退化全链校验。外层对象把合成对象当依赖。
- **IR JSON**：`{ "objects":[{id, kind|cmd, args, refs}], "goals":[] }`，命令名走同一套目录签名。
- **形态自动识别**：`{` 开头且能解析成 JSON → IR；否则文本；`--input json|text` 可强制。
- **绑定变量**：`Sequence/Sum/Product/Curve/Surface` 等的迭代参数（如 `k`）是命令绑定符号，不作引用、不报未定义。
- **保留常量**：`pi`/`e`/`euler`/`gamma` 是常量，不作对象引用（不会误报 `dep/undefined`）。

## 4. 命令表（549 条，与内核对齐）

- 主源：20 个官方分类 JSON（map 与 array 两种 `commands` 结构都支持），`go:embed` 进二进制。
- 补充：`supplement.json`，所有签名逐一对照内核 `Cmd*` 处理器核实。
- **覆盖核查**：对内核权威枚举 `Commands.java`（548 常量）逐一比对——**缺失 0**；JSON 仅多收 parser 函数 `REAL`（保留字，非命令）。
- 匹配粒度：命令名精确 + 参数个数 + 粗类型（Point/Line/Circle/…），按 overload 匹配；字面量参数宽松通过（不阻塞有效性判定）。

## 5. 精度与求值

- **退化判定**走精确求值：`math/big.Rat` + 保留常量 + **嵌套算术**（`+ - * / ^ 括号`）。用于判断半径/坐标的正、零、负，决定是否退化。
- π/e 为无理数，按**高精度有理逼近**求符号（足够精确判正/零/负；不做逐位相等）。
- 幂运算右结合、且 `^` 比一元负号更紧（`-2^2 == -(2^2)`，与 GeoGebra/数学一致）；`^` 仅接受非负小整数指数，且指数有上限（`maxExponent`），防在不可信 AI 输出里出现超大指数时失控循环。
- 不全局求坐标 → 不引入 float 交点误差问题，这也是"只判可建立"比数值校验轻的原因。

## 6. 包布局（Go，纯 stdlib）

```
cmd/ggbcheck/main.go      # 单二进制：check 子命令 + --json / --input + 退出码
internal/
  text/               # 文本 → 对象图：lexer+parser+build+命令嵌套物化+绑定变量
  ir/                 # 唯一共享对象图 ir.Graph + 种类 + IR JSON 解析（两条输入的接缝）
  number/             # 精确算术求值器（big.Rat + π/e + 嵌套算式）
  catalog/            # 命令表：go:embed 20 分类 + supplement 合并，归一 549 条
  sig/                # 签名匹配：命令名/参数个数/粗类型（overload）
  deps/               # 依赖无环 (topo sort) + 可执行顺序
  geo/                # 退化判定（复用 number 精确求值）
  reach/              # 目标可达 & 顶层对象
  check/              # 编排：两条输入 → 全部阶段 → 收据
  diag/               # 错误码、中文诊断、收据结构
```

> **接缝原则**：`ir.Graph` 是唯一共享类型。`text`/`ir` 两条输入都产它；`sig`/`deps`/`geo`/`reach` 只吃它。改动一端不碰另一端，只碰 `ir`。

## 7. CLI

```
ggbcheck check <file>                 # 主命令：整条链 + 收据
ggbcheck check --json <file>          # 结构化收据（默认人类可读中文）
ggbcheck check --input text|ir <file> # 强制输入形态（默认自动识别）
```
退出码：`0` 全部通过 / `1` 构造不可建立（有错误码）/ `2` 用法或输入错误。

收据：`ok` / `errors[]`（码+中文+对象+行号）/ `warnings[]` / 可执行对象清单(拓扑序)。

## 8. 错误码

| 码 | 含义 |
|---|---|
| `parse/syntax` | 行无法解析（等号/括号/命令调用） |
| `parse/json` | IR JSON 结构损坏 |
| `dep/redefine` | 对象重复定义 |
| `dep/undefined` | 引用了未定义对象 |
| `dep/cycle` | 依赖成环，无法定序 |
| `cmd/unknown` | 命令不在命令表里（拼写错最常见；含嵌套命令） |
| `cmd/arg` | 参数个数/类型不匹配任何签名（含嵌套命令） |
| `geo/degenerate` | 退化构造（零半径圆、重合点直线等） |
| `goal/unreachable` | goals 里有不存在的目标对象 |
| `usage` | 用法/命令表加载错误 |

## 9. 测试策略

- 纯标准库，无外部依赖，`go test ./...` 全绿。
- 各包单元测试：`text`（解析/嵌套/绑定变量/保留常量）、`number`（求值/零判断）、`geo`（退化/π/e）、`sig`（overload）、`catalog`（map+array 合并/缺漏）、`check`（端到端多场景）。
- 集成夹具在 `testdata/`：正例文本+IR、undefined、degenerate、cycle、unknown-cmd、ai-bad、mirror-arc、sequence-ai、nested、pi-script。

## 10. 待定（v1 未定，不挡当前功能）

1. 模块路径 `github.com/hycjack/geogebra-dsl-go` 为占位，发布前可换真实地址。
2. 文本暂**一行一条指令**（不支持 `;` 分号多指令同行的拆分、语句分隔）。
3. `--input` 与外部命令表覆盖（env/flag）已预留，但外部加载路径未接完整 UI。
4. 命令嵌套合成对象的命名用 `owner.SubCmdN`——若涉及与 GeoGebra 实际自动命名(如 `A_1`)对齐，需后续确认。