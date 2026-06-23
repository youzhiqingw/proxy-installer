# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

---

## Project Overview

Proxy Installer is a Windows desktop application built with Wails (Go + React) for deploying proxy nodes on Linux VPS servers. It provides a GUI for SSH-based VPS management and proxy protocol deployment (Hysteria2, VLESS Reality, VMess, Trojan, Shadowsocks).

**Tech Stack** (FROZEN - do not propose changes):
- **Frontend**: React 19 + Wails v2, `App.jsx` shell + `frontend/src/components/*.jsx` page components + `frontend/src/components/ui/*.jsx` shared UI + `frontend/src/utils/*.js` helpers + `frontend/src/hooks/*.js` backend wrappers, useState + Wails Events, inline styles
- **Backend**: Go 1.23.0, Wails v2, single monolithic app
- **Configuration**: JSON file storage (no database)
- **Deployment**: sing-box command-line
- **Connection**: golang.org/x/crypto/ssh SSH client library
- **UI Icons**: lucide-react v1.17.0
- **QR Code**: qrcode v1.5.4
- **Encryption**: AES-GCM (`crypto/aes`, `crypto/cipher`, `crypto/rand`) — `internal/vault/`

---

## Build Commands

```bash
# Development with hot reload
wails dev

# Production build (creates executable in build/bin/)
wails build

# Install frontend dependencies
cd frontend && npm install

# Run tests
go test ./...

# Run specific test
go test -run TestIPv6HostFormatting ./...
```

---

## Architecture

**Backend (Go)**:
- `main.go`: Wails app initialization, system tray
- `app.go`: Core logic - SSH, VPS inspection, proxy deployment, speed tests, IP quality checks, password encryption, log sanitization
- `internal/vault/`: AES-GCM encryption module for credential storage
- Key structs: `App`, `SSHProfile`, `DeployConfig`, `AppState`
- All backend methods bound to frontend via Wails (e.g., `LoadAppState`, `TestConnection`, `StartDeploy`)

**Frontend (React)**:
- `frontend/src/App.jsx`: Main app shell with tab routing
- `frontend/src/components/*.jsx`: Page components (`Dashboard`, `CostCenter`, `Configs`, `Deploy`, `SpeedCenter`, `Maintenance`, `Progress`, `Result`)
- `frontend/src/components/ui/*.jsx`: Shared UI components (`Field`, `StatCard`, `PanelTitle`, `Icons`)
- `frontend/src/utils/*.js`: Formatting, constants, protocol definitions
- `frontend/src/hooks/*.js`: Backend call wrappers
- `frontend/src/main.jsx`: React entry point
- `frontend/wailsjs/`: Auto-generated Wails bindings for Go backend calls

