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
   If approving the executable alone leaves input blocked, also check the
   terminal application used to launch it. In local testing, enabling Terminal
   and restarting the executable resolved this case.
5. Open TextEdit, create an empty document, and focus it. The initial `keyboard`
   profile starts in single mode. Right buttons map to p, a, b, y, z.
   Press Ctrl+G in the status screen for double mode. Left buttons then map
   LEFT=l, UP=u, RIGHT=r, DOWN=d, MIN=m.

The config is created at `~/Library/Application Support/zwiftboard/config.yml`
(unless `LOCALAPPDATA` is explicitly set, preserving the existing config-path
behavior). New and recovered macOS configs select `keyboard`. All bundled profiles have
focus and launch settings cleared on macOS. Existing or migrated configs remain
unchanged on startup. Switching profiles or modes saves that selection in the config.
Accepted recovery replaces the config after making a backup. Pass `-config /path/to/config.yml` to use an existing file,
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
250 ms, then every 33 ms until release. These are repeated key-down events;
the receiving application decides whether they repeat an action. Text input
may also use macOS's accent menu instead of inserting repeated letters.

Set `focusProgramNamePrefixOnClick: null` to send input to the foreground app.
A nonempty focus target **blocks injection** on macOS; it is never silently
ignored. `startProgram` reports that launching is unsupported; open the target
application yourself. Keep it in focus while using the controller.

## MyWhoosh

Reuse the existing `mywhoosh` bindings with both
`focusProgramNamePrefixOnClick: null` and `startProgram: null`. Connect the
Click pods to zwiftboard and the trainer to MyWhoosh. Open MyWhoosh manually
and keep it focused during the ride.

Single mode uses PLUS=I and B=K. Double mode uses PLUS=K, MIN=I, and B=Enter. A/Z and
LEFT/RIGHT send steering arrows. Y sends U for the menu. Verify gear direction
in your installed MyWhoosh version; these differ from the October test mapping.
Holding a shift
button changed gear once in earlier local testing.
Steering worked with MyWhoosh focused; no shift-specific repeated taps are
emulated.

## Controller modes and left unlock

New configs start in single mode. Later starts restore the saved profile and mode.
Older configs without `defaultMode` start in double mode. Press Ctrl+G to switch modes.
Ctrl+M remains an alias in terminals that report it separately. Apple Terminal
sends Ctrl+M as Enter, so use Ctrl+G there. Enter does not switch modes.
Windows and Linux retain Ctrl+M; Ctrl+G is specific to macOS.
Single mode uses right-controller buttons only. Both pods remain connected in either mode.
Mode changes release held keys before installing the other mapping.

The left controller requires a valid, time-limited unlock. Press Ctrl+Z in the
status screen to sign into Zwift. Sign-in is optional for right-controller use.
Successful sign-in restarts the left session to obtain a server-signed unlock.
A network failure uses upstream's fallback acknowledgment, which cannot renew
an expired unlock. The right session continues operating.

The upstream token store saves `zwift-token.json` beside `config.yml` with
owner-only permissions. It stores session tokens, not your password. Do not
share this file. Keychain storage is outside this change.

Use Ctrl+R to reload a profile, Ctrl+E to edit its config, and Ctrl+C to quit.
The old Q quit shortcut no longer applies. A broken config opens a recovery
dialog. Accepted recovery backs up the file and restores macOS defaults.

## Physical results before the dual-controller merge (2026-10-21)

User-observed results on Apple Silicon with a Click V2 pair:

- Both pods connected; all five mapped letters and simultaneous input worked
  in TextEdit. The initial P mapping passed hold/repeat/release testing.
- MyWhoosh shifting, left/right steering, and menu toggling worked.
- Input remained usable after ten minutes idle and after reconnecting.
- Turning Bluetooth off during a held right arrow stopped cursor movement
  when the disconnect was detected. After Bluetooth was restored and the pods
  reconnected, movement resumed while the button was still physically held.

Quit and profile reload/switch during a hold, modifier combinations, and
Bluetooth permission denial/recovery remain untested physically. Native Intel
and Windows execution and hosted CI remain pending. MyWhoosh version and pod
firmware were not recorded; these observations apply to the tested setup.

## Testing without a controller

`just integration-macos` exercises config loading, real 0x23 frame decoding,
notification gating, mirrored-pod deduplication, key resolution, and the Quartz
state machine together. It checks holds and repeat, final release, disconnect
with late frames, profile-map changes, shutdown, and blocked-focus recovery. The root integration
tests also exercise the real profile and mode switching functions.
The controlled clock also fires cancelled callbacks to check stale-timer safety.
Both macOS CI architectures run this suite through `just ci-macos`.

Only the clock, permission response, physical modifier state, and final OS event
sink are replaced. The test bridge is compiled only with the `integration` tag;
normal executables do not expose it. Tests assemble session input components
without a Bluetooth transport: they do not test native discovery/handshakes,
the complete Session function, native macOS event delivery, or
MyWhoosh. Physical validation remains necessary for those boundaries.

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
- Connect both pods; test all ten buttons in double mode and right-only input in single mode.
- Sign into Zwift and verify left input beyond one minute and after ten minutes idle.
- Test shared-key holds and Ctrl+G mode changes during a hold.
- Tap, hold and release a letter in TextEdit; verify repeat stops on release.
- Test arrows and game polling, F keys, Control/Option/Shift, and Caps Lock;
  include a physical modifier held while pressing a controller button.
- Disconnect one pod during a hold, then both; ensure no permanently stuck key.
- Reload or switch profiles during a hold, including a blocked focus profile.
- Quit using Ctrl+C and SIGTERM while holding a button; verify cleanup.
- Leave both pods idle for at least ten minutes; verify the next press works.
- Switch Bluetooth off/on and sleep/wake the pods; verify scan-gated reconnect.

The dual-controller merge requires new hardware validation. Earlier right-controller
results do not establish left unlock renewal or revised shift direction.

An abrupt kill (`SIGKILL`), OS crash, or revoked Accessibility permission can
prevent cleanup events from reaching macOS. Report physical results separately
from build/unit-test results; Intel runtime requires an Intel machine.

## Dual-controller reset coverage

Mode changes, profile changes, and recovery release held keys before applying new mappings.
The change is atomic with respect to controller frames. Old button releases cannot cancel
a new hold that uses the same key. Tests cover mirrored frames, shared keys, both release
orders, stale session cleanup, and concurrent resets. These tests replace OS delivery.
Physical validation of the updated dual-controller build remains pending.
