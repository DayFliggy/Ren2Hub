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
```bash
cd electron
bun install --frozen-lockfile
```

前端与 Electron 统一使用 Bun 1.4.0，各自维护 bun.lock；构建不自动发布。

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
