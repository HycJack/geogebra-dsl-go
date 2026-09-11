# GeoGebra 指令校验器 (Go) — 设计说明

> 唯一目的：**校验 AI 生成的 GeoGebra 指令正确、可以执行。**
> 输入：两种形态，走同一条判定链——① 文本/多行指令，② IR JSON（`objects[]`+`goals[]`，备选输入）。
> 判定：构造可建立（依赖无环、无退化、目标可求得/可达）。交付：只要收据/报告，不产文件。
> 命令表：以 geogeo 在用的 `geogebra-main/3D_Commands.json` 富目录为主。

## 1. 范围（明确的不做）

| 做 | 不做 |
|---|---|
| 解析文本指令（多行，指令/赋值/变量定义） | 计算坐标/几何数值（只判可建立，不数值求解） |
| 解析/承接 IR JSON 输入（`objects[]`+`goals[]`，沿用现有 shape） | 生成 `.ggb` 脚本（不交付，只报告） |
| 校验命令存在性 + 签名（参数个数/类型） | 内置绘图/可视化 |
| 校验构造可建立：依赖无环、对象无退化、目标(goals)可达 | 公开库 API（v1 单 CLI 内部实现） |
| 输出结构化收据：ok + 错误码 + 中文诊断 | 多语言/RIch UI |
| 输出拓扑序可执行的对象清单（证明"可执行"） | |

## 2. 两条输入，一份对象图

无论哪种形态，先都建出**同一份内部对象图**（object DAG：`id → (cmd, args, refs)`），此后的判定完全共享。

```
  输入 A：文本 / 多行指令           输入 B：IR JSON
       │                                │
   [1a] 解析文本                     [1b] 解析 JSON
       按行切分+括号/逗号/等号配对          → objects[](id, kind|cmd, args, refs)
       配不平→fail                      → goals[](目标 id)
       │                                │
       └──────────┬─────────────────────┘
                  ▼
           同一份对象图 DAG
                  │
                  ▼
   [2] 语义   命令存在性+签名（目录）   /   语法怪 → fail-closed 停
   [3] 无环   依赖拓扑排序，有环→fail
   [4] 无退化 半径≤0/重合/共线→退化→fail
   [5] 可达   goals[] 目标在图里都能建出（文本输入若无 goals，则以"全部顶层对象"为 targets）
   [6] 收据   ok / errors[] / warnings[] / 可执行对象清单(拓扑序) + 退出码
```

- **IR shape（沿用现有）**：`objects[]{id, kind|cmd, args, refs}` + `goals[]` + 可选 `presentation{}`。`kind|cmd` 里的命令名走同一套目录签名校验，`refs` 即依赖。
- **goals 语义**：IR 输入带 `goals[]` 时，校验这些对象都能建立且在图内可达；目标建不出 → fail。文本输入没有 goals，则以"全部顶层对象"（不被任何对象引用的最终对象）作为 targets。
- 输入形态自动识别：以 `{` 开头且能解析成 JSON → 当 IR；否则当文本。(可用 `--input json|text` 强制。)

fail-closed：任何一步不过，后面不跑，直接给错误码（解析/JSON 结构错 `parse/*`、命令不存在 `cmd/unknown`、参数错 `cmd/arg`、环 `dep/cycle`、重复定义 `dep/redefine`、未定义引用 `dep/undefined`、退化 `geo/degenerate`、目标不可达 `goal/unreachable`）。

## 3. 命令表（唯一外部数据）

- 主源：geogeo 已读的 `geogebra-main/3D_Commands.json`（含命令参数/回退类型），`go:embed` 内嵌进二进制。
- **解析粒度**：命令名(`name`) + 参数个数(自动从 args 数) + 粗类型（点/线/圆/数/布尔…）。匹配用"命令名精确 + 参数个数 + 类型尽量匹配"，够判定 AI 指令对错。
- 支持外部文件覆盖默认目录（env/flag），便于补充 AI 常生成、目录还没有的新命令。

## 4. 精度

- 退化判定用 `math/big.Rat` 走精确（半径差、两点重合、三点共线都精确比较），这是唯一要求精确的地方。
- 不数值求解坐标 → 不引入 float 交点误差问题。这也是为什么这版比 geogeo 的 gate4（目标校验）简单：不需要算坐标验证，只要"对象能建出来并在图中可达"即判定可执行。

## 5. 包布局（Go，纯 stdlib）

```
cmd/ggcm/main.go      # 单二进制
internal/
  lexer/              # 按行+括号/逗号/等号切 token（fail-closed）
  parser/             # token → 指令序列（命令名、参数、赋值/变量定义）
  ir/                 # 唯一共享对象图 IS(cmd,args,refs) + IR JSON 解析(1b)
  catalog/            # 命令表：go:embed 富目录 + 外部覆盖
  sig/                # 签名匹配：命令名/参数个数/粗类型
  build/              # 文本指令 → 逐条建对象图（redefine/undefined 检测，对接 DSL AST）
  deps/               # 依赖无环检查 (topo sort)
  geo/                # 退化判定（big.Rat 精确）
  reach/              # 目标可达 & 可执行对象清单(拓扑序)
  diag/               # 错误码、中文诊断、收据结构
```

> `ir` 是接缝：`parse`(文本) 走 `build` 建图，IR JSON 直接建图，两边都产同一份 `ir.Graph`，无环/退化/可达只吃 `ir.Graph`。

## 6. CLI

```
ggcm check <file>                # 主命令：整条链 + 收据
ggcm check --json <file>         # 结构化收据（默认给可读中文）
                                # 输入形态自动识别（{ 开头能解析→IR；否则文本）；--input json|text 强制
```
退出码：`0` 全部通过 / `1` 构造不可建立（有错误码） / `2` 用法或输入错误。

收据内容：`ok` / `errors[]` / `warnings[]` / 可执行对象清单(拓扑序) —— 这份清单就是"可以执行"的证明。

## 7. 起步

最小闭环：`lexer`+`parser`+`ir`(文本→图)+`catalog`+`sig`+`deps` 先跑通一条简单文本（如 `Line(A,B)` + `Point` 定义 + 一个 `Circle`），`ggcm check` 输出 ok 和可执行清单；端到端跑通后，再加 IR JSON 输入(1b)与 `geo`(退化)、`reach`(可达/目标)。每一段都从红测试起步。

## 待定

1. 模块路径 `github.com/you/geogebra-dsl-go` 占位，开工前可换真实地址。
2. 文本切分要不要支持 `;` 分隔单行多指令，还是严格一行一条。
3. 目录到底是内嵌富目录即可，还是想要一个精简的教学子集当默认。
4. IR JSON 的 `kind|cmd` 里，`kind`(如 point/line) 与 `cmd`(如 Circle) 谁优先做签名匹配——先按 `cmd` 匹配，`kind` 作交叉校验。