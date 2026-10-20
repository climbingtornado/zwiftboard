# macOS CLI

The macOS port uses CoreBluetooth through `tinygo.org/x/bluetooth` and Quartz
keyboard events. Build on macOS with the Go version in `go.mod`, Xcode Command
Line Tools (`xcode-select --install`), and `just`:

```sh
just ci-macos
./dist/zwiftboard
```

`CGO_ENABLED=1` is required. `just build-macos` builds the host architecture;
CI runs native Apple Silicon and Intel jobs. macOS release downloads, app
bundles, signing/notarization, automatic app launching, and window focusing
are not included in this change. Windows release assets and Scoop are unchanged.

## First run

1. Close Zwift and Companion; the controllers accept one BLE connection each.
2. Turn Bluetooth on and wake **both** Click V2 pods.
3. Run the built binary from a stable path, rather than `go run` (which creates
   a temporary executable). Allow Bluetooth access when macOS requests it.
4. In **System Settings > Privacy & Security > Accessibility**, allow the
   terminal application or `zwiftboard` as identified by macOS. Add the built
   executable using the `+` picker if needed, then restart the terminal/program.
5. Open TextEdit, create an empty document, and focus it. The initial `keyboard`
   profile maps PLUS=p, A=a, B=b, Y=y, Z=z. Both pods stay connected; only the
   right pod supplies these mapped buttons.

The config is created at `~/Library/Application Support/zwiftboard/config.yml`
(unless `LOCALAPPDATA` is explicitly set, preserving the existing config-path
behavior). New macOS configs select `keyboard`; existing or migrated configs
are never rewritten. Pass `-config /path/to/config.yml` to use an existing file,
`-p keyboard` to select that profile, or `-plain -v` for diagnostic logs.
An older config may not contain the new profile: add it from `config.yaml` or
clear focus/launch settings on a profile you already use.

The executable embeds `NSBluetoothAlwaysUsageDescription`. Accessibility is
checked without repeatedly prompting; controller logging remains available
when injection is denied. The status screen shows an input-blocked warning.
No Input Monitoring permission is requested by this implementation.

## Mappings and unsupported options

Config tokens and Windows virtual-key identifiers stay compatible. On macOS,
letters, digits, punctuation, navigation, F1–F12, and modifiers map to **US
physical keyboard positions**. Text on other layouts can differ. `ctrl` means
Control, not Command; `alt` means Option. `insert` has no supported mapping.
Physical modifiers are combined with controller modifiers. Caps Lock toggles
without repeat; other modifiers do not repeat. Ordinary keys repeat after
250 ms, then every 33 ms until release.

Set `focusProgramNamePrefixOnClick: null` to send input to the foreground app.
A nonempty focus target **blocks injection** on macOS; it is never silently
ignored. `startProgram` reports that launching is unsupported; open the target
application yourself. Keep it in focus while using the controller.

## Testing without a controller

```sh
just ci-macos
python3 scripts/check-macos-metadata.py dist/zwiftboard
./dist/zwiftboard -version
./dist/zwiftboard -demo
```

Tests inject event sinks, clocks, connection state and write functions. They do
not post real keyboard events or explicitly enable/scan Bluetooth. The pinned
TinyGo dependency creates CoreBluetooth managers at import time, including in
tests and `-version`/`-demo`; macOS may therefore show a Bluetooth prompt even
though those modes perform no application-level scanning. No Accessibility
prompt is requested. This dependency behavior is not patched or vendored here.

TinyGo's macOS `Scan` also does not reliably report radio-off/permission errors;
the Bluetooth indicator reflects scan-call health and can remain on while the
radio is off. Startup errors include permission guidance. Sessions check native
connection state before writes, because successfully queuing a write without
response is not evidence that a controller is connected. If recovery does not
occur after re-enabling Bluetooth, restart and capture `-plain -v` logs.

## Physical validation checklist

Automated checks cannot establish controller firmware behavior, permission
ownership, application receipt, or game key-state polling. Record macOS version,
CPU architecture, pod firmware if known, and target application with results.

- Deny then grant Bluetooth; verify startup guidance and recovery.
- Deny then grant Accessibility; verify input stays blocked until authorized.
- Connect both pods; press all five mapped buttons and check for duplicate input.
- Tap, hold and release a letter in TextEdit; verify repeat stops on release.
- Test arrows and game polling, F keys, Control/Option/Shift, and Caps Lock;
  include a physical modifier held while pressing a controller button.
- Disconnect one pod during a hold, then both; ensure no permanently stuck key.
- Reload or switch profiles during a hold, including a blocked focus profile.
- Quit using `q`, Ctrl+C, and SIGTERM while holding a button; verify cleanup.
- Leave both pods idle for at least ten minutes; verify the next press works.
- Switch Bluetooth off/on and sleep/wake the pods; verify scan-gated reconnect.

An abrupt kill (`SIGKILL`), OS crash, or revoked Accessibility permission can
prevent cleanup events from reaching macOS. Report physical results separately
from build/unit-test results; Intel runtime requires an Intel machine.
