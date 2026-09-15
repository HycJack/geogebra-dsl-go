---
name: ggbcheck
description: 用 ggbcheck 校验 AI 生成的 GeoGebra 指令脚本（文本或 IR JSON）能否被建立、是否有错，拿到结构化/可读的校验收据并据错误码逐条修复。Use when the user wants to validate, check, review, or repair a GeoGebra instruction script for correctness and executability. Keywords: GeoGebra 校验, 指令检查, ggbcheck, validate GeoGebra script, check GeoGebra commands.
---

# ggbcheck — GeoGebra 指令校验器

校验 AI 生成的 GeoGebra 指令**是否正确、能否被执行**。单二进制的命令行工具，
纯标准库。它只做「能不能建立」的校验，不产 `.ggb` 文件、不绘图、不算坐标——
桌上几何交给 GeoGebra 自己。

先用，别手猜：**拿到一段 GeoGebra 脚本，第一件事就是拿 ggbcheck 过一遍**，
而不是靠眼读去估。

## 构建与运行

命令行工作目录是本仓库 `geogebra-dsl-go`。若没有现成二进制，先构建：

```bash
go build -o ggbcheck.exe ./cmd/ggbcheck
```

校验一条脚本（默认自动识别文本脚本还是 IR JSON，但**必须写成文件**，不支持 stdin）：

```bash
ggbcheck check path/to/script.txt        # 人类可读收据（最常用）
ggbcheck check --json path/to/file       # 结构化收据（管道/脚本用）
ggbcheck check --input text|ir <file>    # 强制输入形态（默认自动识别）
```

- 参数里可以没有 `check` 子命令（`ggbcheck <flags> <file>` 也可），`check` 可省略。
- `/code-review` 这类查看代码的任务不需要它；它是**运行校验器**用的。

## 退出码

| 码 | 含义 |
|----|------|
| `0` | 全部通过，构造可以建立 |
| `1` | 构造不可建立（语义/无环/退化/可达任一失败） |
| `2` | 用法或输入结构错误（usage / parse/syntax / parse/json） |

> 注意：**用法错误返回 2**。文件读不到、flag 拼错、缺文件参数也返回 2。
> 若返回 2 且没有 `❌ 校验未通过` 的 JSON 输出，多半是命令本身写错了，先核对用法。

## `--json` 收据结构（机器可读）

```jsonc
{
  "ok": true,               // 全部通过 ⇔ true
  "errors": [],             // 阻断性错误，任意一条非空则 ok=false
  "warnings": [],           // 非阻断性提示（当前恒为 []）
  "executable": ["a","b"],  // 对象 id 的拓扑（可执行）顺序
  "source_in": "text"       // "text" 或 "ir"
}
```

`errors[]` 里的每条 Problem：

```jsonc
{
  "code": "cmd/unknown",    // 稳定错误码，见下表
  "msg":  "…",              // 人类可读描述
  "obj":  "c",              // 涉及的对象 id（非对象级时省略）
  "line": 3                 // 1 起始的行号，未知为 0（文本脚本才有）
}
```

## 错误码（逐条修复依据）

| 码 | 含义 | 常见 AI 生成脚本的成因 | 修复方向 |
|----|------|------------------------|----------|
| `parse/syntax` | 行无法解析（等号/括号/命令调用语法） | 括号不配对、多个 `=`、漏逗号 | 先修语法；结构损坏是 fail-closed，后面的错误不会同时给出 |
| `parse/json` | IR JSON 结构损坏 | 缺 `objects`、JSON 不合法 | 修 JSON |
| `dep/redefine` | 对象重复定义 | 同一变量名赋了两次 | 改名或合并 |
| `dep/undefined` | 引用了未定义对象 | 打字错误、对象名不一致、前面没用过 | 先定义再引用，或统一名字 |
| `dep/cycle` | 依赖成环，无法定序 | `A` 依赖 `B` 又反向依赖 | 打破环 |
| `cmd/unknown` | 命令不在命令表里 | **拼写错误最常见** | 核对命令名（如 `Circle` 不是 `Ciricle`） |
| `cmd/arg` | 参数个数/类型不匹配任何签名 | 参数个数错、类型错 | 查该命令的合法签名 |
| `geo/degenerate` | 退化构造 | 半径≤0 的圆、重合两点的直线 | 改参数 |
| `goal/unreachable` | goals 里有不存在的目标对象 | IR 里 `goals` 提到未定义 id | 补定义或改 goals |

命令表内嵌全部 **549 条** GeoGebra 指令（含 3D、统计、CAS、变换），由官方分类库
合并而来。判断 `cmd/unknown` 时以命令表为准，不要凭记忆。

## 判定链（报告策略很重要）

文本/IR 会走到同一个对象图，然后过四道关：**语义**（命令存在+签名）、**无环**
（拓扑排序）、**退化**（半径≤0、重合点直线，用高精度有理数精确判定）、**可达**
（goals/顶层对象可建立）。

只要结构能解析，语义/无环/退化/可达会把**所有独立错误一次全报出来**（不是报第一个
就停）——这正是修复 AI 输出的正确方式：**逐条对照 `errors[]` 全改完再重跑**，减少来回。
只有结构损坏（`parse/syntax`、`parse/json`）才 fail-closed 直接停。

### 命令嵌套

参数里出现子命令会**物化为合成对象**参与全链校验。例如：

```text
c = Circle(Midpoint(A, B), 2)
```

会自动生成 `c.Midpoint1` 对象，校验 `Midpoint` 的签名、建立依赖边、参与无环与
退化判定。所以嵌套命令也能被检出来（比如 `Midpoint` 参数个数不够会报 `cmd/arg`）。

### 保留值

`pi`（π）、`e`、`γ`、`i`、`freehand`、`deg` 是保留常数，不能当变量名；`pi`/`e`
可直接作数值用。退化判定支持含这些常数的嵌套算式，例如 `Circle(A, 2*pi)` 判为正
半径（通过），`Circle(A, 2*pi - 2*pi)` 判为半径恰好 0（退化拒绝）。

## 工作流：校验 → 修复 → 复检

1. 把 AI 生成的脚本写进临时文件（`script.txt` 或 `script.json`）。
2. `ggbcheck check --json <file>` 拿结构化收据。
3. 若 `ok:false`，**逐一**按 `errors[]` 里每条 `code`+`msg`+`obj`(+`line`) 修脚本；
   嵌套命令的错误对象名是合成名（如 `c.Midpoint1`），据此定位。
4. 改完**重跑**直到 `ok:true`、退出码 `0`。
5. 通过后再把脚本交给用户 / 写入 `.ggb` 或喂给 ai-server。

也支持 CI / 管道：`ggbcheck check --json file > receipt.json` 再按 `ok`/`errors` 解析。

## 普通错误速查

- **`✅ 校验通过`** 且退出码 0：可以放心交付。
- **`❌ 校验未通过`** 且退出码 1：构造有问题，`errors[]` 已列出全部。
- **退出码 2**：用法 / 输入结构坏了。先核对：文件在不在、flag 对不对、是文本脚本
  还是 IR JSON（必要时加 `--input text|ir` 强制形态）。

## 相关

完整设计与判定细节见仓库 `README.md`；目录结构见同文件「目录结构」一节。构建该
工具前的源码没在本技能范围内——本技能只负责**用它**。