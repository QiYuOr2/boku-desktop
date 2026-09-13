# Desktop 客户端

Electron + Vue 3。工具链统一走 Vite+，规范见 `docs/spec/desktop-toolchain.md`。

## 包

```text
shared/     Domain 契约 + IPC 契约（node / browser 通用）
main/       Electron main（node target，CJS 产物）
preload/    contextBridge 白名单（node target，CJS 产物）
renderer/   Vue 3 应用（browser target，ESM 产物）
scripts/    dev 编排
```

依赖方向：`renderer → shared`、`main → shared`、`preload → shared`，不得反向。

## 命令

在仓库根执行：

```bash
vp install
vp run dev      # renderer + main/preload watch + Electron
vp run build    # 构建三个产物
vp run test
vp run dist     # electron-builder 打包
```

单包操作使用 `-C`，例如 `vp -C apps/desktop/renderer dev`。

## 网络环境

Electron 与 electron-builder 的二进制从 GitHub 下载，且不读取 `HTTP_PROXY`。
在国内网络下用镜像变量：

```powershell
$env:ELECTRON_MIRROR = "https://npmmirror.com/mirrors/electron/"
$env:ELECTRON_BUILDER_BINARIES_MIRROR = "https://npmmirror.com/mirrors/electron-builder-binaries/"
```

- `vp install` 后如果 `apps/desktop/node_modules/electron/dist` 不存在，说明 Electron 运行时没下载：
  设置 `ELECTRON_MIRROR` 后执行 `vp rebuild electron`，或在 electron 包目录内执行 `node install.js`。
- `vp run dist` 首次打包会下载 nsis / winCodeSign，需要 `ELECTRON_BUILDER_BINARIES_MIRROR`。
