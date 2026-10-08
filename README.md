# unlock-keepawake

A tiny Windows tray utility that keeps your session awake by injecting an
**F15** keypress on a fixed interval. It exists to stop the screen from locking
or the machine from going to sleep during long-running work — a software
"mouse jiggler".

> Sending *any* keyboard/pointer input resets the Windows idle timer, so a
> periodic F15 is enough to defeat the screensaver / lock screen. F15 is used
> because it has no effect in virtually any application.

Built with Go and [`github.com/lxn/walk`](https://github.com/lxn/walk).

## Features

- Runs in the system tray (closing the window only hides it).
- Configurable interval (1 / 2 / 4 / 5 / 10 / 15 / 30 minutes).
- Optional **time window** — only stay awake between e.g. `09:00` and `17:30`,
  including windows that cross midnight.
- Auto-start into the tray.
- Config persisted to `.unlock.json` next to the executable.

## Usage

1. Download `unlock-keepawake.exe` from the [Releases](../../releases) page.
2. Run it. Pick an interval and press **开始 / Start**.
3. The window minimizes to the tray; right-click the tray icon to show it again
   or to quit.

### Configuration file

`.unlock.json` is created next to the executable:

```json
{
  "interval_minutes": 4,
  "use_time_window": false,
  "start_time": "09:00",
  "end_time": "17:30",
  "auto_start": false
}
```

| Field | Meaning |
|---|---|
| `interval_minutes` | How often to send F15. |
| `use_time_window` | Restrict activity to the window below. |
| `start_time` / `end_time` | `HH:MM`, may cross midnight. |
| `auto_start` | Start running immediately on launch (into the tray). |

> **Note:** the config is written next to the `.exe`. If you install the app
> under `C:\Program Files\`, a standard user cannot write there. Either keep it
> in a user-writable folder, or run it as admin.

## Build from source

Requirements: Go 1.22+.

```bash
# regenerate the Windows resource object (icon + manifest + version info)
go install github.com/tc-hib/go-winres@latest
go-winres make --in winres/winres.json --arch amd64 --out rsrc

# cross-compile
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -H windowsgui" -o unlock-keepawake.exe .
```

The generated `rsrc_windows_amd64.syso` is auto-linked by the Go toolchain and
embeds the application icon, manifest and version information.

## Code signing

Release binaries are built in CI and signed through
[SignPath.io](https://signpath.io) / the
[SignPath Foundation](https://signpath.org). See
[`.github/workflows/release.yml`](.github/workflows/release.yml).

## License

[MIT](LICENSE)

---

## 中文说明

`unlock-keepawake` 是一个 Windows 托盘小工具，按固定间隔（默认 4 分钟）模拟按一次 **F15** 键，
用来阻止系统空闲锁屏或休眠，即常见的“防锁屏 / 保活”工具。

- 关闭窗口只会最小化到托盘；右键托盘图标可显示窗口或退出。
- 支持设置时间窗口（如仅在 09:00–17:30 内生效，可跨午夜）。
- 配置保存在 exe 同目录的 `.unlock.json`。
- 安装在 `C:\Program Files\` 下时普通用户无写权限，建议放在用户可写目录，
  或将配置改到 `%APPDATA%`。
