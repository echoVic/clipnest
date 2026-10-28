#!/usr/bin/env python3
"""Run Clipnest native acceptance tests with real file-dialog UI confirmation."""

import argparse
import json
import os
from pathlib import Path
import plistlib
import shutil
import signal
import subprocess
import sys
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, help="save JSON results in this directory")
    parser.add_argument("--negative-control", action="store_true", help="deliberately fail the confirmed Bundle ID assertion")
    parser.add_argument("--ui-timeout", type=int, default=180, help="seconds allowed for each real UI confirmation")
    args = parser.parse_args()
    if args.ui_timeout < 1:
        parser.error("--ui-timeout must be positive")
    if sys.platform != "darwin":
        parser.error("this acceptance test requires a macOS desktop session")
    root = Path(__file__).resolve().parent.parent
    work = Path(tempfile.mkdtemp(prefix="clipnest-native-exclusions-"))
    print(f"Acceptance workspace: {work}", flush=True)
    output = (args.output or work).resolve()
    output.mkdir(parents=True, exist_ok=True)
    fixture = work / "fixtures/Native E2E Fixture.app"
    (fixture / "Contents").mkdir(parents=True)
    with (fixture / "Contents/Info.plist").open("wb") as f:
        plistlib.dump({
            "CFBundleIdentifier": "app.clipnest.e2e.fixture",
            "CFBundleName": "Native E2E Fixture", "CFBundleDisplayName": "Native E2E Fixture",
            "CFBundlePackageType": "APPL",
        }, f)
    source = work / "source"
    source.mkdir()
    for path in root.glob("*.go"):
        if path.name != "main.go" and not path.name.endswith("_test.go"):
            shutil.copy2(path, source / path.name)
    for name in ("go.mod", "go.sum", "mygo.json"):
        shutil.copy2(root / name, source / name)
    shutil.copytree(root / "web", source / "web")
    shutil.copy2(root / "testdata/native-exclusions/main.go", source / "main.go")
    # Change only the acceptance host's initial picker directory. Selection,
    # filtering, callback, metadata lookup and persistence stay unchanged.
    picker_source = source / "native_application_darwin.go"
    picker_text = picker_source.read_text()
    if picker_text.count('"/Applications"') != 1:
        raise RuntimeError("picker initial-directory configuration changed; update the test launcher")
    picker_source.write_text(picker_text.replace('"/Applications"', json.dumps(str(fixture.parent))))
    bundle = work / "Clipnest Native E2E.app"
    contents = bundle / "Contents"
    (contents / "MacOS").mkdir(parents=True)
    driver = contents / "MacOS/driver.dylib"
    subprocess.run(["xcrun", "clang", "-dynamiclib", "-fobjc-arc", "-Wall", "-Wextra", "-Werror", "-framework", "Cocoa",
                    str(root / "testdata/native-exclusions/driver.m"), "-o", str(driver)], check=True)
    binary = contents / "MacOS/native-e2e"
    subprocess.run(["go", "build", "-race", "-o", str(binary), "."], cwd=source, check=True)
    with (contents / "Info.plist").open("wb") as f:
        plistlib.dump({
            "CFBundleIdentifier": "app.clipnest.native-exclusions.e2e",
            "CFBundleName": "Clipnest Native E2E", "CFBundleExecutable": binary.name,
            "CFBundlePackageType": "APPL", "NSPrincipalClass": "NSApplication",
            "LSMinimumSystemVersion": "13.0", "NSHighResolutionCapable": True,
        }, f)
    summary = {
        "sourceCommit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(),
        "hostOS": subprocess.check_output(["sw_vers", "-productVersion"], text=True).strip(),
        "negativeControl": args.negative_control,
        "cases": [],
        "workspace": str(work),
        "scope": "Native settings actions and real NSOpenPanel confirmation; isolated data, paused collection, fixture initial directory.",
    }
    failure = None
    modes = ["manage"] if args.negative_control else ["manage", "restart", "quit-open", "quit-confirm"]
    try:
        for mode in modes:
            data = work / ("data-manage" if mode in ("manage", "restart") else f"data-{mode}")
            result = work / f"{mode}.json"
            stdout = work / f"{mode}.stdout"
            stderr = work / f"{mode}.stderr"
            argv = ["--data-dir", str(data), "--driver", str(driver), "--fixture", str(fixture),
                    "--result", str(result), "--mode", mode, "--ui-timeout", f"{args.ui_timeout}s"]
            if args.negative_control:
                argv.append("--negative-control")
            print(f"Mode: {mode}. Progress: {result}.phase.json", flush=True)
            if mode != "quit-open":
                print(f"When select-and-confirm appears, select {fixture} and click 添加.", flush=True)
            # Each process performs at most one confirmation. Restart uses the
            # previous process's data rather than seeding a rule in memory.
            try:
                subprocess.run(["open", "-n", "-W", "--stdout", str(stdout), "--stderr", str(stderr),
                                str(bundle), "--args", *argv], timeout=args.ui_timeout + 90, check=True)
            except (subprocess.SubprocessError, KeyboardInterrupt):
                # open -W's timeout does not itself terminate its child app.
                pid_path = Path(str(result) + ".pid")
                if pid_path.exists():
                    pid = int(pid_path.read_text())
                    command = subprocess.run(["ps", "-p", str(pid), "-o", "command="],
                                             capture_output=True, text=True).stdout
                    if command.startswith(str(binary)) or command.startswith(str(binary.resolve())):
                        try:
                            os.kill(pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                raise
            if not result.exists():
                raise RuntimeError(f"{mode}: app exited without results")
            report = json.loads(result.read_text())
            summary["cases"].append(report)
            for case in report["checks"]:
                print(f"{'PASS' if case['passed'] else 'FAIL'}: {case['name']}", flush=True)
            if report.get("error"):
                raise RuntimeError(f"{mode}: {report['error']}")
            if stderr.exists() and "WARNING: DATA RACE" in stderr.read_text():
                raise RuntimeError(f"{mode}: race detector reported a data race")
    except (subprocess.SubprocessError, RuntimeError, KeyboardInterrupt) as err:
        failure = err
        summary["error"] = str(err) or "interrupted"
    finally:
        summary["passed"] = sum(case["passed"] for report in summary["cases"] for case in report["checks"])
        summary["failed"] = sum(not case["passed"] for report in summary["cases"] for case in report["checks"])
        for pattern in ("*.json", "*.stderr", "*.stdout", "*.ui.txt"):
            for path in work.glob(pattern):
                if path.parent != output:
                    shutil.copy2(path, output / path.name)
        result_path = output / "native-exclusions-results.json"
        result_path.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n")
        print(f"Results: {result_path}", flush=True)
    if failure is not None:
        raise RuntimeError(summary["error"]) from failure
    print(f"Native acceptance passed: {summary['passed']} checks.")


if __name__ == "__main__":
    try:
        main()
    except (subprocess.SubprocessError, RuntimeError, OSError) as err:
        print(f"Native acceptance failed: {err}", file=sys.stderr)
        sys.exit(1)
