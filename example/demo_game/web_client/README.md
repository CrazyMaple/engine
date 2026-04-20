# Engine Demo Game — Web Client

Guess Number 在线竞猜游戏的 Web 前端示例,展示 Engine 生成的 TypeScript SDK 用法。

## 目录

- `sdk.ts` — 引擎 codegen 产出的强类型 SDK(GameClient、PushSubscriber、MessageMap、Codec)
- `app.ts` — 使用 SDK 实现的应用逻辑(连接、消息路由、UI 状态)
- `index.html` — 单文件演示页(内联 JS,零构建即可运行)
- `dist/` — `npm run build` 产出目录(bundled JS)

## 两种运行方式

### 方式一:零构建(推荐快速体验)

直接打开 `index.html`。内联 JS 已内嵌精简版 GameClient,无需 npm 依赖。

```bash
go run example/demo_game/main.go          # 另起终端启动服务端
open example/demo_game/web_client/index.html
```

### 方式二:完整 TypeScript 工程(推荐集成到自有项目)

```bash
cd example/demo_game/web_client

# 安装依赖
npm install

# 类型检查
npm run typecheck

# 构建(输出到 dist/app.js)
npm run build

# 监听构建
npm run watch

# 最小化构建
npm run build:min

# 启动静态服务器(浏览器访问 http://localhost:5173)
npm run serve
```

构建后可将 `dist/app.js` 通过 `<script src="dist/app.js"></script>` 注入到自有 HTML 页面中。

## 生成 SDK

`sdk.ts` 由 codegen 自动生成,不应手动编辑。重生成命令:

```bash
# 在仓库根目录执行
go run codegen/cmd/msggen/main.go
# 或
engine gen -input=./example/demo_game/messages.go -ts-rpc=example/demo_game/web_client/sdk.ts
```

## 消息协议

参见 `sdk.ts` 中的 `MessageMap`。所有 C2S/S2C 均为 JSON 格式,handshake 阶段使用 `__handshake__` 内部类型。

## 依赖

- [esbuild](https://esbuild.github.io/) — 打包器(零配置、极速)
- [typescript](https://www.typescriptlang.org/) — 仅用于 `tsc --noEmit` 类型检查

## 许可

与主仓库一致。
