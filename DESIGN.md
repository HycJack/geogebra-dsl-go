# ggbcheck — GeoGebra 指令校验器设计文档

> **唯一目的**：校验 AI 生成的 GeoGebra 指令**正确、可建立**。
> **输入**：文本脚本 / IR JSON 两种形态，走同一份对象图，共享同一条判定链。
> **判定**：构造可建立（命令存在且签名对、依赖无环、对象无退化、目标可达）。
> **交付**：只要收据/报告——不产 `.ggb` 文件、不数值求解、不内置绘图。
> **命令表**：内嵌 GeoGebra 官方分类命令库（20 个 JSON）+ 源码核验的 `supplement.json`，**549 条**（与内核 `Commands.java` 548 个命令常量全对齐，仅多一个保留字 `REAL`）。
> **数据驱动**：命令→返回粗类型、Scripting 命令集合都放在 `cmdmeta.json`——加/改一个命令的类型只需改这一份 JSON，不碰 Go。

---

## 1. 目标与非目标

| 做 | 不做 |
|---|---|
| 解析文本指令（多行、赋值/字面量/命令嵌套/`#` 与 `//` 行注释 + `/* */` 块注释） | 数值求解（只求值判退化，不算坐标） |
| 承接 IR JSON 输入（`objects[]`+`goals[]`） | 生成 `.ggb` 文件 |
| 校验命令存在性 + 签名（参数个数/粗类型/嵌套命令） | 内置绘图/可视化 |
| 校验构造可建立：依赖无环、无退化、目标可达 | 公开库 API（v1 单 CLI 内部实现） |
| 输出结构化收据：ok + 错误码 + 中文诊断 | 多语言/富 UI |
| 输出拓扑序可执行对象清单（证明"可执行"） | 教学/评判学生答案对错 |

---

## 2. 一条收集式判定链（fail-closed 只在必要时）

两条输入都先建成**同一份对象图** `ir.Graph`（`id → (cmd, kind, args, refs, params)`），此后的判定完全共享：

```
  输入 A：文本 / 多行指令              输入 B：IR JSON
     │                                     │
  [1a] text 包：按行+括号/逗号/等号切分    [1b] ir.ParseJSON
        命令嵌套 → 合成对象                    → objects[](id, cmd/kind, args, refs)
        命令/字面量/裸语句分流                   → goals[]
        行无法解析 → fail-closed 停
     │                                     │
     └──────────────┬──────────────────────┘
                    ▼
              同一份 ir.Graph DAG
                    │
                    ▼
   [2] 语义    catalog.ApplyKinds（cmdmeta.json 数据驱动）
   [2'] 数值化   text.ReclassifyNumericExprs（表达式 → KNumber）
   [3] 语义    sig 签名匹配（含嵌套合成对象）
   [4] 无环    Kahn 拓扑 + Tarjan SCC 真环归因 + blocked 下游
   [5] 退化    big.Rat 精确求值（含 π/e 与嵌套算式、科学计数法）
   [6] 可达    goals[]（IR）或顶层对象（文本）都能建立
                    │
                    ▼
              收据(ok/errors/可执行清单/退出码)
```

**报告策略**：只要结构能解析，[3]–[6] 会把**全部**独立错误一次报出来（不是报第一个就停）——方便你对照 AI 输出逐条改。只有输入结构损坏（某行无法解析、IR 缺 `objects`、JSON 不合法）才真 fail-closed 直接停。

> 与"严格 fail-closed"的典型流水线不同：这里下游错误与上游错误多为**同一层对象的独立缺陷**，全量收集比"首个错误即停"对用户更有价值。

**环的精确归因**：`deps.Order` 用 Tarjan 强连通分量把遗留节点分成两类——**真环成员**（每个 SCC 单独报告）与**被牵连的下游**（`DepCycle` 的第二条诊断，消息前缀"以下对象依赖成环节点"）。这样 AI 修复循环指向的是**真环本身**，而不是被误报的无辜下游。

---

## 3. 输入形态

### 3.1 文本脚本

