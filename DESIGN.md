# ggbcheck — GeoGebra 指令校验器设计文档

> **唯一目的**：校验 AI 生成的 GeoGebra 指令**正确、可建立**。
> **输入**：文本脚本 / IR JSON 两种形态，走同一份对象图，共享同一条判定链。
> **判定**：构造可建立（命令存在且签名对、依赖无环、对象无退化、目标可达）。
> **交付**：只要收据/报告——不产 `.ggb` 文件、不数值求解、不内置绘图。
> **命令表**：内嵌 GeoGebra 官方分类命令库（20 个 JSON）+ 源码核验的 `supplement.json`，**合并去重 587 条**（官方 495 + supplement 92），共 1742 条 overload。**与本地内核 `Commands.java`（550 条）差集比对：真实缺失 0 条**（详见 §4）。
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
- **字符串感知**：`splitArgs` 与 `stripComment` 都用同一套字符串引号感知——`Text("a, b")` 里的逗号不算分隔符，`Text("x, y", P)` 也不会被误切。
- **数字字面量**：`isNumber` 接受整数、小数、`+/-` 前缀、科学计数法（`1e3`、`2.5E-2`），与 GeoGebra 一致；至少一位数字（`.` 或 `+` 单独不算数字）。

### 3.2 IR JSON

- 结构：`{ "objects":[{id, kind?, cmd?, args?, refs?}], "goals":[id, ...] }`。
- 命令名走同一套目录签名；IR 侧**必须**提供 `kind`（或 `cmd`），因为 IR 是权威输入，不承担"从命令名猜类型"的责任。
  - **已强制执行**：`kind` 与 `cmd` 同时缺失、或 `kind` 写了无法识别的拼写，一律报 `parse/json`。此前这类空壳对象会被判为"可建立"——`sig` 无签名可查、`geo` 无从检验、`reach` 只看目标存在与否，于是 `{"objects":[{"id":"x"}],"goals":["x"]}` 直接 `ok:true`。
- 若 `objects` 缺失、JSON 不合法、`goals` 类型错，直接 fail-closed。
- **IR 也会走文本包的数值化**：`check.Check` 对两条输入统一调用 `text.ReclassifyNumericExprs`，因此 IR 对象声明 `kind:"Function"` 而 `args` 为空时必须跳过分类（不得越界）。这是已知的分层欠账——该函数只操作 `*ir.Graph`，与文本解析无关，理应属于独立的语义层而非 `text` 包。

### 3.3 形态自动识别

- 以 `{` 开头且能解析成 JSON → IR；否则文本。
- `--input json|text` 可强制。

---

## 4. 命令表（587 条）

- **主源**：20 个官方分类 JSON（`geogebra-commands/*.json`，`commands` 的 map 与 array 两种 shape 都支持），`go:embed` 进二进制。
- **补充**：`supplement.json`（92 条命令 / 165 条 overload，逐一对照内核 `Cmd*` 处理器核实；也用于纠正主源的类型标注，如 `If` 的 `<Then>/<Else>` 主源写成 `<Object>` 但内核按表达式求值）。
- **元数据**：`cmdmeta.json` 两份数据：
  - `returns`：命令 → 返回粗类型（`Point` / `Line` / `Number` / `Polygon` / `Quadric` / `Plane` / `Polyhedron` / `List` / `Text` / `Matrix` / `Curve` / `Conic` / `Boolean` / `Script` …），数据驱动替代了原来 200+ 行的 `kindForCmd` switch。当前 103 条。
  - `scripting`：官方 67 条 Scripting 命令集合（返回 "Script"），也是"裸语句合法性"的**唯一权威源**。
- **进程级缓存**：`catalog.Default()` 用 `sync.OnceValues` 缓存——旧实现在每次 `Check` 都重解析全部 20+ JSON（约 16 ms/call），现在降至 ~7.5 µs/call；AI 修复循环（`MaxRepair+1` 次）在长会话下不再重复付出这个代价。
- **计数口径**：587 = 21 个 JSON 文件去重合并后的命令名总数（官方分类 495 + supplement 92），同一命令出现在多个分类文件时 overload 会累加但只计一条。
- **覆盖核查（2026-10-01 对本地内核源码实测）**：内核命令枚举位于
  `source/shared/common/src/main/java/org/geogebra/common/kernel/commands/Commands.java`。
  **注意它已从"一堆 `public static final CommandName` 常量"重构为 `enum Commands implements CommandsConstants`**，表下标常量移到了 `CommandsConstants`——所以按旧结构 grep 的核查脚本必然失效，这正是本项长期无法自动化的原因。当前应改为抓 enum 条目（行首缩进的 `Name(TABLE_*)`）：
  ```
  grep -oE '^\s{1,2}[A-Z][A-Za-z0-9_]*\(' Commands.java | tr -d ' (' | sort -u
  ```
  实测 **550 条**（去重大写；原始条目 554，含 4 组大小写变体）。与命令表差集：
  - **内核有、命令表缺：0 条。** 表与这版内核**完全对齐**。
  - 命令表有、内核无：**38 条**，逐条定性如下：
    - **约 28 条数学函数**（`Sin`/`Cos`/`Sqrt`/`Ln`/`Abs`/`Round`/`Floor`/`Sign`/`Stdev`/`Var`…）——它们是 `ParserFunctionsFactory` 的 **parser 函数**，不是 `Commands` 枚举成员。`supplement.json` 有意并入，好让 `h = Sqrt(5)`、`r = Cos(0)` 这类 AI 常见写法通过。**合法，非幻觉**。
    - `Real`——parser 保留字，非命令，符合预期。
    - **6 条带 `alias` 字段的"恢复命令"**（见下条），有意保留。
    - **3 条确认的幻觉残留**：`ParallelLine` / `LineThrough` / `CircleWithCenter`——全内核源码搜索 0 命中或仅命中 GUI 常量类。`232e6d2` 的幻觉清理漏了这 3 条。
