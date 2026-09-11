# ggcm — GeoGebra 指令校验器

校验 AI 生成的 GeoGebra 指令**是否正确、能否被执行**。单二进制的命令行工具，纯标准库（Go ≥ 1.21）。

输入两种形态，走同一套判定链：

| 形态 | 示例 |
|---|---|
| 文本脚本 | `A = Point(0, 2)` / `l = Line(A, B)` / `c = Circle(C, T)` |
| IR JSON | `{ "objects": [{"id":"A","cmd":"Point","args":["0","2"]}], "goals": ["l"] }` |

> 你的目的就是这一个：**人多眼杂地挑出 AI 输出里的错，一条命令看全部**。所以它只做"能不能建立"，不做数值求坐标、不产 `.ggb` 文件、不内置绘图——绘图交给 GeoGebra 自己。

## 构建与运行

```bash
go build -o ggcm.exe ./cmd/ggcm

ggcm check path/to/script.txt        # 人类可读收据
ggcm check --json path/to/file       # 结构化收据（管道/脚本用）
ggcm check --input text|ir <file>    # 强制输入形态（默认自动识别）
```

退出码：`0` 全部通过 / `1` 构造不可建立 / `2` 用法或输入错误。

## 判定链（fail-closed：只在必要时停）

```
  文本 ──▶ 解析(括号/逗号/等号) ──▶ 构建对象图(重复定义/未定义引用)
  IR  ──▶ 解析 JSON ────────────────┘
                                        │
                     同一个对象图 DAG
                                        │
      [语义] 命令存在 + 参数签名（嵌入 GeoGebra 富命令表）
      [无环] 依赖拓扑排序（有环 → dep/cycle）
      [退化] 半径≤0 / 重合点直线 / 零半径圆（big.Rat 精确）
      [可达] goals（IR）或顶层对象（文本）都能建立
                                        │
                                      收据
```

**报告策略**：只要结构能解析，语义/无环/退化/可达会把**所有**独立错误一次全报出来（不是报第一个就停）——方便你对照 AI 输出逐条改。只有输入结构损坏（如一行无法解析、IR 缺 objects）才 fail-closed 直接停。

## 错误码

| 码 | 含义 |
|---|---|
| `parse/syntax` | 行无法解析（等号/括号/命令调用语法） |
| `parse/json` | IR JSON 结构损坏 |
| `dep/redefine` | 对象重复定义 |
| `dep/undefined` | 引用了未定义对象 |
| `dep/cycle` | 依赖成环，无法定序 |
| `cmd/unknown` | 命令不在命令表里（拼写错误最常见） |
| `cmd/arg` | 参数个数/类型不匹配任何签名 |
| `geo/degenerate` | 退化构造（零半径圆、重合点直线等） |
| `goal/unreachable` | goals 里有不存在的目标对象 |
| `usage` | 用法/命令表加载错误 |

## 命令表

命令表来自 GeoGebra 官方分类命令库 `geogebra-commands/`（取自
`ggb-gen-api/geogebra-commands`），内嵌全部 20 个分类 JSON 并合并成一份，
再补充 `supplement.json`（所有签名逐一对照 GeoGebra 内核源码
`org.geogebra.common.kernel.commands` 的 Cmd 处理器手动核验），当前 **549 条**。

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
cmd/ggcm         CLI 入口（check 子命令 → exit code）
internal/
  text/          文本脚本 → 对象图（lexer+parser+build）
  ir/            共享对象图 + IR JSON 解析（两条输入的汇合点）
  catalog/       命令表（go:embed 全部分类 JSON，合并 504 条命令）
  sig/           签名匹配（命令名/参数个数/粗类型）
  deps/          依赖无环 + 拓扑序
  geo/           退化判定（math/big.Rat 精确）
  reach/         goals 可达 & 可执行清单
  check/         编排：输入 → 全部阶段 → 收据
  diag/          错误码 + 收据结构
```

## 测试

```bash
go test ./...
go vet ./...
```

集成用例在 `testdata/`（distance-1 教学构造、undefined、degenerate、cycle、
unknown-cmd、ai-bad 等），既有 CLI 手测也有单元测试。