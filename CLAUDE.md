# CLAUDE.md — 容器根指引

本仓库自 v1.12 起采用 **engine / gamelib / tool 三层** 拆分，容器根 `code/engine/` 只是 git 仓库根 + `go.work` 聚合点，**自身不再是 Go module**。

## 三层定位

| 层 | module | 定位 | 详细说明 |
|---|---|---|---|
| A | `engine/` | Actor + 分布式骨架（最小闭环） | `engine/CLAUDE.md` |
| B | `gamelib/` | 游戏侧通用功能运行时 | `gamelib/CLAUDE.md` |
| C | `tool/` | 开发 / 运维期可执行工具 | `tool/CLAUDE.md` |

## 依赖方向铁律（v1.12 §2.4）

```
tool   ──▶ gamelib ──▶ engine
 │                        ▲
 └────────────────────────┘   (tool 也可直接依赖 engine)

engine   不得 import gamelib / tool
gamelib  不得 import tool
better/  不被任何 module import
```

CI 守护：`cd tool && go run ./cmd/engine doctor deps --root ..` 必须零违规。

## go.work 用法

```bash
# 在仓库根执行（= 本文件所在目录）
go work sync                 # 同步三个 module 的 require 集合

# 构建
(cd engine && go build ./...)
(cd gamelib && go build ./...)
(cd tool && go build ./...)

# 测试
(cd engine && go test ./...)
(cd gamelib && go test ./...)
(cd tool && go test ./...)
```

> 若 `go.work` 中某个 module 未就位（缺 `go.mod`），`go work sync` 会直接报错；此时先按 v1.12 计划补齐骨架。

## 容器根目录结构

```
code/engine/
├── go.work                # use (./engine ./gamelib ./tool)
├── go.work.sum
├── README.md
├── CLAUDE.md              # 本文件，只描述容器级约束
├── .gitignore
├── doc/                   # 版本路线图与完成度审核
├── better/                # 只读参考（Leaf / Proto.Actor 源码，不编译、不引用）
├── engine/                # A 类 module
├── gamelib/               # B 类 module
└── tool/                  # C 类 module
```

Phase 6 预留的 `demo_game/` 与 `server/` 上线后同样落在本目录下，并加入 `go.work`。

## better/ 目录规则

`better/` 为参考实现目录（vendored Leaf 和 Proto.Actor 源码），遵循：

1. **不参与编译**：不进入任何 module 产物；
2. **不参与审核**：审核功能完成度时不计入已完成项；
3. **不参与测试统计**：`better/` 下测试失败不影响整体测试状态；
4. **不计入代码量**：统计 LOC 时排除；
5. **只读参考**：新代码应在 engine/gamelib/tool 对应模块内独立实现，**不得 import**。

## 版本与路线图

- 路线图与每个大版本完成度审核一律放 `doc/`；
- 当前基线：**v1.12 · 三层拆分**（2026-04-21，完成度见 `doc/v1.12_完成度审核.md`）；
- 各层内部细节（消息流、模块清单、设计模式）请看对应层的 `CLAUDE.md`，本文件不再复述。
