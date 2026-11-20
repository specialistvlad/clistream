"""Run XCTest against a private broker, live pipes, PTY, CLI viewer and loopback services."""

import contextlib
import http.server
import json
import os
import re
import shutil
import socket
import socketserver
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

APP = Path(__file__).resolve().parent
BIN = APP.parent / "bin"


class ReverseTCP(socketserver.BaseRequestHandler):
    def handle(self):
        with self.request.makefile("rb") as reader:
            self.request.sendall(reader.read()[::-1])


class ReverseUDP(socketserver.BaseRequestHandler):
    def handle(self):
        data, sock = self.request
        sock.sendto(data[::-1], self.client_address)


def run(root, stack, suite, command):
    if simulator := os.environ.get("SIMULATOR_ID"):
        settings = {"appearance": "light"} if suite == "files" else {}
        if suite == "touch":
            settings = {"appearance": "dark", "increase_contrast": "enabled", "content_size": "accessibility-large"}
        for setting, value in settings.items():
            action = ["xcrun", "simctl", "ui", simulator, setting]
            previous = subprocess.check_output(action, text=True).strip()
            if previous != value:
                subprocess.run([*action, value], check=True)
                stack.callback(subprocess.run, [*action, previous], check=True)
    work = root / "workspace"
    work.mkdir()
    (work / "inbox" / "nested").mkdir(parents=True)
    (root / "outside").write_text("private")
    (work / "escape").symlink_to(root)
    name = f"QA{os.urandom(4).hex()}"
    saved = root / "saved"
    saved.mkdir()
    sample = os.urandom(75007).hex()[:150013].encode("ascii")
    (work / f"{name}.txt").write_bytes(sample)
    (work / "fixture.txt").touch()
    if suite == "touch":
        for index in range(60):
            (work / "inbox/nested" / f"row-{index:03d}").touch()
    server = [BIN / "clistreamd", "-state", root / "server"]
    supervisor = [BIN / "clistream-supervise", "-state", root / "sup"]
    viewer = [BIN / "clistream", "-state", root / "viewer"]

    def launch(args, log, env=None, stdin=subprocess.DEVNULL):
        with (
            (root / (log + ".out")).open("wb") as out,
            (root / (log + ".err")).open("wb") as err,
        ):
            proc = subprocess.Popen(
                args,
                cwd=work,
                env=env,
                stdin=stdin,
                stdout=out,
                stderr=err,
            )

        def stop():
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=4)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()

        stack.callback(stop)
        return proc

    def wait_until(check):
        for _ in range(200):
            result = check()
            if result:
                return result
            time.sleep(0.05)
        raise RuntimeError("fixture did not become ready")

    with socket.socket(type=socket.SOCK_DGRAM) as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    launch(server + ["-listen", f"127.0.0.1:{port}"], "server")
    wait_until(lambda: (root / "server/admin.sock").exists())

    def invite(role):
        return subprocess.check_output(server + ["-qr=false", "invite", role])

    subprocess.run(supervisor + ["-pair"], input=invite("supervisor"), check=True)
    subprocess.run(viewer + ["pair"], input=invite("client"), check=True)
    exports = []
    for proto, cls, handler in [
        ("tcp", socketserver.ThreadingTCPServer, ReverseTCP),
        ("udp", socketserver.ThreadingUDPServer, ReverseUDP),
    ]:
        endpoint = stack.enter_context(cls(("127.0.0.1", 0), handler))
        endpoint.daemon_threads = True
        threading.Thread(target=endpoint.serve_forever, daemon=True).start()
        stack.callback(endpoint.shutdown)
        exports += [
            "-export",
            f"{proto}={proto}:127.0.0.1:{endpoint.server_address[1]}",
        ]

    filler_processes = []

    def session(mode, args, label, flags=(), env=None, filler=False):
        proc = launch(
            supervisor + ["-mode", mode, "-local-input=false", *flags, "--", *args],
            label,
            env,
        )

        if filler:
            filler_processes.append(proc)

        def registered():
            for msg in (root / (label + ".err")).read_text().splitlines():
                if msg.startswith("session "):
                    return msg.split()[1]

        return wait_until(registered)

    greeting = "echo 'copyword selection'; " if suite == "touch" else ""
    sid = session(
        "pipe",
        [
            "/bin/sh",
            "-c",
            greeting + 'while IFS= read -r v; do echo "out:$v"; echo "err:$v" >&2; done',
        ],
        "pipe",
        exports,
    )
    pty = ""
    if suite == "hosted":
        pty = session(
            "pty",
            [
                "/bin/sh",
                "-c",
                """stty -echo; read v; stty size; printf '\\033[32m%s\\033[0m\\n' "$v"; exit 23""",
            ],
            "pty",
        )
    if suite == "scroll":
        # Fill the real session list past the bottom edge of a full-size iPad.
        for index in range(22):
            session("pipe", ["/bin/cat"], f"scroll-row-{index}", filler=True)

    # Capture bytes at the child, beyond the UI, terminal emulator and broker.
    capture = root / "terminal-input"
    ui_pty = session(
        "pty",
        [
            sys.executable,
            "-u",
            "-c",
            """
import os, signal, sys, time, tty
tty.setraw(0)
sizes = open(sys.argv[1] + ".sizes", "w", buffering=1)
def record_size(*args):
    cols, rows = os.get_terminal_size(0)
    sizes.write(f"{cols} {rows}\\n")
signal.signal(signal.SIGWINCH, record_size)
record_size()
os.write(1, b'\\x1b[2 q\\x1b[?25l')
if sys.argv[2] == "scroll":
    os.write(1, b'scroll-ready')
    while not os.path.exists(sys.argv[1] + ".history"):
        time.sleep(0.05)
    for number in range(350):
        os.write(1, f"history-{number:03d}\\r\\n".encode())
    os.write(1, b'history-end')
else:
    os.write(1, b'COPYME terminal selection test\\r\\n')
    os.write(1, b'\\x1b[?1000h\\x1b[?1006h')
with open(sys.argv[1], 'wb', buffering=0) as captured:
    mouse_press = b'\\x1b[<0;1;1M'
    tail = b''
    clicks = 0
    while data := os.read(0, 4096):
        captured.write(data)
        pending = tail + data
        reported = pending.count(mouse_press)
        tail = pending[-(len(mouse_press) - 1):]
        if reported:
            clicks += reported
            os.write(1, f"\\x1b[2;1Hpointer-{clicks}".encode())
""",
            str(capture), suite,
        ],
        "ui-pty",
    ) if suite in {"scroll", "terminal"} else ""
    observer = launch(viewer + ["attach", sid], "observer", stdin=subprocess.PIPE)
    stack.callback(observer.stdin.close)
    env = os.environ.copy()
    env.update(
        TEST_RUNNER_CLISTREAM_SESSION=sid,
        TEST_RUNNER_CLISTREAM_PTY_SESSION=pty,
        TEST_RUNNER_CLISTREAM_UI_PTY=ui_pty,
        TEST_RUNNER_CLISTREAM_FILENAME=name,
        TEST_RUNNER_CLISTREAM_SAVE_DIR=str(saved),
    )
    # CI can take longer to launch a cold simulator than the lifetime of an
    # invitation. Only this private runner may request one client invite per case.
    route = "/" + os.urandom(24).hex() + "/"
    available = {"PIPE", "PTY", "FILES", "PORTS", "DRIVERS", "QR"}
    if suite == "hosted":
        available.update({"FLUSH", "FLUSHED"})
    if suite == "scroll":
        available.add("HISTORY")
    if simulator and suite == "files":
        available.add("DOCUMENT")
    if simulator and suite == "touch":
        available.add("WORD")
    initial_grid = None
    if simulator and suite == "terminal":
        available.update({"GRID", "RESIZED"})
        model = subprocess.check_output(
            ["xcrun", "simctl", "getenv", simulator, "SIMULATOR_MODEL_IDENTIFIER"], text=True
        ).strip()
        if model.startswith("iPad"):
            available.add("HID")
    class Invitations(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            nonlocal initial_grid
            target = urlsplit(self.path)
            purpose = target.path.removeprefix(route)
            if not target.path.startswith(route) or purpose not in available:
                self.send_error(404)
                return
            available.remove(purpose)
            if purpose in {"DOCUMENT", "WORD"}:
                try:
                    params = parse_qs(target.query, strict_parsing=True)
                    x, y = (float(params[axis][0]) for axis in ("x", "y"))
                    if not (0 <= x < 4096 and 0 <= y < 4096):
                        raise ValueError("invalid simulator point")
                    gesture = ["--hold", "1"] if purpose == "WORD" else []
                    subprocess.run(["axe", "tap", "-x", str(x), "-y", str(y),
                                    *gesture, "--udid", simulator], check=True, timeout=30)
                except (OSError, KeyError, ValueError, subprocess.SubprocessError):
                    self.send_error(503, "Simulator touch delivery failed")
                    return
                payload = b"delivered"
            elif purpose == "HID":
                # USB HID usage 224 is left Control and usage 7 is the D key.
                # XCTest can otherwise synthesize a plain software-keyboard "d".
                try:
                    subprocess.run(["axe", "key-combo", "--modifiers", "224", "--key", "7",
                                    "--udid", simulator], check=True, timeout=25)
                    wait_until(lambda: b"\x04" in capture.read_bytes())
                except (OSError, RuntimeError, subprocess.SubprocessError) as error:
                    print(f"Simulator hardware input failed: {error}. Test child received {capture.read_bytes()!r}", flush=True)
                    self.send_error(503, "Simulator hardware input failed")
                    return
                payload = b"delivered"
            elif purpose in {"GRID", "RESIZED"}:
                def grid():
                    lines = Path(str(capture) + ".sizes").read_bytes().splitlines()
                    return lines[-1] if lines else None

                if purpose == "GRID":
                    initial_grid = wait_until(grid)
                    payload = initial_grid
                else:
                    def changed_grid():
                        current = grid()
                        return current if current != initial_grid else None

                    try:
                        payload = wait_until(changed_grid)
                    except RuntimeError:
                        print(f"Child PTY size stayed at {initial_grid!r}", flush=True)
                        self.send_error(409, "Child PTY size did not change")
                        return
            elif purpose == "FLUSHED":
                try:
                    wait_until(lambda: b"out:before-detach" in (root / "observer.out").read_bytes().splitlines())
                except RuntimeError:
                    self.send_error(409, "Child did not receive the last input before detach")
                    return
                payload = b"received"
            elif purpose == "HISTORY":
                # The real list has already scrolled and the PTY is attached.
                # Release its filler supervisors before exercising scrollback.
                for proc in filler_processes:
                    if proc.poll() is None:
                        proc.terminate()
                Path(str(capture) + ".history").touch()
                payload = b"released"
            elif purpose == "QR":
                qr = subprocess.check_output(server)
                payload = subprocess.check_output([APP / "build/test-qr"], input=qr)
            else:
                payload = invite("client")
            self.send_response(200)
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

    service = stack.enter_context(http.server.HTTPServer(("127.0.0.1", 0), Invitations))
    threading.Thread(target=service.serve_forever, daemon=True).start()
    stack.callback(service.shutdown)
    env["TEST_RUNNER_CLISTREAM_FIXTURE"] = f"http://127.0.0.1:{service.server_port}{route}"
    if suite == "hosted" and os.environ.get("CLISTREAM_APP_DRIVERS") == "1":
        sys.path.insert(0, str(APP.parent / "tests"))
        from model_endpoint import CLAUDE_ARGS, CODEX_ARGS, Messages, claude_environment

        endpoint = stack.enter_context(
            http.server.ThreadingHTTPServer(("127.0.0.1", 0), Messages)
        )
        endpoint.challenge = "AppleClient-" + os.urandom(10).hex()
        endpoint.answer = "AppleResponse-" + os.urandom(10).hex()
        endpoint.seen = threading.Event()
        threading.Thread(target=endpoint.serve_forever, daemon=True).start()
        stack.callback(endpoint.shutdown)
        for driver, args, process_env in [
            (
                "claude",
                [
                    *CLAUDE_ARGS,
                    "--input-format",
                    "stream-json",
                    "--output-format",
                    "stream-json",
                    "--verbose",
                ],
                claude_environment(root / "claude", endpoint),
            ),
            ("codex", CODEX_ARGS, None),
        ]:
            exe = shutil.which(driver)
            if not exe:
                raise RuntimeError(
                    f"{driver} executable required for the Apple driver suite"
                )
            env["TEST_RUNNER_CLISTREAM_" + driver.upper() + "_SESSION"] = session(
                "pipe", [exe, *args], driver, env=process_env
            )
        env.update(
            TEST_RUNNER_CLISTREAM_CLAUDE_INPUT=endpoint.challenge,
            TEST_RUNNER_CLISTREAM_CLAUDE_OUTPUT=endpoint.answer,
        )
    # An independent writer produces output while the UI is inside Files.
    def signal_upload():
        target = work / "inbox/nested" / f"{name}.txt"
        while not finished.wait(0.05):
            if target.exists():
                observer.stdin.write(b"workspace-finished\n")
                observer.stdin.flush()
                return

    finished = threading.Event()
    worker = threading.Thread(target=signal_upload, daemon=True)
    worker.start()
    try:
        # Each terminal-key check opens a native menu and waits for UIKit to
        # finish animating. A cold hosted simulator can spend minutes there.
        timeout = 720 if simulator and suite in {"terminal", "scroll"} else 420
        subprocess.run(command, cwd=APP, env=env, check=True, timeout=timeout)
    except (subprocess.TimeoutExpired, subprocess.CalledProcessError) as failure:
        stage = "Timed out" if isinstance(failure, subprocess.TimeoutExpired) else "XCTest failed"
        observed = (root / "observer.out").read_bytes().splitlines()
        print(f"{stage} in {suite}. Echo received: {b'out:echo' in observed}. "
              f"Workspace output received: {b'out:workspace-finished' in observed}. "
              f"Uploaded file published: {(work / 'inbox/nested' / (name + '.txt')).exists()}", flush=True)
        if "-resultBundlePath" in command:
            bundle = Path(command[command.index("-resultBundlePath") + 1])
            for log in bundle.glob("Staging/**/Session-*UITests*.log"):
                actions = [
                    line.partition(" +0000: ")[-1]
                    for line in log.read_text(errors="replace").splitlines()
                    if "willStartActivity:" in line and " +0000: " in line
                ]
                for action in actions[-10:]:
                    if not any(s in action for s in ["://", "Type text", "Type '"]):
                        print(re.sub(r"[a-fA-F0-9]{24,}", "<redacted>", action)[:190])
        raise
    finally:
        finished.set()
        worker.join(5)
    bundle = command[command.index("-resultBundlePath") + 1]
    summary = json.loads(subprocess.check_output([
        "xcrun", "xcresulttool", "get", "test-results", "summary", "--path", bundle
    ]))
    optional_driver = int(suite == "hosted" and os.environ.get("CLISTREAM_APP_DRIVERS") != "1")
    minimum = 6 if suite == "hosted" else 1
    passed, skipped, failed = (summary[key] for key in ("passedTests", "skippedTests", "failedTests"))
    if failed or skipped != optional_driver or passed < minimum - optional_driver:
        raise RuntimeError(f"Unexpected {suite} XCTest outcome: {passed} passed, {skipped} skipped, {failed} failed")
    if suite == "hosted":
        for label in ("pipe", "observer"):
            assert b"out:hosted-apple-echo" in (root / (label + ".out")).read_bytes()
            assert b"err:hosted-apple-echo" in (root / (label + ".err")).read_bytes()
        expected = bytes(range(256)) * 937 + bytes(range(147))
        assert len(expected) == 240019
        assert (work / "inbox/apple.bin").read_bytes() == expected
    elif suite == "files":
        lines = (root / "observer.out").read_bytes().splitlines()
        assert b"out:echo" in lines
        if not simulator:
            assert b"out:keyboard-return" in lines, "input focus was not restored after closing Files"
        assert (work / "inbox/nested" / f"{name}.txt").read_bytes() == sample
    elif suite == "touch":
        observed = (root / "observer.out").read_bytes().splitlines()
        assert b"out:copyword" in observed and b"out:copyword selection" in observed, observed
    elif suite == "scroll":
        assert capture.read_bytes() == b"", "scrolling or keyboard controls sent terminal input"
    elif suite == "terminal":
        received = capture.read_bytes()
        assert b"\x1b[<0;1;1M" in received, (
            f"missing mouse press in first cell: {received!r}"
        )
        expected_presses = 2
        if simulator:
            model = subprocess.check_output(
                ["xcrun", "simctl", "getenv", simulator, "SIMULATOR_MODEL_IDENTIFIER"], text=True
            ).strip()
            if model.startswith("iPad"):
                expected_presses += 1
                assert received.count(b"\x04") == 1 and b"d" not in received, (
                    f"incorrect hardware Control-D: {received!r}"
                )
        assert received.count(b"\x1b[<0;1;1M") == expected_presses, received
        resize_events = Path(str(capture) + ".sizes").read_text().splitlines()
        sizes = [tuple(map(int, line.split())) for line in resize_events]
        assert len(set(sizes)) >= 2 and all(min(size) > 0 for size in sizes), sizes
        if os.environ.get("SIMULATOR_ID"):
            assert b"\x1b[A\x1b[B\x1b[C\x1b[D\x1b\x03" in received, (
                f"missing terminal keys: {received!r}"
            )
    print(f"PASS: {suite} fixture assertions", flush=True)


if __name__ == "__main__":
    with (
        tempfile.TemporaryDirectory(
            dir="/tmp", prefix="clistream-accept-"
        ) as directory,
        contextlib.ExitStack() as stack,
    ):
        run(Path(directory), stack, sys.argv[1], sys.argv[2:])
