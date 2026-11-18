# Clipnest

[简体中文](README.md) | English

A keyboard-first clipboard history panel for macOS and Windows. Copy as usual and get it back whenever you need it; pick with numbers, arrow keys or the scroll wheel, and confirm to paste back into the app you came from.

## Download

- **macOS 13+**: download the universal installer from [Releases](https://github.com/echoVic/clipnest/releases); it runs on Intel and Apple silicon and is signed with a Developer ID and notarized by Apple.
- **Windows (amd64)**: the NSIS installer in the same release, supported since 0.1.9. It isn't signed yet, so on the first run choose More info → Run anyway in SmartScreen.

## Usage

- Press **⌘⇧V** (**Ctrl+Alt+V** on Windows) to open the panel; **1–9** paste directly, **↑↓ / wheel** move, **Enter** confirms and **Esc** closes.
- **⌘K** searches everything, and **⌘,** (**Ctrl+,** on Windows) opens preferences; the Shortcuts tab lists every panel shortcut.
- Select an entry and press **⌘/** (**Ctrl+/** on Windows), or click the "…" under the preview, to open the actions menu for that entry. Shortcuts work directly too (Ctrl replaces ⌘ on Windows): **⌘↩** copy only; **⌘S** save as a snippet in a prefilled editor you can edit before saving (images and text over 64 KB can't be saved); **⌘O** open a link in your default browser; **⌘⌫ / Delete** remove it. Deleting history selects the entry that moves up, and typing in the search field still deletes characters.
- **Paste in sequence**: copy several things in a row, select the earliest of them in history and choose Paste in sequence from here in the actions menu. Each **⌘V** (**Ctrl+V** on Windows) then pastes the next one in copy order, up to the newest, at most 100. The count stays beside the menu bar icon (in the tray tooltip on Windows); stop it from the tray menu or the banner at the top of the panel, and it ends on its own after 10 minutes without a paste. macOS and Windows only; macOS needs Accessibility permission.
- **Pin the panel**: press **⌘P** (**Ctrl+P** on Windows) or click the pin at the top to keep the panel on screen. It stays up when you switch apps and after you paste, and a paste gives the keyboard back to the app it went into. The paste target at the bottom follows the last app you used. Press the panel shortcut to bring the keyboard back to the panel, and **Esc** to close it and unpin it.
- Switch to **Snippets** at the top: insertion works like history, **⌘N** creates, **⌘E** edits, **⌘⌫ / Delete** removes (with confirmation) and **⌥↑ / ⌥↓** reorder.
- Auto-paste needs Accessibility permission on macOS; without it you can use Copy Only. Windows needs no permission.
- If the shortcut is taken, Clipnest tries the next one (macOS **⌥⇧V**, Windows **Ctrl+Shift+V**), and you can pick another in Preferences → Shortcuts.
- On Windows, turn on **Open Clipnest with Win+V** in Preferences → Shortcuts. Clipnest takes **Win+V** from the system clipboard panel and restarts Explorer, so the taskbar disappears for a few seconds. Emoji, GIFs and clipboard history from that panel then open with **Win+.**, and while Clipnest isn't running, Win+V does nothing. Turning the switch off or uninstalling Clipnest gives Win+V back to Windows.

## Features

- Records plain text, links, code and PNG images; duplicates are merged and the most recent copy moves to the front. File lists are not recorded.
- SQLite storage with no limit on the number of entries; history is trimmed by capacity (512 MiB by default) and by days kept. Snippets are stored separately, up to 500.
- Search text, titles and source apps, filtered by text, link, code or image; text in screenshots is recognized on-device and becomes searchable too, never uploaded: by the Vision framework on macOS and by the built-in Windows OCR on Windows, which reads Chinese once Chinese (with Optical character recognition) is added in Windows language settings.
- Exclude apps from recording; since macOS 15.4 Clipnest follows the system clipboard-recording permission and won't read in the background when the answer is Default or Denied.
- Recognizes common keys and tokens (AWS, GitHub, GitLab, Slack, Stripe, Google, JWT, private keys and more) and card numbers copied on their own. By default they are recorded masked: the list, preview and search show only the start and end, Show reveals the original, and pasting still uses the original. You can also skip them or turn recognition off.
- Ignore content by regular expression, such as `^\d{6}$` for one-time codes. When a password manager clears the clipboard within 5 minutes of a copy, the entry it left is deleted too (on by default).
- History loads 50 entries per page and the list renders only what's visible, so arrow keys and the wheel keep working with ten thousand entries.
- Pin the panel to paste back and forth between apps without summoning it each time; new copies show up in an open panel as you make them, and the selection stays put.
- Follows Reduce Motion, Reduce Transparency and Increase Contrast.
- An actions menu and shortcuts for the selected entry (copy only, paste in sequence, save as a snippet, open a link, delete); the Shortcuts tab in preferences lists every panel shortcut, with Ctrl/Alt/Shift on Windows and Linux.

## Privacy and data

- Nothing is uploaded or analyzed online. Data lives in `~/Library/Application Support/Clipnest/clipnest.sqlite3` on macOS and `%AppData%\Clipnest\clipnest.sqlite3` on Windows by default. Preferences → Storage → Data location moves it to another folder or disk: Clipnest copies and checks the data, restarts, and deletes the old copy once the new place has opened. When that disk isn't connected, Clipnest asks at startup instead of starting new data elsewhere.
- New installs use SQLite (file mode 0600) and are not encrypted; pause recording or clear history when needed.
- Text is capped at 512 KiB per entry, images at 8 MiB and 16,000 pixels on the longest side; larger copies are skipped.
- Clipboards marked as sensitive are ignored (macOS `ConcealedType` and friends, Windows `ExcludeClipboardContentFromMonitorProcessing`). Unmarked content can only be recognized by format: masking affects what is shown and searched, while the database keeps the original. Use ignore rules for formats Clipnest doesn't recognize. Settings are stored in `privacy.json` in the data directory.

## Development

You need Go (version in `go.mod`; MyGo is pinned to 0.8.3) and Node for the browser tests. Running the app needs no Node, Bun or network.

```sh
go run . --data-dir /tmp/clipnest-dev            # a separate data directory
go run . --web 127.0.0.1:4183                    # browser demo with in-memory sample data
```

Checks (the same ones CI runs):

```sh
go test ./...
go test -race -count=1 ./...
go vet ./...
go tool mygo vet .
node --test web/*.test.mjs
```

Build a local macOS preview: `go tool mygo build -platform darwin/universal -skip-dmg -skip-notarize -sign - .`; cross-build Windows with `go tool mygo build -platform windows/amd64`, which needs NSIS installed. Release builds run `scripts/sign-notarize-macos.sh` for signing, notarization and stapling.

## Documents

- [Storage and search improvements](docs/storage-and-response.md)
- [App exclusion validation](docs/app-exclusions-validation.md)
- [Image text validation](docs/image-text-validation.md)
- [CHANGELOG](CHANGELOG.md): the changes in each version

## License

[MIT](LICENSE), © 2026 echoVic.
