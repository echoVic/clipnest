# Clipnest

[简体中文](README.md) | English

A keyboard-first clipboard history panel for macOS and Windows. Copy as usual and get it back whenever you need it; pick with numbers, arrow keys or the scroll wheel, and confirm to paste back into the app you came from.

## Download

- **macOS 13+**: download the universal installer from [Releases](https://github.com/echoVic/clipnest/releases); it runs on Intel and Apple silicon and is signed with a Developer ID and notarized by Apple.
- **Windows (amd64)**: the NSIS installer in the same release, supported since 0.1.9. It isn't signed yet, so on the first run choose More info → Run anyway in SmartScreen.

## Usage

- Press **⌘⇧V** (**Ctrl+Alt+V** on Windows) to open the panel; **1–9** paste directly, **↑↓ / wheel** move, **Enter** confirms and **Esc** closes.
- **⌘K** searches everything, and **⌘,** (**Ctrl+,** on Windows) opens preferences.
- Switch to **Snippets** at the top: insertion works like history, **⌘N** creates, **⌘E** edits and **⌥↑ / ⌥↓** reorder.
- Auto-paste needs Accessibility permission on macOS; without it you can use Copy Only. Windows needs no permission.
- If the shortcut is taken, Clipnest tries the next one (macOS **⌥⇧V**, Windows **Ctrl+Shift+V**), and you can pick another in preferences.

## Features

- Records plain text, links, code and PNG images; duplicates are merged and the most recent copy moves to the front. File lists are not recorded.
- SQLite storage with no limit on the number of entries; history is trimmed by capacity (512 MiB by default) and by days kept. Snippets are stored separately, up to 500.
- Search text, titles and source apps, filtered by text, link, code or image; on macOS, text recognized by the system Vision framework from screenshots is searchable too, on-device and never uploaded.
- Exclude apps from recording; since macOS 15.4 Clipnest follows the system clipboard-recording permission and won't read in the background when the answer is Default or Denied.
- History loads 50 entries per page and the list renders only what's visible, so arrow keys and the wheel keep working with ten thousand entries.
- Follows Reduce Motion, Reduce Transparency and Increase Contrast.

## Privacy and data

- Nothing is uploaded or analyzed online. Data lives in `~/Library/Application Support/Clipnest/clipnest.sqlite3` on macOS and `%AppData%\Clipnest\clipnest.sqlite3` on Windows.
- New installs use SQLite (file mode 0600) and are not encrypted; pause recording or clear history when needed.
- Text is capped at 512 KiB per entry, images at 8 MiB and 16,000 pixels on the longest side; larger copies are skipped.
- Clipboards marked as sensitive are ignored (macOS `ConcealedType` and friends, Windows `ExcludeClipboardContentFromMonitorProcessing`); unmarked sensitive content can't be reliably detected.

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
