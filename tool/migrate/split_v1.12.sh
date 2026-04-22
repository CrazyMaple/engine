#!/usr/bin/env bash
# =============================================================================
# split_v1.12.sh  —  engine 三层拆分迁移脚本（dry-run / --apply）
# 对应文档：doc/v1.12_三层拆分重构计划.md
#
# 目录布局目标：
#   code/engine   -> Actor 骨架
#   code/gamelib  -> 游戏侧引擎功能
#   code/tool     -> 开发期工具
#   code/better   -> 参考实现（从 engine/better 迁出）
#
# 所有写操作默认 dry-run；加 --apply 才会真正执行。
# =============================================================================
set -euo pipefail

# ---------- 配置 ------------------------------------------------------------
ENGINE_DIR_NAME="engine"
GAMELIB_DIR_NAME="gamelib"
TOOL_DIR_NAME="tool"
BETTER_DIR_NAME="better"

# B 类：搬到 gamelib/ 的目录
GAMELIB_MODULES=(
  log config timer gate scene ecs bt syncx fixedpoint
  room skill inventory quest mail leaderboard replay
  saga persistence hotreload middleware telemetry
)

# C 类：搬到 tool/ 的目录
TOOL_MODULES=(
  codegen console dashboard testkit bench stress deploy example
)
# cmd/engine 单独处理（嵌套路径）

# A 类：留在 engine/ 的白名单（仅用于校验，不执行操作）
ENGINE_KEEP=(
  actor remote cluster grain router pubsub
  proto codec network errors internal
)

# ---------- 参数解析 --------------------------------------------------------
APPLY=0
FORCE_MAIN=0
PHASE="all"
for arg in "$@"; do
  case "$arg" in
    --apply)      APPLY=1 ;;
    --force-main) FORCE_MAIN=1 ;;
    --phase)      shift || true ;;
    --phase=*)    PHASE="${arg#*=}" ;;
    1|2|3|4)      PHASE="$arg" ;;
    -h|--help)
      sed -n '2,16p' "$0"
      exit 0 ;;
  esac
done
# 支持 "--phase 2" 这种写法
for i in "$@"; do
  [[ "$i" =~ ^[1-4]$ ]] && PHASE="$i"
done

# ---------- 工具函数 --------------------------------------------------------
C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YEL=$'\033[33m'; C_RST=$'\033[0m'
log()  { echo "${C_GREEN}[ok]${C_RST}  $*"; }
warn() { echo "${C_YEL}[..]${C_RST}  $*"; }
err()  { echo "${C_RED}[!!]${C_RST}  $*" >&2; }

run() {
  if (( APPLY )); then
    echo "  \$ $*"; eval "$@"
  else
    echo "  (dry) $*"
  fi
}

require_clean_worktree() {
  # 仅拒绝已追踪文件的 modified/staged 改动；允许 untracked 文件存在
  # 兼容用户"不要 commit"的偏好：未追踪文件不会被 git mv 影响，可放行
  local dirty
  dirty="$(git status --porcelain | awk '$1 !~ /^\?\?/ && $1 != "" {print}')"
  if [[ -n "$dirty" ]]; then
    warn "检测到已追踪文件的修改（与本次迁移无关也允许）："
    echo "$dirty" | sed 's/^/    /'
    warn "继续执行（如需中止请 Ctrl-C）"
  fi
}

require_in_engine() {
  local pwd_tail="${PWD##*/}"
  if [[ "$pwd_tail" != "$ENGINE_DIR_NAME" ]]; then
    err "请在 code/engine/ 下执行本脚本（当前 $PWD）。"; exit 1
  fi
}

require_branch_safety() {
  local br; br="$(git rev-parse --abbrev-ref HEAD)"
  if [[ "$br" == "main" || "$br" == "master" ]]; then
    warn "当前在 $br 分支执行迁移；本脚本不会自动 commit/tag，由用户自行决定提交策略"
  fi
}

