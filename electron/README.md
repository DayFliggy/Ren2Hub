# New API Electron Desktop App

This directory contains the Electron wrapper for Ren2Hub, retaining the New API desktop package identity. It starts the Go gateway in production, loads the Vue application from the local gateway, and provides a system tray window for Windows, macOS, and Linux. Dependencies and packaging targets are defined in [package.json](package.json).

## Prerequisites

### 1. Go Binary (Production)
Production packaging requires the compiled Go binary. Development mode uses a separately started backend. For production, you have two options:

**Option A: Use existing binary (without Go installed)**
```bash
# From electron/, use a binary built from this checkout for the target platform
cp ../new-api-macos ../new-api
```

**Option B: Build from source (requires Go)**
From the repository root, run `make build-web` followed by `go build -o new-api .`
(`new-api.exe` on Windows). `make build-web` builds Vue and validates and copies
its production assets into `frontend/embed-dist` before Go embeds them.

### 2. Electron Dependencies
Use Bun 1.4.0 and Node.js 22.12.0 or newer; the dependency and download checks
were run with Node.js 24.21.0.

```bash
cd electron
bun install --frozen-lockfile
```

前端与 Electron 统一使用 Bun 1.4.0，各自维护 bun.lock；构建不自动发布。

2026-10-13 更新至 Electron 41.10.6，并定向吸收兼容的安全补丁。Electron Builder 26.15.3 的 `@electron/get` 3.1.0 通过可选 `global-agent` 引入 `roarr → sprintf-js`；本项目将 `global-agent` 固定覆盖为官方 4.1.3。[官方发布记录](https://github.com/gajus/global-agent/releases)说明 4.1.0 已移除 `roarr`，4.1.3 修正了发布产物。4.1.3 保留旧下载器调用的 CommonJS `bootstrap()` API 和代理环境变量，实际代理下载及校验失败检查通过。锁文件和正常安装依赖中不再包含 `roarr` 或 `sprintf-js`，`bun audit --json` 无已知漏洞。不禁用 optional 依赖，保留 Electron 下载代理和跨平台打包工具。

此次下载验证包含 HTTP 代理、HTTPS CONNECT、代理绕过和校验失败。Linux 目录打包只能检查打包流程与资源完整性；Windows/macOS 安装包和任一桌面系统的图形启动与托盘操作尚未验证。后续升级 Builder 时应重新核对下载器依赖与代理检查，移除不再需要的覆盖。完整同步状态见[同步账本](../docs/upstream/SYNC_LEDGER.md#依赖与构建)。

### Download proxies

构建时有两个下载器：Electron 41 使用 `@electron/get` 5，Builder 26 使用 3。需要代理时，设置 `ELECTRON_GET_USE_PROXY=1`，同时配置标准代理变量和旧下载器的命名空间：

```bash
export ELECTRON_GET_USE_PROXY=1
export HTTP_PROXY=http://127.0.0.1:7890
export HTTPS_PROXY="$HTTP_PROXY"
export NO_PROXY=localhost,127.0.0.1
export GLOBAL_AGENT_HTTP_PROXY="$HTTP_PROXY"
export GLOBAL_AGENT_HTTPS_PROXY="$HTTPS_PROXY"
export GLOBAL_AGENT_NO_PROXY="$NO_PROXY"
bun install --frozen-lockfile
bun run build -- --publish never
```

这些变量用于开发和构建过程的下载。普通直连构建无需设置。代理启用方式见 [get 3 文档](https://github.com/electron/get/blob/v3.1.0/README.md#proxies)和 [get 5 发布说明](https://github.com/electron/get/releases/tag/v5.0.0)。现有托盘图标是版本控制中的 PNG 文件，打包不需要 `canvas` 或图标生成脚本。

### Download diagnostics

`ROARR_LOG=true` 在 global-agent 4 中已失效。[官方变更](https://github.com/gajus/global-agent/releases/tag/v4.1.0)移除了 roarr，改为显式传入[自定义 logger](https://github.com/gajus/global-agent/blob/v4.1.3/README.md#custom-logger)。get 3 和 Builder 不自动配置该 logger；get 5 使用 Undici，也不消费它。`DEBUG=@electron/get:*` 只提供下载器层日志，并非原代理日志的等价替代；它会输出完整下载 URL 和选项，不适合直接收集含凭据的环境日志。

在 `electron/` 下沿用上面的代理变量执行以下诊断。它通过当前安装的 get 3、get 5 和 Builder 包装入口，各自用临时空缓存下载当前 Electron 的小型 `SHASUMS256.txt`，不打包或启动应用。logger 和代理初始化 debug 只计数，错误只输出固定类别，不打印代理 URL、凭据、下载选项或异常详情；代理模块初始化失败时立即结束，避免继续直连。

```bash
node <<'NODE'
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { createRequire } = require('node:module');

function failureKind(error) {
  for (let e = error; e; e = e.cause) {
    if (['CERT_HAS_EXPIRED', 'DEPTH_ZERO_SELF_SIGNED_CERT', 'SELF_SIGNED_CERT_IN_CHAIN',
      'UNABLE_TO_VERIFY_LEAF_SIGNATURE', 'ERR_TLS_CERT_ALTNAME_INVALID'].includes(e.code)) return 'TLS';
    if (['ENOTFOUND', 'ECONNREFUSED', 'ETIMEDOUT', 'ECONNRESET'].includes(e.code)) return 'NETWORK';
    const status = e.response?.statusCode ?? e.response?.status;
    if (Number.isInteger(status) && status >= 400 && status <= 599) return `HTTP_${status}`;
  }
  return 'DOWNLOAD_FAILED';
}

(async () => {
  process.env.DEBUG = '';
  process.env.ELECTRON_GET_NO_PROGRESS = '1';
  const debug = require('debug');
  let proxyInitErrors = 0;
  debug.log = () => { proxyInitErrors++; };
  debug.enable('@electron/get:proxy');
  const proxyEvents = { debug: 0, error: 0, info: 0, trace: 0, warn: 0 };
  const logger = { child() { return this; } };
  for (const level of Object.keys(proxyEvents)) logger[level] = () => { proxyEvents[level]++; };
  if (process.env.ELECTRON_GET_USE_PROXY) require('global-agent').bootstrap({ logger });
  const get3 = createRequire(require.resolve('app-builder-lib/package.json'))('@electron/get');
  const get5 = await import('@electron/get');
  const builder = require('app-builder-lib/out/util/electronGet');
  const builderLog = require('builder-util').log;
  for (const level of ['debug', 'info', 'warn', 'error']) builderLog[level] = () => {};
  if (proxyInitErrors) {
    console.log(JSON.stringify({ result: 'PROXY_INIT_FAILED', proxyInitErrors }));
    process.exitCode = 1;
    return;
  }
  const version = require('electron/package.json').version;
  const cache = fs.mkdtempSync(path.join(os.tmpdir(), 'electron-diagnostic-'));
  try {
    for (const entry of ['get3', 'get5', 'builder']) {
      const args = { version, artifactName: 'SHASUMS256.txt', isGeneric: true,
        force: true, cacheRoot: path.join(cache, entry) };
      try {
        const file = entry === 'builder'
          ? await builder.downloadElectronArtifactZip({ version, artifactName: args.artifactName,
            platformName: process.platform, arch: process.arch, cacheDir: args.cacheRoot,
            electronDownload: { isGeneric: true, force: true,
              downloadOptions: { quiet: true, retry: 0, timeout: { request: 20000 } } } })
          : await (entry === 'get3' ? get3 : get5).downloadArtifact({ ...args,
            downloadOptions: entry === 'get3'
              ? { quiet: true, retry: 0, timeout: { request: 20000 } }
              : { quiet: true, signal: AbortSignal.timeout(20000) } });
        const found = fs.readFileSync(file, 'utf8').includes(`electron-v${version}-linux-x64.zip`);
        console.log(JSON.stringify({ entry, result: found ? 'OK' : 'MANIFEST_ENTRY_MISSING' }));
        if (!found) process.exitCode = 1;
      } catch (error) {
        console.log(JSON.stringify({ entry, result: failureKind(error) }));
        process.exitCode = 1;
      }
    }
  } finally {
    fs.rmSync(cache, { recursive: true, force: true });
  }
  console.log(JSON.stringify({ proxyRequested: Boolean(process.env.ELECTRON_GET_USE_PROXY),
    proxyInitErrors, globalAgentEvents: proxyEvents }));
})().catch(() => { console.error('DIAGNOSTIC_FAILED'); process.exitCode = 1; });
NODE
```

三个 `OK` 仅说明清单下载成功并包含当前版本条目；事件计数只属于 get 3/Builder 的 global-agent，不表示 get 5 的代理请求数。确认实际经过代理仍需在自己的代理端核对连接计数。此命令沿用镜像与代理配置，不能验证安装包 SHA256、所有 NO_PROXY 规则、代理认证兼容性、GUI、托盘或各平台安装包。请勿用关闭 TLS 或 checksum 校验来处理失败；在本机根据固定错误类别继续检查配置，分享上述摘要即可。

## Development

Start the backend, the frontend, and Electron in separate terminals:
```bash
# Repository root
go run .

# Repository root
make dev-web

# electron/
bun run dev-app
```

Development mode does not start the Go process or choose a database path. Start `go run .` (or the dev Docker backend) separately, then start Vite with `make dev-web`; Electron checks `http://127.0.0.1:5175/` and loads it. The backend therefore uses the database path configured by that separately started process. Electron opens a window with DevTools enabled and creates a system tray icon (menu bar on macOS).

`bun run dev-app` 使用 `cross-env` 设置 `NODE_ENV=development`，主进程依赖该值选择开发模式。曾检查直接使用 Bun 的内联环境变量赋值；Bun 1.4.0 在 Linux 上强制 Bun Shell、继承 `NODE_ENV=production` 时未覆盖该值，因此保留 `cross-env` 和原开发命令。恢复后的入口在默认 Shell 与 Bun Shell 下均通过变量覆盖检查。这是开发入口检查，Windows/macOS 桌面启动仍未验收。

## Building for Production

### Quick Build
```bash
# From electron/, build the frontend, Go binary, and desktop package
./build.sh

# Or package an existing binary for the current platform
bun run build

# Platform-specific builds
bun run build:mac    # Creates .dmg and .zip
bun run build:win    # Creates .exe installer
bun run build:linux  # Creates .AppImage and .deb
```

### Build Output

- Built applications are in `electron/dist/`
- macOS: `.dmg` (installer) and `.zip` (portable)
- Windows: `.exe` (installer) and portable exe
- Linux: `.AppImage` and `.deb`

The packaging scripts define all three platforms. The current [Electron build workflow](../.github/workflows/electron-build.yml) runs on Windows only; macOS and Linux packages require a separate local or CI build.

## Configuration

### Port
Default port is 3000. To change, edit `main.js`:
```javascript
const PORT = 3000; // Change to desired port
```

### Database Location

- **Development**: selected by the separately started Go process. Electron development mode does not set `SQLITE_PATH`.
- **Production**: Electron sets `SQLITE_PATH` to `path.join(app.getPath('userData'), 'data', 'new-api.db')`. The per-user directory is determined by Electron and the packaged application identity; the process logs the resolved location on startup.

Production packaging includes a platform-specific Go binary under `bin/`; the binary must already contain the Vue assets prepared in `frontend/embed-dist`.