- **赋值**：`ID = Command(args)`、`ID = (x, y)`（字面点）、`ID = 3.5` / `ID = 1e3` / `ID = 2.5E-2`（数字，支持科学计数法）、`ID = {1, 2, 3}`（列表字面量）。
- **代数表达式 / 函数定义**：`y = x^2 + 1`（无参数 → 数值化判定）、`f(x) = 2x + 1`（有 `Params` → 保持 KFunction）。
- **裸语句**：`SetColor(c, "red")`、`StartAnimation(a)`——仅**官方 Scripting 命令**（67 条，`cmdmeta.json.scripting`）合法；`Circle(A, B)` 这类无赋值号写法**必须**是语法错误，因为它是"打错赋值"的典型形态。
- **注释**：`# ...`、`// ...` 行尾注释（行内或独占行）、`/* ... */` 跨行块注释（**在切行之前**剥离，避免跨行的注释被切成多条语句）。UTF-8 BOM 自动剥离。
- **命令嵌套**：参数里出现 `SubCmd(...)` 会**物化为合成对象**（如 `c.Midpoint1`），带自己的 cmd/args/refs，参与签名/依赖/退化全链校验；外层对象把它当依赖。嵌套可任意加深。
- **绑定变量**：`Sequence/Sum/Product/Curve/Surface` 等的迭代参数（如 `k`）是命令绑定符号，不作对象引用、不报未定义。
- **保留常量**：`pi` / `e` / `euler` / `gamma` / `i` 是保留字常量，不作对象引用（不会误报 `dep/undefined`）；`deg` / `freehand` 等同样是保留字。
- **字符串感知**：`splitArgs` 与 `stripComment` 都用同一套字符串引号感知——`Text("a, b")` 里的逗号不算分隔符，`Point(0, "x, y")` 也不会被误切。
- **数字字面量**：`isNumber` 接受整数、小数、`+/-` 前缀、科学计数法（`1e3`、`2.5E-2`），与 GeoGebra 一致；至少一位数字（`.` 或 `+` 单独不算数字）。

### 3.2 IR JSON

- 结构：`{ "objects":[{id, kind?, cmd?, args?, refs?}], "goals":[id, ...] }`。
- 命令名走同一套目录签名；IR 侧**必须**提供 `kind`（或 `cmd`），因为 IR 是权威输入，不承担"从命令名猜类型"的责任。
- 若 `objects` 缺失、JSON 不合法、`goals` 类型错，直接 fail-closed。

### 3.3 形态自动识别

- 以 `{` 开头且能解析成 JSON → IR；否则文本。
- `--input json|text` 可强制。

---

## 4. 命令表（549 条，与内核对齐）

- **主源**：20 个官方分类 JSON（`geogebra-commands/*.json`，`commands` 的 map 与 array 两种 shape 都支持），`go:embed` 进二进制。
- **补充**：`supplement.json`（238 条签名，逐一对照内核 `Cmd*` 处理器核实）。
- **元数据**：`cmdmeta.json` 两份数据：
  - `returns`：命令 → 返回粗类型（`Point` / `Line` / `Number` / `Polygon` / `Quadric` / `Plane` / `Polyhedron` / `List` / `Text` / `Matrix` / `Curve` / `Conic` / `Boolean` / `Script` …），数据驱动替代了原来 200+ 行的 `kindForCmd` switch。
  - `scripting`：官方 67 条 Scripting 命令集合（返回 "Script"），也是"裸语句合法性"的**唯一权威源**。
- **进程级缓存**：`catalog.Default()` 用 `sync.OnceValues` 缓存——旧实现在每次 `Check` 都重解析全部 20+ JSON（约 16 ms/call），现在降至 ~7.5 µs/call；AI 修复循环（`MaxRepair+1` 次）在长会话下不再重复付出这个代价。
- **覆盖核查**：对内核权威枚举 `Commands.java`（548 个命令常量）逐一比对——**缺失 0**；JSON 仅多收一个 parser 函数 `REAL`（保留字，非命令）。
- **匹配粒度**：命令名精确 + 参数个数 + 粗类型（按 overload 匹配）；字面量参数宽松通过（不阻塞有效性判定）。
- **诊断增援**：`sig` 报告 `cmd/arg` 时，诊断**附该命令的正确 overload 签名（前 3 条 + 总数）**，AI 修复循环据此直接改正；`cmd/unknown` 附最近命令名 + 官方 URL。