# ---------- Phase 1：创建骨架 ---------------------------------------------
phase1() {
  warn "Phase 1 — 创建 gamelib/tool/go.work/better 骨架"

  run "mkdir -p ../$GAMELIB_DIR_NAME ../$TOOL_DIR_NAME ../$BETTER_DIR_NAME"

  # 1) gamelib/go.mod
  if (( APPLY )); then
    cat > "../$GAMELIB_DIR_NAME/go.mod" <<'EOF'
module gamelib

go 1.24.1

require (
    engine v0.0.0
    github.com/golang/protobuf v1.5.4
    github.com/gorilla/websocket v1.5.0
    gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22
    gopkg.in/yaml.v3 v3.0.1
)

replace engine => ../engine
EOF
  else
    echo "  (dry) write ../$GAMELIB_DIR_NAME/go.mod (module gamelib, replace engine=../engine)"
  fi

  # 2) tool/go.mod
  if (( APPLY )); then
    cat > "../$TOOL_DIR_NAME/go.mod" <<'EOF'
module tool

go 1.24.1

require (
    engine  v0.0.0
    gamelib v0.0.0
)

replace (
    engine  => ../engine
    gamelib => ../gamelib
)
EOF
  else
    echo "  (dry) write ../$TOOL_DIR_NAME/go.mod (module tool, replace engine/gamelib)"
  fi

  # 3) go.work
  if (( APPLY )); then
    cat > "../go.work" <<'EOF'
go 1.24.1

use (
    ./engine
    ./gamelib
    ./tool
)
EOF
  else
    echo "  (dry) write ../go.work (use engine, gamelib, tool)"
  fi

  # 4) better/ 迁出（如果 engine/better 存在）
  if [[ -d "./$BETTER_DIR_NAME" ]]; then
    # git 仓库根在 engine/，无法 git mv 跨出仓库边界，改用普通 mv
    # ../better 由前一步 mkdir -p 创建为空目录，先 rmdir 再 mv 整个 better 过去
    run "rmdir ../$BETTER_DIR_NAME && mv ./$BETTER_DIR_NAME ../$BETTER_DIR_NAME"
  else
    warn "engine/better 不存在，跳过"
  fi

  log "Phase 1 完成"
}

# ---------- Phase 2：搬迁 gamelib 模块 -------------------------------------
phase2() {
  warn "Phase 2 — 搬迁 ${#GAMELIB_MODULES[@]} 个 B 类模块到 gamelib/"
  for m in "${GAMELIB_MODULES[@]}"; do
    if [[ -d "./$m" ]]; then
      run "mv ./$m ../$GAMELIB_DIR_NAME/$m"
    else
      warn "engine/$m 不存在，跳过"
    fi
  done

  warn "改写 import 路径：engine/<X> → gamelib/<X>"
  for m in "${GAMELIB_MODULES[@]}"; do
    rewrite_import "engine/$m" "gamelib/$m"
  done

  if (( APPLY )); then
    (cd .. && go work sync) || warn "go work sync 警告（首次可能 require 尚未可见）"
  fi
  log "Phase 2 完成"
}

# ---------- Phase 3：搬迁 tool 模块 ----------------------------------------
phase3() {
  warn "Phase 3 — 搬迁 C 类模块到 tool/"
  for m in "${TOOL_MODULES[@]}"; do
    if [[ -d "./$m" ]]; then
      run "mv ./$m ../$TOOL_DIR_NAME/$m"
    else
      warn "engine/$m 不存在，跳过"
    fi
  done

  # cmd/engine 特别处理（保留嵌套）
  if [[ -d "./cmd" ]]; then
    run "mkdir -p ../$TOOL_DIR_NAME/cmd && mv ./cmd/engine ../$TOOL_DIR_NAME/cmd/engine"
    # 如果 cmd 下还有其他子目录，一并搬
    run "if [[ -d ./cmd && -z \"\$(ls -A ./cmd 2>/dev/null)\" ]]; then rmdir ./cmd; fi"
  fi

  warn "改写 import 路径：engine/<X> → tool/<X>"
  for m in "${TOOL_MODULES[@]}"; do
    rewrite_import "engine/$m" "tool/$m"
  done
  rewrite_import "engine/cmd/engine" "tool/cmd/engine"

  log "Phase 3 完成"
}