- **⚠️ `alias` 字段目前是死数据**：`Incenter→TriangleCenter`、`Circumcenter→TriangleCenter`、`Orthocenter→TriangleCenter`、`Circumcircle→Circle`、`RegularPolygon→Polygon`、`TextBox→Textfield` 共 6 条在 JSON 里标了真实命令，但 `catalog.go` 的 `rawDoc` / `Command` **没有 `Alias` 字段**，全仓无任何 Go 代码读它。后果：`f296d71`「添加 alias 字段指向真实命令」的意图没有落地——用户写 `Incenter(A,B,C)` 会通过校验，但**永远不会看到"真实命令是 TriangleCenter"这条提示**。另 3 条（`ArcCot`/`ArcSec`/`ArcCsc`）连 alias 字段都没有。
- **自动覆盖核查仍缺位**：`catalog` 单测只有 `len(Names()) >= 400` 的单向下限，检测不到单条命令缺失（少一条真命令 587→586，测试照样绿）。差集比对脚本依赖本地内核源码路径，无法进 CI——待办：把权威命令名清单（纯文本，554 行）随仓库固化，测试从下限换成差集双向比对。
- **匹配粒度**：命令名精确 + 参数个数 + 粗类型（按 overload 匹配）；字面量参数宽松通过（不阻塞有效性判定），**但只限值槽与表达式槽**。对象通配槽（`<Object>` / `<GeoObject>` / `<Geometric Object>` / `<Region>` / `<Image>` / 表格单元格 / UI 控件等）只收真实对象，裸数字/布尔一律拒绝——否则 `Point(0, 0)` 会被误读成"对象 0 上的参数点 0"、`Rotate(0, 90, O)` 会被当成平移一个数字。字面点请写 `A = (0, 0)` 或 `Point({0, 0})`；`<Expression>` / `<Any>` / `<Variable>` / `<Name>` / 枚举值等槽保持宽松（`Sequence(2, k, 1, 10)`、`Text(0)`、`If(cond, 1, 2)` 都合法）。
  - **命令名大小写不敏感**：查表、签名匹配、标签类型预测三处统一按大写处理。标签类型表若按原始大小写查，`midpoint(A,B)` 会掉进 `General` 字符集拿到标签 `a`，与脚本里后面的 `a = 5` 冲突并误报 `dep/redefine`。
  - **用户自定义函数**：`f(x) = ...` 的调用名不在命令表内，但它是图里真实存在的对象。目录未命中时先查图中的函数定义并按其 `Params` 校验元数（错则报 `cmd/arg`，绝不报 `cmd/unknown`）；**目录优先**——真命令永远压过同名用户定义，与 GeoGebra 一致。参数类型未建模，故只校验元数。
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

收据：`ok` / `errors[]`（码 + 中文 + 对象 + 行号 + 官方 URL）/ `warnings[]` / 可执行对象清单（拓扑序）/ `executable` / `kinds`。

**收据契约**：`errors` / `warnings` / `executable` / `kinds` 四个集合字段**恒为 `[]` / `{}`，绝不序列化为 `null`**。`NewReceipt` 负责初始化——WASM 入口的文档明说宿主直接拿收据驱动修复循环，而 JS 侧 `receipt.errors.forEach(...)` 遇到 `null` 会直接抛。

> 收据是 CLI / HTTP / WASM 三方的集成面，**目前没有 schema 版本**。加字段容易、改字段无保护；`errors` 曾是唯一漏初始化为 `[]` 的集合，就是缺契约的产物。

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
  - `number`：求值 / 零判断 / **`e - 3` 与 `e-3` 的区别** / 幂运算 / 保留常量 / **超大指数不截断**（`IsInt64` 拦截 + 读最低位取奇偶性，`0^2^64` 必须为 0 而非 1）；
  - `geo`：退化 / π/e / 嵌套算式 / **半径经变量与算式代入**；
  - `sig`：overload / 诊断签名附注 / **用户自定义函数的元数校验**；
  - `catalog`：map+array 合并 / 缺漏 / **cmdmeta.json 加载 / KindOf / IsScriptingCommand**；
  - `deps`：Kahn 拓扑序 + **Tarjan SCC 真环归因** + **blocked 下游**；
  - `check`：端到端多场景（**56 个**测试函数）。
- 集成夹具在 `testdata/exam/`（**18 道**中考/高考风格正例：注释、四心、切线、椭圆焦点、函数拟合、3D 等）与 `testdata/exam-bad/`（**14 道**应拒绝的反例：传点、退化、环、未知命令等），由 `internal/check` 端到端测试统一驱动。
- 覆盖核查（`catalog` 单测）：**只是** `len(Names()) >= 400` 的单向下限，检测不到单条命令缺失；实际合并后 587 条。**2026-10-01 已用本地内核源码做过一次完整差集比对，真实缺失 0 条**，方法与 38 条反向差异的逐条定性见 §4；但该比对依赖内核源码路径，尚无法进 CI 自动执行。

---

## 11. 待定（v1 未定，不挡当前功能）

1. 模块路径 `github.com/hycjack/geogebra-dsl-go` 为占位，发布前可换真实地址。
2. 文本暂**一行一条指令**（不支持 `;` 分号多指令同行的拆分）。
3. 命令嵌套合成对象的命名用 `owner.SubCmdN`——若涉及与 GeoGebra 实际自动命名（如 `A_1`）对齐，需后续确认。
4. 外部命令表覆盖（`--input` flag 已预留外部加载）路径未接完整 UI。