---

## 5. 精度与求值（`number` 包）

- **退化判定**走精确求值：`math/big.Rat` + 保留常量 + **嵌套算术**（`+ - * / ^ 括号`）+ **科学计数法字面量**（`1e3`、`2.5E-2`、`1E-16`）。用于判断半径/坐标的正、零、负，决定是否退化。
- **π / e**：无理数，按**高精度有理逼近**求符号（足够精确判正/零/负；不做逐位相等）。
- **一元负号**：由 `number` 解析器自身处理（不通过 `isNumberToken` 吸进 token）；`mantissaEndsWithE` 只在 token 已经是"数字尾 + e"时才把 `+/-` 吸进去，所以 `e - 3` 不会被误读成 `e-3` 的科学计数法。
- **幂运算**：右结合，且 `^` 比一元负号更紧（`-2^2 == -(2^2)`，与 GeoGebra/数学一致）；`^` 仅接受非负小整数指数，且指数有上限（`maxExponent = 4096`），防在不可信 AI 输出里出现超大指数时失控循环。
- **不全局求坐标**：不引入 float 交点误差问题，这也是"只判可建立"比数值校验轻的原因。

---

## 6. 数值化表达式（`text.ReclassifyNumericExprs`）

文本输入里的裸表达式（`r = d + 1`）在 build 阶段被物化为 `KFunction` 占位。`catalog.ApplyKinds` 把命令对象的粗类型补齐后，`ReclassifyNumericExprs` 迭代到不动点：

- 若表达式所有自由标识符都是数字字面量 / 保留常量 / 已知的 `KNumber` 对象 → 升级为 `KNumber`。
- 函数定义（`Params` 非空）永远保持 `KFunction`。
- 迭代到不动点（`r = d + 1` 在 `d = Distance(...)` 之前出现时，`d` 被解析后才在这一轮升级；每轮只可能把 `KFunction → KNumber`，所以 |V| 轮内一定收敛）。

**为什么要后置**：命令产生的类型是数据驱动的（`cmdmeta.json`），在 build 阶段还不知道。若把 `r = d+1` 过早标成 `KFunction`，后续 `sig` 会允许把它传给需要 `Number` 的槽位——正是这次修复要堵的漏洞。

---

## 7. 包布局（Go，纯 stdlib）

```
cmd/ggbcheck/main.go       # CLI：check 子命令 + --json / --input + 退出码 + /dev/stdin
cmd/ai-server/main.go      # 可选 HTTP 服务（见 DESIGN-AI.md）
internal/
  text/               # 文本 → 对象图：lexer+parser+build+命令嵌套物化+绑定变量+字符串感知
                      #   科学计数法字面量、纯算术表达式 → KFunction 占位、KNumber 后置数值化
  ir/                 # 唯一共享对象图 ir.Graph + 种类枚举 + IR JSON 解析（两条输入的接缝）
  number/             # 精确算术求值器（big.Rat + π/e + 嵌套算式 + 科学计数法）
  catalog/            # 命令表：go:embed 20 分类 + supplement + cmdmeta.json
                      #   sync.OnceValues 进程级缓存；KindOf / IsScriptingCommand / ApplyKinds
  sig/                # 签名匹配：命令名 / 参数个数 / 粗类型（overload），诊断附正确签名
  deps/               # 依赖无环：邻接表 Kahn + Tarjan SCC 真环归因 + blocked 下游报告
  geo/                # 退化判定（复用 number 精确求值）
  reach/              # 目标可达 & 顶层对象
  check/              # 编排：两条输入 → 全部阶段 → 收据
  diag/               # 错误码、中文诊断、收据结构
```