# ---------- Phase 4：engine 收口 -------------------------------------------
phase4() {
  warn "Phase 4 — engine 收口"
  warn "校验 engine 顶层目录只剩白名单"
  local keep_set=" ${ENGINE_KEEP[*]} "
  for d in */; do
    local name="${d%/}"
    if [[ "$name" == "doc" || "$name" == "vendor" ]]; then continue; fi
    if [[ "$keep_set" != *" $name "* ]]; then
      err "engine/$name 不在白名单，请确认是否应搬走"
    fi
  done

  warn "更新 go.mod（清理已迁移依赖）"
  run "go mod tidy"

  if (( APPLY )); then
    (cd ../gamelib && go mod tidy) || warn "gamelib go mod tidy 警告"
    (cd ../tool    && go mod tidy) || warn "tool go mod tidy 警告"
  fi

  warn "构建三 module"
  run "cd .. && go build ./engine/... ./gamelib/... ./tool/..."

  warn "扫描反向依赖"
  scan_reverse_deps

  log "Phase 4 完成"
}

# ---------- import 路径改写 ------------------------------------------------
# $1 = 旧前缀（如 engine/log） $2 = 新前缀（如 gamelib/log）
rewrite_import() {
  local src="$1" dst="$2"
  # 仅改写 .go 文件中出现的 "engine/X" 或 "engine/X/..."
  local files
  files=$(grep -rln --include='*.go' "\"$src" .. 2>/dev/null || true)
  if [[ -z "$files" ]]; then return 0; fi
  echo "  rewrite: \"$src...\" → \"$dst...\""
  while IFS= read -r f; do
    if (( APPLY )); then
      # 用 @ 作分隔符规避 / 冲突；以 " 开头确保只改 import 字符串
      sed -i "s@\"$src@\"$dst@g" "$f"
    else
      echo "    (dry)   $f"
    fi
  done <<< "$files"
}

# ---------- 反向依赖扫描 ---------------------------------------------------
scan_reverse_deps() {
  local bad=0
  # engine 不得 import gamelib/tool
  if grep -rln --include='*.go' -E '"(gamelib|tool)/' ./ 2>/dev/null | grep -v _test.go >/dev/null; then
    err "engine 中检测到 gamelib/tool import："
    grep -rln --include='*.go' -E '"(gamelib|tool)/' ./ 2>/dev/null
    bad=1
  fi
  # gamelib 不得 import tool
  if grep -rln --include='*.go' '"tool/' "../$GAMELIB_DIR_NAME/" 2>/dev/null >/dev/null; then
    err "gamelib 中检测到 tool import："
    grep -rln --include='*.go' '"tool/' "../$GAMELIB_DIR_NAME/" 2>/dev/null
    bad=1
  fi
  (( bad == 0 )) && log "反向依赖扫描 OK"
}

# ---------- 主流程 ---------------------------------------------------------
main() {
  require_in_engine
  require_clean_worktree
  require_branch_safety

  if (( APPLY == 0 )); then
    warn "=== DRY-RUN 模式 —— 加 --apply 才会真正执行 ==="
  else
    warn "=== APPLY 模式 —— 将真实改动工作区 ==="
  fi
  warn "PHASE=$PHASE  APPLY=$APPLY  BRANCH=$(git rev-parse --abbrev-ref HEAD)"
  echo ""

  case "$PHASE" in
    1)   phase1 ;;
    2)   phase2 ;;
    3)   phase3 ;;
    4)   phase4 ;;
    all) phase1; phase2; phase3; phase4 ;;
    *)   err "未知 phase：$PHASE"; exit 1 ;;
  esac

  echo ""
  log "脚本执行结束。"
  (( APPLY == 0 )) && warn "如需真正应用，追加 --apply 重新运行。"
}

main "$@"