**Data Storage**:
- Local app data: `%LOCALAPPDATA%\proxy-installer\`
- State file: `data/state.json` (SSH profiles, deploy config, history)
- Auto key file: `.autokey` (AES-GCM encryption key, 0600 permissions)
- App data root: `proxyDataRoot()` in `app.go` (returns `%LOCALAPPDATA%\proxy-installer` on Windows)
- Password encryption: AES-GCM with auto key, stored in `PasswordEncrypted` field

**Build Configuration**:
- `wails.json`: Build config with frontend dev/build commands
- Output: `build/bin/proxy-installer.exe`
- Webview user data: `%LOCALAPPDATA%\proxy-installer\webview\`

**Tabs** (7 total):
| ID | Label | Description |
|----|-------|-------------|
| `dashboard` | 仪表盘 | 状态总览 |
| `configs` | VPS 管理 | SSH 与体检 |
| `deploy` | 节点部署 | 协议与订阅 |
| `speed` | 测速中心 | 延迟与出口 |
| `maintenance` | 维护清理 | 印记与卸载 |
| `progress` | 进度日志 | 部署事件 |
| `cost` | 成本中心 | 厂商与账单 |
| `result` | 节点信息 | 客户端订阅 |

---

## Critical Constraints

**🔒 CORE PROTECTED FILES** (read-only, minimal changes only):
- `app.go` (root) - SSH connection core, protocol deployment templates, Wails initialization
- `frontend/src/App.jsx` structure - Single-file mode
- `wails.json` - Build config
- `go.mod` / `package.json` - Dependencies (NO new external libraries)

**FORBIDDEN**:
- ❌ Refactoring existing architecture
- ❌ Introducing Redux/Zustand/MobX or any state management library
- ❌ Replacing React 19 or Wails v2
- ❌ Adding databases (SQLite, MySQL, etc.)
- ❌ Modifying sing-box deployment flow
- ❌ Changing subscription URL formats (vmess://, trojan://, etc.)
- ❌ Adding new Go or NPM dependencies

**ALLOWED**:
- ✅ Add new tab/page component in `frontend/src/components/*.jsx`
- ✅ Add new Wails binding methods in `app.go`
- ✅ Add new protocol templates in `app.go`
- ✅ Extend existing struct fields with `omitempty`
- ✅ Add new VPS detection functions
- ✅ Local security fixes only

---

## Development Workflow (MANDATORY)

**Before any development**, you MUST:

1. **Read all agent documents** in `.project-agents/`:
   - `GOAL.md` (highest constraint)
   - `00-scope-guardian.md` (boundary enforcement)
   - `01-security-auditor.md` (security rules)
   - `02-feature-planner.md` (feature evaluation)
   - `03-fullstack-developer.md` (development standards)
   - `04-infrastructure-engineer.md` (protocol/VPS standards)
   - `05-quality-assurance.md` (testing standards)

2. **Execute pre-development checklist** (6 steps):
   - Step 1: Read agent docs, confirm role and boundaries
   - Step 2: Read target files and dependencies
   - Step 3: Conflict detection (naming, interfaces, data flow)
   - Step 4: Baseline protection check
   - Step 5: Code size estimation (files ≤5, new lines ≤500, modified ≤200, deleted ≤50)
   - Step 6: Dependency check (no new dependencies)

3. **Output validation report** after development:
   - Conflict validation (new vs existing)
   - Baseline validation (protected files touched?)
   - Size validation (within thresholds?)
   - Dependency validation (no new deps?)

**If any check fails**: Stop immediately, output report, do NOT proceed.

---

## Conflict Detection Rules

**Naming Conflicts** (must be unique):
- Go function names (package-level)
- Go struct fields (within struct)
- Wails binding methods (within App struct)
- React state names (within component)
- JSON config fields
- Protocol type values
- CSS class names
- Tab IDs

**Interface Conflicts**:
- Parameter types must match between frontend/backend
- Return structures must match
- Wails event names must be unique
- Error formats must follow existing patterns

**Data Flow Conflicts**:
- New config fields must have `omitempty` and default values
- State changes must use Wails bindings
- New protocols must follow sing-box JSON structure

---

## Wails Bindings

Frontend calls Go methods via auto-generated bindings in `frontend/wailsjs/go/main/App.js`:

```javascript
// Available bindings (12 total):
import {
  CheckPorts,
  CleanupSelectedFootprint,
  InspectVPS,
  LoadAppState,
  MeasureLatency,
  RunIPQuality,
  RunNodeSpeedTest,
  RunSpeedTest,
  SaveAppState,
  ScanFootprint,
  StartDeploy,
  TestConnection,
  GetCostV2Instances,
  SaveCostVPSInstance,
  DeleteCostVPSInstance,
  GetCostV2Summary,
  LinkVPSProfile,
} from '../wailsjs/go/main/App';
```

The full list of Wails bindings:

| Method | Parameters | Description |
|--------|-----------|-------------|
| `CheckPorts` | `host, ports` | Check if ports are open |
| `CleanupSelectedFootprint` | `host, items, dryRun` | Clean selected VPS artifacts |
| `InspectVPS` | `host` | Inspect VPS system info |
| `LoadAppState` | - | Load saved app state |
| `MeasureLatency` | `host, port` | Measure connection latency |
| `RunIPQuality` | `host` | Run IP quality check |
| `RunNodeSpeedTest` | `host, duration` | Run node speed test |
| `RunSpeedTest` | `host` | Run VPS exit speed test |
| `SaveAppState` | `state` | Save app state |
| `ScanFootprint` | `host` | Scan VPS for proxy installer artifacts |
| `StartDeploy` | `host, config` | Start proxy deployment |
| `TestConnection` | `host` | Test SSH connection |
| `GetCostV2Instances` | - | Load cost center instances |
| `SaveCostVPSInstance` | `instance` | Save or update a VPS cost instance |
| `DeleteCostVPSInstance` | `id` | Delete a VPS cost instance |
| `GetCostV2Summary` | - | Get cost summary by currency |
| `LinkVPSProfile` | `instanceID, profileID` | Link a cost instance to an SSH profile |

---

## Deployment Flow

1. User adds SSH profile → `SaveAppState`
2. Connect and inspect VPS → `TestConnection` → `InspectVPS`
3. Configure deployment (protocols, ports, token) → `StartDeploy`
4. Deployment emits progress events via Wails EventsOn
5. Result generates subscription URLs for Shadowrocket, Clash Meta, V2rayNG, sing-box

---

## Supported Protocols

| Protocol | Template Location | Subscription Format |
|----------|-------------------|---------------------|
| Hysteria2 | `app.go` | `hysteria2://` |
| Reality | `app.go` | `vless://` |
| TUIC | `app.go` | `tuic://` |
| Trojan | `app.go` | `trojan://` |
| Shadowsocks | `app.go` | `ss://` |
| VMess | `app.go` | `vmess://` |

---

## Supported Remote Systems

Proxy Installer deploys to Linux VPS with systemd. It auto-detects the package manager:

| Package Manager | Common Distributions |
|-----------------|---------------------|
| apt | Debian, Ubuntu |
| dnf | Fedora, Rocky Linux, AlmaLinux, RHEL |
| yum | CentOS, old RHEL variants |
| pacman | Arch Linux |
| zypper | openSUSE |

---

## Code Size Limits

**Hard limits per commit**:
- Files affected: ≤ 5
- Lines added: ≤ 500
- Lines modified: ≤ 200
- Lines deleted: ≤ 50

**If exceeded**: Split into multiple commits or provide justification.

---

## Agent System

This project uses a specialized agent system in `.project-agents/`:
- **GOAL.md**: Highest constraint document, mandatory workflow
- **00-scope-guardian**: Enforces boundaries, blocks violations
- **01-security-auditor**: Security-only reviews (read-only)
- **02-feature-planner**: Feature evaluation (no implementation)
- **03-fullstack-developer**: Go + React development (incremental only)
- **04-infrastructure-engineer**: Protocol deployment + VPS operations
- **05-quality-assurance**: Testing + release review + documentation

**All agents must follow GOAL.md workflow**. Violations will be blocked by scope-guardian.

---

## Key Principles

1. **Incremental only**: Add features, don't refactor
2. **Protect core files**: Minimal changes to protected files
3. **No new dependencies**: Use existing libraries only
4. **Single-file frontend**: Keep `App.jsx` as one file
5. **Conflict prevention**: Check for naming conflicts before adding
6. **Stay within limits**: Respect code size thresholds
7. **Follow GOAL workflow**: Read agents, check conflicts, validate after
8. **Security first**: All user input must be validated, sanitized, and logged safely

---

## Security Fixes (2026-06-14)

All P0/P1/P2 security issues have been fixed:

| # | Issue | Fix | Location |
|---|-------|-----|----------|
| 1 | SSH HostKey validation | `SSHHostKeyStore` known_hosts persistence | `app.go:1040-1066` |
| 2 | Password plaintext storage | AES-GCM encryption via `vault.Vault` | `internal/vault/` + `app.go` |
| 3 | Unsafe random numbers | `crypto/rand.Read()` | `realityKeys()`, `stableUUID()` |
| 4 | Log message leakage | `sanitizeLogMessage()` filters sensitive fields | `app.go:1103-1120` |
| 5 | Input length limits | `safeToken()` 64, `safeName()` 64, `safeDomain()` 253 | `app.go:3728-3774` |
| 6 | Path traversal `..` | `safeLocationPath()` detect and fallback | `app.go:2201-2223` |
| 7 | SHA256 checksum missing | `verifySHASums()` + remote script validation | `app.go:3178-3234`, `1963-1972` |

---

## Quality Improvements (branch: refactor/quality-improvements)

Batch 1–3 completed (6 of 19 tasks). Branch `refactor/quality-improvements` diverged from `main` at `4505767`.

| Batch | Commit | Tasks | Summary |
|-------|--------|-------|---------|
| 1 | `e8ef9b6` | T-01, T-03 | Remove commented go.mod directive; centralize 30+ hardcoded literals into `const` block |
| 2 | `d6759d5` | T-19, T-02 | Structured logging via `internal/logger` (slog, daily rotation, 30-day retention); SSH HostKey TOFU user confirmation (`ErrNewHostKey` → `AcceptHostKey` → `HostKeyDialog`) |
| 3 | `f9143fb` | T-06, T-07 | PBKDF2 key derivation (SHA-256, 600k iterations) replacing legacy XOR KDF; Windows Credential Manager via `go-keyring` with auto-migration from `.autokey` |

### Key changes

**Structured Logging** (`internal/logger/`):
- Package: `proxy-installer/internal/logger` — uses Go `log/slog`
- Output: `%LOCALAPPDATA%/proxy-installer/logs/YYYY-MM-DD.log` + stdout
- Retention: 30-day automatic cleanup via `cleanOldLogs()`
- Integration: `logger.Init(root)` in `startup()`, `logger.Info/Warn/Error/Debug` across all public methods

**SSH HostKey Confirmation**:
- Backend: `hostKeyCallback()` returns `&ErrNewHostKey{}` for unknown keys (TOFU model)
- New method: `AcceptHostKey(host string, port int) error` — stores key after user approval
- Frontend: `callSSH()` wrapper detects `HOSTKEY_CONFIRM:` sentinel → shows `HostKeyDialog` → retry on accept
- UX: Shield icon, host:port, keyType, fingerprint display; "取消" and "信任并继续" buttons

**PBKDF2 Migration** (`internal/vault/encrypt.go`):
- New: `deriveKeyPBKDF2()` using `golang.org/x/crypto/pbkdf2` (SHA-256, 600k iterations)
- Legacy: `deriveKeyLegacy()` retained for backward-compatible decryption
- VaultData: `Version` field — `"v2"` = PBKDF2, empty = legacy XOR KDF
- Auto-migration: decrypt old format → re-encrypt as v2 on next save

**OS Keyring** (`internal/vault/keyring.go`):
- Library: `github.com/zalando/go-keyring` (Windows Credential Manager)
- Priority: keyring → `.autokey` file → generate new
- Migration: `MigrateAutoKeyToKeyring()` copies file-based key to OS keyring on first load
- Redundancy: new keys saved to both keyring and `.autokey` file

### New dependencies added (branch only)

| Package | Purpose |
|---------|---------|
| `golang.org/x/crypto/pbkdf2` | Standard PBKDF2 key derivation |
| `github.com/zalando/go-keyring` | Cross-platform OS credential storage |

### Remaining tasks (batch 4–12)

T-04, T-05, T-08 through T-18 — see task checklist document for full plan.

---

## Build Output

- Development: `wails dev` runs frontend dev server + Go backend
- Production: `wails build` outputs to `build/bin/proxy-installer.exe`
- Windows installer: Uses NSIS scripts in `build/windows/installer/`