> **接缝原则**：`ir.Graph` 是唯一共享类型。`text` / `ir` 两条输入都产它；`sig` / `deps` / `geo` / `reach` 只吃它。改动一端不碰另一端，只碰 `ir`。
> **类型数据化**：命令 → 返回粗类型（原 `ir.kindForCmd` 200+ 行 switch）与 Scripting 命令集合（原 `ir.scriptingCommands` 67 项 map）都移入 `catalog/cmdmeta.json`。`check.Check` 在编排时经 `catalog.ApplyKinds` 落到 `ir.Graph`；`text.Parse` 依赖 `catalog` 判断裸语句合法性。

---

## 8. CLI

```
ggbcheck check <file>                 # 主命令：整条链 + 收据
ggbcheck check --json <file>          # 结构化收据（默认人类可读中文）
ggbcheck check --input text|ir <file> # 强制输入形态（默认自动识别）
ggbcheck check /dev/stdin             # Unix 管道/重定向；不支持 `-` 简写
```

退出码：`0` 全部通过 / `1` 构造不可建立（有错误码）/ `2` 用法或输入错误。

收据：`ok` / `errors[]`（码 + 中文 + 对象 + 行号 + 官方 URL）/ `warnings[]` / 可执行对象清单（拓扑序）/ `executable`。

---

## 9. 错误码

| 码 | 含义 |
|---|---|
| `parse/syntax` | 行无法解析（等号/括号/命令调用） |
| `parse/json` | IR JSON 结构损坏 |
| `dep/redefine` | 对象重复定义 |
| `dep/undefined` | 引用了未定义对象 |
| `dep/cycle` | 依赖成环——**精确归因**：只列真环成员（每个 SCC 一条），被牵连的下游单独报告 |
| `cmd/unknown` | 命令不在命令表里（拼写错最常见；诊断附最近命令 + 官方 URL） |
| `cmd/arg` | 参数个数/类型不匹配任何签名（诊断附正确 overload 签名，前 3 条 + 总数） |
| `geo/degenerate` | 退化构造（零半径圆、重合点直线等） |
| `goal/unreachable` | goals 里有不存在的目标对象 |
| `usage` | 用法/命令表加载错误 |

---

## 10. 测试策略

- 纯标准库，无外部依赖，`go test ./...` 全绿。
- 各包单元测试：
  - `text`：解析 / 嵌套 / 绑定变量 / 保留常量 / **科学计数法** / **字符串感知切分** / **KNumber 后置数值化**；
  - `number`：求值 / 零判断 / **`e - 3` 与 `e-3` 的区别** / 幂运算 / 保留常量；
  - `geo`：退化 / π/e / 嵌套算式；
  - `sig`：overload / 诊断签名附注；
  - `catalog`：map+array 合并 / 缺漏 / **cmdmeta.json 加载 / KindOf / IsScriptingCommand**；
  - `deps`：Kahn 拓扑序 + **Tarjan SCC 真环归因** + **blocked 下游**；
  - `check`：端到端多场景。
- 集成夹具在 `testdata/exam/`（**18 道**中考/高考风格正例：注释、四心、切线、椭圆焦点、函数拟合、3D 等）与 `testdata/exam-bad/`（**13 道**应拒绝的反例：传点、退化、环、未知命令等），由 `internal/check` 端到端测试统一驱动。
- 覆盖核查（`catalog` 单测）：命令总数 ≥ 400 是弱下限；实际合并后 549 条，与内核枚举全对齐。

---

## 11. 待定（v1 未定，不挡当前功能）

1. 模块路径 `github.com/hycjack/geogebra-dsl-go` 为占位，发布前可换真实地址。
2. 文本暂**一行一条指令**（不支持 `;` 分号多指令同行的拆分）。
3. 命令嵌套合成对象的命名用 `owner.SubCmdN`——若涉及与 GeoGebra 实际自动命名（如 `A_1`）对齐，需后续确认。
4. 外部命令表覆盖（`--input` flag 已预留外部加载）路径未接完整 UI。
