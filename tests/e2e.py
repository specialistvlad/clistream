"""Exercise installed binaries on real UDP sockets with isolated, temporary identities."""

import concurrent.futures
import contextlib
import http.server
import json
import os
import pathlib
import pty
import re
import selectors
import signal
import shutil
import socket
import statistics
import subprocess
import sys
import tempfile
import termios
import threading
import time
import unittest

from model_endpoint import CLAUDE_ARGS, CODEX_ARGS, Messages, claude_environment
from network_path import NetworkPath

BIN = pathlib.Path(
    os.environ.get("CLISTREAM_BIN", pathlib.Path(__file__).resolve().parents[1] / "bin")
)
DAEMON, SUP, CLI = [
    str(BIN / name) for name in ("clistreamd", "clistream-supervise", "clistream")
]


def port(kind=socket.SOCK_STREAM):
    with socket.socket(type=kind) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def line(stream):
    with selectors.DefaultSelector() as sel:
        sel.register(stream, selectors.EVENT_READ)
        if not sel.select(10):
            raise AssertionError("timed out waiting for output")
    return stream.readline()


def status_line(stream, prefix):
    for _ in range(16):
        msg = line(stream)
        if msg.startswith(prefix):
            return msg
        if not msg:
            break
    raise AssertionError(f"missing process status {prefix!r}")


class BrokerFixture(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(dir="/tmp")
        self.addCleanup(self.tmp.cleanup)
        self.base = pathlib.Path(self.tmp.name)
        self.work = self.base / "workspace"
        self.work.mkdir()
        self.address = f"127.0.0.1:{port(socket.SOCK_DGRAM)}"
        self.server = [DAEMON, "-state", self.base / "server", "-qr=false"]
        self.supervisor = [SUP, "--interactive", "-once", "-state", self.base / "supervisor"]
        self.a = [CLI, "-state", self.base / "a"]
        self.b = [CLI, "-state", self.base / "b"]
        self.start_server()
        link = self.run_cli(self.server + ["invite"]).stdout
        for role, args in [
            ("client", self.a),
            ("client", self.b),
            ("supervisor", self.supervisor),
        ]:
            self.run_cli(
                args + (["-pair"] if role == "supervisor" else ["pair"]), data=link
            )
            self.assertEqual(
                self.run_cli(
                    args + (["-pair"] if role == "supervisor" else ["pair"]),
                    data=link,
                    check=False,
                ).returncode,
                0,
            )

    def start_server(self):
        self.console = self.base / "daemon.stderr"
        with self.console.open("wb") as output:
            self.daemon = self.spawn(self.server + ["-listen", self.address], stderr=output)
        deadline = time.monotonic() + 8
        while b"serving QUIC" not in self.console.read_bytes():
            self.assertIsNone(self.daemon.poll(), self.console.read_text())
            self.assertLess(time.monotonic(), deadline)
            time.sleep(0.02)

    def spawn(self, args, **kw):
        p = subprocess.Popen(
            args,
            stdin=kw.pop("stdin", subprocess.PIPE),
            stdout=kw.pop("stdout", subprocess.PIPE),
            stderr=kw.pop("stderr", subprocess.PIPE),
            bufsize=0,
            **kw,
        )

        def clean():
            if p.poll() is None:
                p.terminate()
                try:
                    p.wait(2)
                except subprocess.TimeoutExpired:
                    p.kill()
                    p.wait()
            for f in (p.stdin, p.stdout, p.stderr):
                if f:
                    f.close()

        self.addCleanup(clean)
        return p

    def run_cli(self, args, data=None, check=True, env=None):
        result = subprocess.run(
            args, input=data, capture_output=True, timeout=12, check=False, env=env
        )
        if check:
            self.assertEqual(result.returncode, 0, result.stderr)
        return result

    def session(self, flags, cmd, **options):
        p = self.spawn(self.supervisor + flags + ["--"] + cmd, cwd=self.work, **options)
        msg = status_line(p.stderr, b"session ")
        return p, msg.split()[1].decode()


class DashboardTerminal:
    def __init__(self, fixture, args=None):
        self.fixture = fixture
        self.fd, slave = pty.openpty()
        fixture.addCleanup(os.close, self.fd)
        try:
            termios.tcsetwinsize(slave, (24, 100))
            self.process = fixture.spawn(args or fixture.a, stdin=slave, stdout=slave, stderr=slave,
                                         env={**os.environ, "TERM": "xterm-256color"})
        finally:
            os.close(slave)

    def send(self, data):
        os.write(self.fd, data)

    def until(self, marker):
        data = bytearray()
        deadline = time.monotonic() + 12
        with selectors.DefaultSelector() as sel:
            sel.register(self.fd, selectors.EVENT_READ)
            while marker not in data:
                self.fixture.assertTrue(sel.select(max(0, deadline - time.monotonic())),
                                        f"Missing {marker!r} in {data[-4096:]!r}")
                data.extend(os.read(self.fd, 8192))
        return data


PTY_INPUT_PROBE = """
import os, tty
tty.setraw(0)
os.write(1, b'\\x1b[?1049h' + b'fast output\\r\\n' * 400 + b'\\x1b[2J\\x1b[H\\x1b[?1000h\\x1b[?1006hVIEWPORT')
while True:
    b = os.read(0, 1)
    if b == b'!':
        size = os.get_terminal_size()
        os.write(1, f'\\r\\nSIZE={size.columns},{size.lines}\\r\\n'.encode())
    elif b == b'x':
        os._exit(19)
    else:
        os.write(1, ('[%02x]' % b[0]).encode())
"""


class Live(BrokerFixture):
    def managed(self, cmd, flags=()):
        return self.spawn([SUP, "--interactive", "-state", self.base / "supervisor", "-local-input=false", *flags, "--", *cmd],
                          cwd=self.work)

    def test_framed_terminal(self):
        p = self.managed([sys.executable, "-c", PTY_INPUT_PROBE], ["-mode", "pty"])
        sid = status_line(p.stderr, b"session ").split()[1]
        t = DashboardTerminal(self)
        t.until(b"R Restart")
        t.send(b"S")
        t.until(b"[Cancel]")
        t.send(b"\r")
        t.until(b"Enter Attach")
        t.send(b"\r")
        t.until(b"VIEWPORT")
        t.send(b"s\x03\x1b[200~q\x1ds\x1b[201~\x1b[<0;5;8M!")
        t.until(b"SIZE=100,22")
        termios.tcsetwinsize(t.fd, (18, 73))
        os.kill(t.process.pid, signal.SIGWINCH)
        time.sleep(0.2)
        t.send(b"!")
        t.until(b"SIZE=73,16")
        t.send(b"\x07")
        t.until(b"Enter Attach")
        t.send(b"q")
        self.assertEqual(t.process.wait(5), 0)
        # The replay contains every child-bound byte, including the pasted detach key.
        viewer = DashboardTerminal(self, self.b + ["attach", sid])
        replay = viewer.until(b"SIZE=73,16")
        for seq in (b"[73][03][71][1d][73]", b"[1b][5b][3c][30][3b][35][3b][36][4d]", b"CLIStream Client", b"Ctrl-G Home"):
            self.assertIn(seq, replay)
        viewer.send(b"\x07")
        viewer.until(b"Enter Attach")
        viewer.send(b"q")
        self.assertEqual(viewer.process.wait(8), 0)
        direct = DashboardTerminal(self, self.b + ["attach", sid])
        direct.until(b"Ctrl-G Home")
        direct.send(b"x")
        self.assertEqual(direct.process.wait(8), 19)
        self.assertIsNone(p.poll())

    def test_no_remote_restart(self):
        script = 'while read -r word; do echo "received-$word"; done; exit 23'
        sup = self.spawn([SUP, "--interactive", "-no-remote-restart", "-state", self.base / "supervisor",
                          "-mode", "pty", "-local-input=false", "--", "/bin/sh", "-c", script])
        sid = status_line(sup.stderr, b"session ").split()[1].decode()
        first = DashboardTerminal(self)
        first.until(b"Attach only")
        first.send(b"rse\r")
        first.until(b"Ctrl-G Disconnect")
        second = DashboardTerminal(self, self.b + ["attach", sid])
        second.until(b"Ctrl-G Disconnect")
        first.send(b"\x07")
        first.until(b"Attach only")
        second.send(b"still-here\r")
        second.until(b"received-still-here")
        first.send(b"\r")
        first.until(b"received-still-here")
        first.send(b"\x04")
        first.until(b"exit 23")
        self.assertEqual(second.process.wait(8), 23)
        self.assertEqual(sup.wait(8), 23)
        self.assertFalse(json.loads(self.run_cli(self.a).stdout))

    def test_cycle_running_apps(self):
        apps = {}
        for _ in range(3):
            child = self.managed(["/bin/cat"])
            sid = status_line(child.stderr, b"session ").split()[1].decode()
            apps[sid] = child
        self.managed(["/bin/true"])
        # The session list and dashboard have different IDs. Start on a known
        # run, then use each child's local output to identify where input went.
        start = next(iter(apps))
        view = DashboardTerminal(self, self.a + ["attach", start])
        view.until(b"Ctrl-G Home")

        def write_and_identify(keys, word):
            view.send(keys + word + b"\r")
            with selectors.DefaultSelector() as sel:
                for sid, child in apps.items():
                    sel.register(child.stdout, selectors.EVENT_READ, sid)
                ready = sel.select(5)
                self.assertEqual(len(ready), 1)
                entry = ready[0][0]
                self.assertEqual(entry.fileobj.readline(), word + b"\n")
                return entry.data

        second = write_and_identify(b"\x1d", b"second")
        third = write_and_identify(b"\x1d", b"third")
        self.assertEqual(len({start, second, third}), 3)
        self.assertEqual(write_and_identify(b"\x1d", b"wrap"), start)
        self.assertEqual(write_and_identify(b"\x1b[91;5u", b"previous"), third)
        view.send(b"\x1einput\r")
        self.assertEqual(line(apps[third].stdout), b"\x1einput\n")
        view.send(b"\x1b")
        time.sleep(0.15)
        view.send(b"escape\r")
        self.assertEqual(line(apps[third].stdout), b"\x1bescape\n")
        view.send(b"\x07")
        view.until(b"Enter Attach")
        view.send(b"q")
        self.assertEqual(view.process.wait(5), 0)
        self.assertTrue(all(child.poll() is None for child in apps.values()))

    def test_restart_command_widget(self):
        executable = self.work / "codex"
        executable.write_text('#!/bin/sh\nprintf "argc=%s arg=%s\\n" "$#" "$1"\nread value\n')
        executable.chmod(0o700)
        child = self.spawn([SUP, "--interactive", "-state", self.base / "supervisor", "-local-input=false", "codex"],
                           env={**os.environ, "PATH": str(self.work) + os.pathsep + os.environ["PATH"]})
        before = status_line(child.stderr, b"session ").split()[1]
        ui = DashboardTerminal(self)
        ui.until(b"R Restart")
        ui.send(b"r")
        ui.until(b"Enter Restart & attach")
        # Type into the prefill, repair an omission in the middle, and submit.
        ui.send(b" --hlp\x1b[D\x1b[De")
        termios.tcsetwinsize(ui.fd, (12, 28))
        os.kill(ui.process.pid, signal.SIGWINCH)
        ui.send(b"\r")
        after = status_line(child.stderr, b"session ").split()[1]
        self.assertNotEqual(before, after)
        ui.until(b"argc=1 arg=--help")

    def test_managed_supervisors(self):
        executable = self.work / "worker"
        executable.write_text('#!/bin/sh\nprintf "<%s>\\n" "$@"\nread value\nexit 23\n')
        executable.chmod(0o700)
        p = self.managed([str(executable), "private argument", ""])
        old = status_line(p.stderr, b"session ").split()[1]
        first, other = DashboardTerminal(self), DashboardTerminal(self, self.b)
        for t in (first, other):
            t.until(b"R Restart")
            t.send(b"r")
            t.until(b"private argument")
        first.send(b"\r")
        replacement = status_line(p.stderr, b"session ").split()[1]
        self.assertNotEqual(old, replacement)
        first.until(b"Ctrl-G Home")
        other.send(b"\r")
        other.until(b"Check the current run")
        self.assertEqual([r["ID"].encode() for r in json.loads(self.run_cli(self.a).stdout)], [replacement])
        self.assertNotEqual(self.run_cli(self.a + ["attach", old], check=False).returncode, 0)
        first.send(b"\x07")
        first.until(b"R Restart")
        first.send(b"e")
        first.until(b"private argument")
        first.send(b"\x1b")
        first.until(b"R Restart")
        first.send(b"s")
        first.until(b"[Cancel]")
        first.send(b"\t\r")
        first.until(b"R Start")
        self.assertFalse(json.loads(self.run_cli(self.a).stdout))
        first.send(b"r")
        first.until(b"private argument")
        alternate = self.work / "new worker"
        alternate.symlink_to(executable)
        first.send(b"\x15" + b"\x1b[200~" + b'"' + os.fsencode(alternate) +
                   b'" "private argument" ""\n"a b;$HOME"\x1b[201~\r')
        edited = status_line(p.stderr, b"session ").split()[1]
        first.until(b"Ctrl-G Home")
        remote = self.spawn(self.a + ["attach", edited])
        self.assertEqual(line(remote.stdout), b"<private argument>\n")
        self.assertEqual(line(remote.stdout), b"<>\n")
        self.assertEqual(line(remote.stdout), b"<a b;$HOME>\n")
        first.send(b"done\r")
        self.assertEqual(remote.wait(8), 23)
        first.until(b"Stopped")
        self.daemon.terminate()
        self.daemon.wait(8)
        first.until(b"Disconnected")
        self.start_server()
        first.send(b"r")
        first.until(b"R Start")
        first.send(b"r")
        first.until(b"a b;$HOME")
        first.send(b"\r")
        new = status_line(p.stderr, b"session ").split()[1]
        first.until(b"Ctrl-G Home")
        viewer = self.spawn(self.a + ["attach", new])
        self.assertEqual(line(viewer.stdout), b"<private argument>\n")
        self.assertEqual(line(viewer.stdout), b"<>\n")
        self.assertEqual(line(viewer.stdout), b"<a b;$HOME>\n")
        first.send(b"\x07")
        first.until(b"R Restart")
        first.send(b"r")
        first.until(b"a b;$HOME")
        first.send(b' "\r')
        first.until(b"unfinished quote")
        self.assertIsNone(viewer.poll())
        first.send(b"\x7f")
        alternate.unlink()
        first.send(b"\r")
        first.until(b"Check the executable")
        self.assertIsNone(viewer.poll())
        alternate.write_text("#!/missing-interpreter\n")
        alternate.chmod(0o700)
        first.send(b"\r")
        self.assertEqual(viewer.wait(8), 128)
        first.until(b"Launch failed")
        first.send(b"\x15/bin/cat\r")
        retry = status_line(p.stderr, b"session ").split()[1]
        self.assertNotEqual(retry, new)
        first.until(b"Ctrl-G Home")
        self.assertIsNone(p.poll())
        p.terminate()
        self.assertEqual(p.wait(8), 0)
        log = (self.base / "server/server.log").read_bytes()
        self.assertNotIn(b"private argument", log)
        self.assertNotIn(b"a b;$HOME", log)
        actions = [json.loads(row) for row in log.splitlines()]
        actions = [event for event in actions if event["event"] == "supervisor_request"]
        self.assertTrue(all(e["device"] and e["supervisor_id"] for e in actions))
        self.assertEqual(len({e["supervisor_id"] for e in actions}), 1)
        self.assertIn(("run-command", old.decode(), "failed"),
                      [(e["operation"], e.get("session_id"), e["outcome"]) for e in actions])

    def test_run_bound_channels(self):
        with socket.socket() as service:
            service.bind(("127.0.0.1", 0))
            service.listen()
            service.settimeout(8)
            export = "test=tcp:127.0.0.1:" + str(service.getsockname()[1])
            p = self.managed(["cat"], ["-export", export])
            old = status_line(p.stderr, b"session ").split()[1]
            forward = self.spawn(self.a + ["forward", old, "test", "127.0.0.1:0"])
            port = int(status_line(forward.stderr, b"forwarding ").split(b":")[-1])
            console = DashboardTerminal(self)
            console.until(b"R Restart")
            with socket.create_connection(("127.0.0.1", port), timeout=8) as local:
                remote, _ = service.accept()
                with remote:
                    remote.settimeout(8)
                    local.sendall(b"old")
                    self.assertEqual(remote.recv(3), b"old")
                    console.send(b"r")
                    console.until(b"Enter Restart & attach")
                    console.send(b"\r")
                    new = status_line(p.stderr, b"session ").split()[1]
                    self.assertEqual(remote.recv(1), b"")
                    self.assertEqual(local.recv(1), b"")
            self.assertNotEqual(old, new)
            self.assertNotEqual(self.run_cli(self.a + ["ls", old], check=False).returncode, 0)
            self.run_cli(self.a + ["ls", new])

    def test_lost_restart_acknowledgement(self):
        child = self.managed(["/bin/sh", "-c", 'echo $$ >> runs; echo ready; exec cat'])
        first = status_line(child.stderr, b"session ").split()[1]
        observer = self.spawn(self.b + ["attach", first])
        self.assertEqual(line(observer.stdout), b"ready\n")
        # Delay the exit acknowledgement, keeping restart in progress.
        observer.send_signal(signal.SIGSTOP)
        self.addCleanup(lambda: observer.poll() is None and observer.send_signal(signal.SIGCONT))
        console = DashboardTerminal(self)
        console.until(b"R Restart")
        console.send(b"r")
        console.until(b"Enter Restart & attach")
        console.send(b"\r")
        old_pid = int((self.work / "runs").read_text())
        deadline = time.monotonic() + 5
        while True:
            try:
                os.kill(old_pid, 0)
            except ProcessLookupError:
                break
            self.assertLess(time.monotonic(), deadline)
            time.sleep(0.01)
        console.process.kill()
        console.process.wait(5)
        observer.send_signal(signal.SIGCONT)
        second = status_line(child.stderr, b"session ").split()[1]
        self.assertNotEqual(first, second)
        retry = DashboardTerminal(self)
        retry.until(b"R Restart")
        self.assertEqual(len((self.work / "runs").read_text().splitlines()), 2)
        self.assertEqual(observer.wait(8), 128)
        # Losing the broker while attached requires an explicit reattachment.
        retry.send(b"\r")
        retry.until(b"Ctrl-G Home")
        self.daemon.terminate()
        self.daemon.wait(8)
        retry.until(b"Disconnected")
        retry.send(b"s")
        self.start_server()
        retry.send(b"r")
        retry.until(b"R Restart")
        self.assertEqual(json.loads(self.run_cli(self.a).stdout)[0]["ID"].encode(), second)
        active_pid = int((self.work / "runs").read_text().splitlines()[-1])
        child.terminate()
        self.assertEqual(child.wait(8), 0)
        with self.assertRaises(ProcessLookupError):
            os.kill(active_pid, 0)

    def test_activity_log(self):
        log = self.base / "server/server.log"
        self.check_activity(log)
        console = self.console.read_bytes().splitlines(keepends=True)
        self.assertEqual(b"".join(row for row in console if row.startswith(b"{")), log.read_bytes())

    def test_closed_console(self):
        self.daemon.terminate()
        self.daemon.wait(8)
        self.daemon = self.spawn(self.server + ["-listen", self.address])
        status_line(self.daemon.stderr, b"serving QUIC")
        self.daemon.stderr.close()
        self.run_cli(self.a + ["list"])
        self.daemon.terminate()
        self.assertEqual(self.daemon.wait(8), 0)
        journal = (self.base / "server/server.log").read_bytes().splitlines()
        self.assertEqual(json.loads(journal[-1])["event"], "daemon_stop")

    def test_log_file_unavailable(self):
        self.daemon.terminate()
        self.daemon.wait(8)
        path = self.base / "server/server.log"
        path.rename(path.with_suffix(".saved"))
        path.mkdir()
        self.check_stderr_activity(fallback=True)

    def test_activity_stderr(self):
        self.daemon.terminate()
        self.daemon.wait(8)
        self.check_stderr_activity(fallback=False)

    def check_stderr_activity(self, fallback):
        flags = [] if fallback else ["-no-log-file"]
        log = self.base / "stderr.log"
        with log.open("wb") as output:
            self.daemon = self.spawn(self.server + flags + ["-listen", self.address], stderr=output)
        deadline = time.monotonic() + 8
        while b"serving QUIC" not in log.read_bytes():
            self.assertLess(time.monotonic(), deadline)
            time.sleep(0.02)
        self.check_activity(log)
        self.assertEqual(b"writing events to stderr" in log.read_bytes(), fallback)

    def check_activity(self, log):
        link = self.run_cli(self.server + ["invite"]).stdout.strip()
        private = "private-audit-marker"
        executable = self.work / (private + "-program")
        executable.symlink_to("/bin/cat")
        _, sid = self.session([], [str(executable)])
        # Attach with the supervisor's identity to exercise role attribution.
        viewer = self.spawn([CLI, "-state", self.base / "supervisor", "attach", sid])
        viewer.stdin.write((private + "-terminal\n").encode())
        self.assertEqual(line(viewer.stdout), (private + "-terminal\n").encode())
        source = self.base / "upload"
        source.write_text(private + "-contents")
        name = private + "-file"
        self.run_cli(self.a + ["put", sid, source, name])
        self.run_cli(self.a + ["get", sid, name, self.base / "download"])
        self.run_cli(self.a + ["ls", sid, "."])
        self.assertNotEqual(self.run_cli(self.a + ["put", sid, source, name], check=False).returncode, 0)
        self.run_cli(self.server + [private], check=False)
        self.daemon.terminate()
        self.assertEqual(self.daemon.wait(8), 0)
        data = log.read_bytes()
        self.assertNotIn(private.encode(), data)
        self.assertNotIn(link, data)
        diagnostics = (b"serving QUIC ", b"clistreamd: activity log unavailable")
        events = [json.loads(row) for row in data.splitlines() if not row.startswith(diagnostics)]
        self.assertEqual(events[-1]["event"], "daemon_stop")
        opened = [e["connection_id"] for e in events if e["event"] == "connection_open"]
        closed = [e for e in events if e["event"] == "connection_close"]
        self.assertCountEqual(opened, {e["connection_id"] for e in closed})
        self.assertEqual(len(opened), len(closed))
        supervisor, = [e for e in closed if e["role"] == "supervisor"]
        self.assertEqual((supervisor["session_id"], supervisor["outcome"]), (sid, "shutdown"))
        self.assertTrue(any(e["device"] == supervisor["device"] and e["role"] == "client"
                            and e["connection_id"] != supervisor["connection_id"] for e in closed))
        channels = [e for e in events if e["event"] == "channel_close" and e["session_id"] == sid]
        self.assertTrue({"fs/get", "fs/list", "fs/put"} <=
                        {e["operation"] for e in channels if e["outcome"] == "confirmed"})
        self.assertIn(("fs/put", "failed"), [(e["operation"], e["outcome"]) for e in channels])
        def stream(event):
            return event["connection_id"], event["stream_id"]
        self.assertCountEqual([stream(e) for e in channels],
                              [stream(e) for e in events if e["event"] == "channel_open"])

    def test_no_log_file(self):
        self.daemon.terminate()
        self.daemon.wait(5)
        path = self.base / "server/server.log"
        previous = path.read_bytes()
        for existing in (True, False):
            if not existing:
                path.unlink()
            self.daemon = self.spawn(self.server + ["-no-log-file", "-listen", self.address])
            status_line(self.daemon.stderr, b"serving QUIC")
            link = self.run_cli(self.server + ["invite"]).stdout.strip()
            self.run_cli(self.a + ["pair"], data=link)
            self.run_cli(self.server + ["private-stderr-marker"], check=False)
            self.daemon.terminate()
            output = self.daemon.communicate(timeout=8)[1]
            self.assertNotIn(link, output)
            self.assertNotIn(b"private-stderr-marker", output)
            self.assertIn("pair", [json.loads(row)["event"] for row in output.splitlines()])
            self.assertEqual(path.exists(), existing)
            if existing:
                self.assertEqual(path.read_bytes(), previous)

    def test_interactive_pairing_and_reconnect(self):
        def terminal(args):
            master, slave = pty.openpty()
            self.addCleanup(os.close, master)
            try:
                proc = self.spawn(args, stdin=slave, stdout=slave, stderr=slave)
            finally:
                os.close(slave)
            return master, proc

        def until(fd, marker):
            output = bytearray()
            deadline = time.monotonic() + 12
            with selectors.DefaultSelector() as sel:
                sel.register(fd, selectors.EVENT_READ)
                while marker not in output:
                    if not sel.select(max(0, deadline - time.monotonic())):
                        self.fail("Missing terminal prompt " + marker.decode())
                    output.extend(os.read(fd, 4096))
            return bytes(output)

        def displayed_link(console):
            screen = until(console, b"Ctrl-C exits.")
            self.assertIn(b"This computer only", screen)
            link = rb"(clistream://pair/v2/[A-Za-z0-9_-]+)"
            client = re.search(rb"clistream " + link, screen)[1]
            supervisor = re.search(rb"clistream-supervise " + link + rb" PROGRAM", screen)[1]
            self.assertEqual(client, supervisor)
            return client

        # Starting the daemon in a fresh terminal must immediately allow pairing.
        console, foreground = terminal([DAEMON, "-state", self.base / "fresh", "-listen", "127.0.0.1:0", "-qr=false"])
        initial = displayed_link(console)
        enrolled = self.run_cli([CLI, "-state", self.base / "first-device", initial])
        self.assertEqual(json.loads(enrolled.stdout), [])
        until(console, b'"event":"pair"')
        foreground.terminate()
        foreground.wait(5)

        duplicate = self.run_cli(self.server)
        self.assertIn(b"already running", duplicate.stdout)
        self.assertNotIn(b"clistream://", duplicate.stdout + duplicate.stderr)
        console, admin = terminal(self.server)
        first = displayed_link(console)
        os.write(console, b"p")
        fresh = displayed_link(console)
        guest = [CLI, "-state", self.base / "guest"]
        self.assertNotEqual(self.run_cli(guest + [first], check=False).returncode, 0)
        # Exercise the unquoted link exactly as a user would paste it into a shell.
        shell = shutil.which("zsh") or "/bin/sh"
        fd, viewer = terminal([shell, "-c", 'exec "$@" ' + fresh.decode(), "test", *guest])
        until(fd, b"Waiting for supervisors")
        shared = self.spawn([SUP, "--interactive", "-once", "-state", self.base / "display-supervisor", fresh, "/bin/sh", "-c", "echo first-ready; while read -r x; do echo reply-$x; done"])
        status_line(shared.stderr, b"session ")
        until(fd, b"Enter Attach")
        os.write(fd, b"\r")
        until(fd, b"first-ready")
        os.write(fd, b"shared-link-input\r")
        until(fd, b"reply-shared-link-input")
        viewer.terminate()
        viewer.wait(5)
        self.assertEqual(len(json.loads(self.run_cli(guest).stdout)), 1)
        fd, viewer = terminal(guest)
        until(fd, b"Enter Attach")
        os.write(fd, b"\r")
        until(fd, b"first-ready")
        viewer.terminate()
        viewer.wait(5)
        self.session(["-mode", "pipe"], ["/bin/sh", "-c", "echo second-ready; cat"])
        fd, viewer = terminal(guest)
        until(fd, b"Enter Attach")
        os.write(fd, b"\x1b[B\r")
        until(fd, b"-ready")
        admin.terminate()
        self.assertEqual(len(json.loads(self.run_cli(guest).stdout)), 2)

    def account(self, name):
        home = self.base / name
        config = home / "config"
        env = dict(os.environ, HOME=str(home), XDG_CONFIG_HOME=str(config))
        if sys.platform == "darwin":
            config = home / "Library/Application Support"
        return env, config / "clistream"

    def test_shared_default_identity(self):
        output = self.run_cli([DAEMON, "-state", self.base / "server", "invite"]).stdout.splitlines()
        self.assertEqual(len(output), 1)
        link = output[0]
        for first in ("client", "supervisor", "concurrent"):
            with self.subTest(first=first):
                env, _ = self.account(first)
                before = set(self.run_cli(self.server + ["devices"]).stdout.splitlines())
                cli = [CLI, link]
                sup = [SUP, "-once", link, "true"]
                if first == "concurrent":
                    with concurrent.futures.ThreadPoolExecutor(2) as pool:
                        jobs = [pool.submit(self.run_cli, cmd, env=env) for cmd in (cli, sup)]
                        for job in jobs:
                            job.result()
                else:
                    self.run_cli(cli if first == "client" else sup, env=env)
                self.run_cli([CLI], env=env)
                self.run_cli([SUP, "-once", "true"], env=env)
                after = set(self.run_cli(self.server + ["devices"]).stdout.splitlines())
                self.assertEqual(len(after - before), 1)
                other = self.run_cli([CLI, "-state", self.base / "isolated", "list"],
                                     env=env, check=False)
                self.assertNotEqual(other.returncode, 0)

    def test_legacy_profiles(self):
        link = self.run_cli(self.server + ["invite"]).stdout.strip()
        for profiles in (("client",), ("supervisor",), ("client", "supervisor")):
            env, root = self.account("-".join(profiles))
            for role in profiles:
                before = set(self.run_cli(self.server + ["devices"]).stdout.splitlines())
                self.run_cli([CLI, "-state", root / role, link])
                after = set(self.run_cli(self.server + ["devices"]).stdout.splitlines())
                enrolled = (after - before).pop().split()[0].decode()
            self.run_cli([CLI], env=env)
            self.run_cli([SUP, "-once", "true"], env=env)
            if len(profiles) == 2:
                # Revoking the supervisor key proves which identity both tools use.
                key = (root / "supervisor/key.pem").read_bytes()
                self.assertNotEqual(key, (root / "client/key.pem").read_bytes())
                devices = json.loads((self.base / "server/devices.json").read_text())
                # Emulate an upgrade with real, already-enrolled keys.
                self.daemon.terminate()
                self.daemon.wait(5)
                for index, pin in enumerate(devices):
                    devices[pin] = ("supervisor", "client")[index % 2]
                (self.base / "server/devices.json").write_text(json.dumps(devices))
                self.start_server()
                self.run_cli(self.server + ["revoke", enrolled])
                self.run_cli([CLI], env=env)
                self.run_cli([SUP, "-once", "true"], env=env)
                self.assertEqual(key, (root / "supervisor/key.pem").read_bytes())

    def test_conflicting_default_servers(self):
        env, root = self.account("conflicting")
        second = [DAEMON, "-state", self.base / "second"]
        daemon = self.spawn(second + ["-listen", "127.0.0.1:0"])
        status_line(daemon.stderr, b"serving QUIC")
        a = self.run_cli(self.server + ["invite"]).stdout.strip()
        b = self.run_cli(second + ["invite"]).stdout.strip()
        self.run_cli([CLI, "-state", root / "client", a])
        self.run_cli([SUP, "-once", "-state", root / "supervisor", b, "true"])
        proc = self.spawn([SUP, "--interactive", "-once", "cat"], env=env)
        sid = status_line(proc.stderr, b"session ").split()[1].decode()
        self.assertFalse(json.loads(self.run_cli([CLI], env=env).stdout))
        self.assertEqual(json.loads(self.run_cli([CLI, b], env=env).stdout)[0]["ID"], sid)
        self.assertEqual(json.loads(self.run_cli([CLI], env=env).stdout)[0]["ID"], sid)
        self.assertFalse(json.loads(self.run_cli([CLI, "-state", root / "client"]).stdout))
        self.assertEqual(len(self.run_cli(second + ["devices"]).stdout.splitlines()), 1)

    def test_supervisor_positional_pair_and_launch(self):
        sup = [SUP, "--interactive", "-once", "-state", self.base / "new-supervisor"]
        missing = self.run_cli(sup + ["/bin/sh"], check=False)
        self.assertIn(b"clistreamd invite", missing.stderr)
        output = self.run_cli([DAEMON, "-state", self.base / "server", "supervisor"]).stdout
        self.assertEqual(len(output.splitlines()), 1)
        link = output.strip()
        self.assertTrue(link.startswith(b"clistream://pair/v2/"))
        alias = self.run_cli(self.server + ["invite", "client"]).stdout.strip()
        self.run_cli(sup + [alias, "true"])
        self.assertNotEqual(self.run_cli(sup + [link], check=False).returncode, 0)
        self.assertNotEqual(self.run_cli(sup + [link, "/no/such/command"], check=False).returncode, 0)
        program = 'test ! -t 0 && echo ready; read -r data; echo "$1:$data"; echo error >&2; exit 7'
        proc = self.spawn(sup + [link, "/bin/sh", "-c", program, "sh", "--child flag"])
        sid = status_line(proc.stderr, b"session ").split()[1]
        viewer = self.spawn(self.a + ["attach", sid])
        self.assertEqual(line(viewer.stdout), b"ready\n")
        viewer.stdin.write(b"ping\n")
        self.assertEqual(line(viewer.stdout), b"--child flag:ping\n")
        self.assertEqual(line(viewer.stderr), b"error\n")
        self.assertEqual(viewer.wait(8), 7)
        self.assertEqual(proc.wait(8), 7)
        self.run_cli(sup + [link, "true"])

        master, slave = pty.openpty()
        self.addCleanup(os.close, master)
        try:
            proc = self.spawn(sup + ["sh", "-c", "test -t 0 && echo terminal-ready; read reply; echo received-$reply"],
                              stdin=slave, stdout=slave)
        finally:
            os.close(slave)
        sid = status_line(proc.stderr, b"session ").split()[1]
        viewer = self.spawn(self.a + ["attach", sid])
        self.assertEqual(line(viewer.stdout), b"terminal-ready\r\n")
        viewer.stdin.write(b"hello\n")
        output, _ = viewer.communicate(timeout=8)
        self.assertIn(b"received-hello\r\n", output)
        self.assertEqual(proc.wait(8), 0)

    def test_shared_session(self):
        p, sid = self.session(
            [],
            [
                "/bin/sh",
                "-c",
                'echo ready; while read -r line; do echo "out:$line"; echo "err:$line" >&2; done; exit 7',
            ],
        )
        viewers = [self.spawn(u + ["attach", sid]) for u in (self.a, self.b)]
        for v in viewers:
            self.assertEqual(line(v.stdout), b"ready\n")
        for source, word in [
            (viewers[0], "first"),
            (viewers[1], "second"),
            (p, "local"),
        ]:
            source.stdin.write((word + "\n").encode())
            for v in viewers:
                self.assertEqual(line(v.stdout), f"out:{word}\n".encode())
                self.assertEqual(line(v.stderr), f"err:{word}\n".encode())
        p.stdin.close()
        for v in viewers:
            self.assertEqual(v.wait(10), 7, v.stderr.read())
        self.assertEqual(p.wait(10), 7)
        self.daemon.terminate()
        self.daemon.wait(8)
        events = [json.loads(row) for row in (self.base / "server/server.log").read_text().splitlines()]
        exits = [e for e in events if e.get("outcome") == "exited"]
        self.assertCountEqual([(e["operation"], e["exit_code"]) for e in exits],
                              [("stdout", 7), ("stderr", 7)] * 2)

    def test_pty(self):
        _, sid = self.session(
            ["-mode", "pty"],
            [
                "/bin/sh",
                "-c",
                "stty -echo; echo ready; read v; printf '\\033[32m%s\\033[0m\\n' \"$v\"",
            ],
        )
        v = self.spawn(self.a + ["attach", sid])
        self.assertEqual(line(v.stdout), b"ready\r\n")
        v.stdin.write(b"hello\n")
        self.assertEqual(line(v.stdout), b"\x1b[32mhello\x1b[0m\r\n")
        self.assertEqual(v.wait(8), 0)

    def test_keystroke_latency(self):
        _, sid = self.session(
            ["-mode", "pty"], ["/bin/sh", "-c", "stty raw -echo; echo ready; exec cat"]
        )
        remote = self.spawn(self.a + ["attach", sid])
        self.assertIn(b"ready", line(remote.stdout))
        samples = []
        with selectors.DefaultSelector() as sel:
            sel.register(remote.stdout, selectors.EVENT_READ)
            for _ in range(21):
                t = time.monotonic_ns()
                remote.stdin.write(b"x")
                self.assertTrue(sel.select(3))
                self.assertEqual(remote.stdout.read(1), b"x")
                samples.append((time.monotonic_ns() - t) / 1e6)
        print(
            f"loopback median single-key echo {statistics.median(samples):.2f} ms",
            flush=True,
        )

    def test_pty_resize(self):
        _, sid = self.session(
            ["-mode", "pty"],
            ["/bin/sh", "-c", "stty -echo; echo ready; read trigger; stty size"],
        )
        master, slave = os.openpty()
        self.addCleanup(os.close, master)
        self.addCleanup(os.close, slave)
        import fcntl
        import struct

        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 43, 109, 0, 0))
        remote = self.spawn(self.a + ["attach", sid], stdin=slave)
        self.assertEqual(line(remote.stdout), b"ready\r\n")
        os.write(master, b"go\n")
        self.assertEqual(line(remote.stdout), b"43 109\r\n")
        self.assertEqual(remote.wait(8), 0)

    def test_files(self):
        _, sid = self.session([], ["/bin/cat"])
        viewer = self.spawn(self.b + ["attach", sid])
        def echo(marker):
            viewer.stdin.write(marker)
            self.assertEqual(line(viewer.stdout), marker)
        echo(b"before-workspace-change\n")
        data = os.urandom(180015)
        source = self.base / "source"
        source.write_bytes(data)
        (self.work / "folder").mkdir()
        (self.work / "escape").symlink_to(self.base)
        os.link(source, self.work / "hardlink")
        self.run_cli(self.a + ["put", sid, source, "folder/file"])
        target = self.base / "copy"
        self.run_cli(self.b + ["get", sid, "folder/file", target])
        self.assertEqual(target.read_bytes(), data)
        files = self.run_cli(self.b + ["ls", sid, "folder"]).stdout
        self.assertEqual(json.loads(files)["Name"], "file")
        (self.work / "folder" / "local-change").touch()
        refreshed = self.run_cli(self.b + ["ls", sid, "folder"]).stdout.splitlines()
        self.assertEqual({json.loads(row)["Name"] for row in refreshed}, {"file", "local-change"})
        echo(b"after-workspace-change\n")
        for name in ["../source", "/etc/passwd", "escape/source", "hardlink"]:
            self.assertNotEqual(
                self.run_cli(
                    self.a + ["get", sid, name, self.base / "forbidden"], check=False
                ).returncode,
                0,
            )
        for name in ["../outside", "escape/outside", "folder/file"]:
            self.assertNotEqual(
                self.run_cli(
                    self.a + ["put", sid, source, name], check=False
                ).returncode,
                0,
            )
        self.assertFalse((self.base / "outside").exists())

    def test_ports(self):
        with contextlib.ExitStack() as stack:
            socks = {}
            exports = []
            for proto, kind in [
                ("tcp", socket.SOCK_STREAM),
                ("udp", socket.SOCK_DGRAM),
            ]:
                s = stack.enter_context(socket.socket(type=kind))
                s.bind(("127.0.0.1", 0))
                exports += [
                    "-export",
                    f"private-export-{proto}={proto}:127.0.0.1:{s.getsockname()[1]}",
                ]
                socks[proto] = s
            tcp = socks["tcp"]
            tcp.listen()

            def echo_tcp():
                conn, _ = tcp.accept()
                with conn:
                    data = bytearray()
                    while chunk := conn.recv(4096):
                        data.extend(chunk)
                    conn.sendall(data)

            def echo_udp():
                for _ in range(3):
                    data, addr = socks["udp"].recvfrom(65535)
                    socks["udp"].sendto(data, addr)

            for f in (echo_tcp, echo_udp):
                threading.Thread(target=f, daemon=True).start()
            _, sid = self.session(exports, ["/bin/cat"])
            refused = self.run_cli(
                self.a + ["forward", sid, "unlisted", "127.0.0.1:0"], check=False
            )
            self.assertNotEqual(refused.returncode, 0)
            for proto, kind in [
                ("tcp", socket.SOCK_STREAM),
                ("udp", socket.SOCK_DGRAM),
            ]:
                v = self.spawn(self.a + ["forward", sid, "private-export-" + proto, "127.0.0.1:0"])
                addr = (
                    status_line(v.stderr, b"forwarding ")
                    .decode()
                    .split()[-1]
                    .split(":")
                )
                with socket.socket(type=kind) as sock:
                    sock.settimeout(8)
                    sock.connect((addr[0], int(addr[1])))
                    if proto == "tcp":
                        sock.sendall(b"data")
                        sock.shutdown(socket.SHUT_WR)
                        self.assertEqual(sock.recv(32), b"data")
                    else:
                        for data in [b"", b"x", os.urandom(1400)]:
                            sock.send(data)
                            self.assertEqual(sock.recv(65535), data)

        self.daemon.terminate()
        self.assertEqual(self.daemon.wait(8), 0)
        data = (self.base / "server/server.log").read_text()
        self.assertNotIn("private-export-", data)
        events = [json.loads(row) for row in data.splitlines()]
        self.assertEqual({e["operation"] for e in events if e["event"] == "channel_close"},
                         {"port/tcp", "port/udp"})

    def test_supervisor_activity_default(self):
        state = self.base / "supervisor"
        script = ('if (exec 9<>/dev/tty) 2>/dev/null; then exit 8; fi; '
                  'echo private-output; echo private-error >&2; read value || exit 9; echo "reply-$value"')
        proc = self.spawn([SUP, "-state", state, "/bin/sh", "-c", script], cwd=self.work)
        proc.stdin.write(b"private-local-input\n")
        proc.stdin.close()
        self.assertIn(b"Activity log", line(proc.stderr))
        with (self.base / "feed").open("wb") as feed:
            while True:
                msg = line(proc.stderr)
                self.assertTrue(msg)
                feed.write(msg)
                if b"Child started" in msg:
                    break
        deadline = time.monotonic() + 5
        while not (sessions := json.loads(self.run_cli(self.a + ["list"]).stdout)):
            self.assertLess(time.monotonic(), deadline)
            time.sleep(0.03)
        sid = sessions[0]["ID"]
        file = self.work / "private-filename"
        file.write_bytes(b"private-content")
        self.run_cli(self.a + ["get", sid, file.name, self.base / "copy"])
        viewer = self.spawn(self.a + ["attach", sid])
        out, err = viewer.communicate(b"private-remote-input\n", timeout=8)
        self.assertEqual(viewer.returncode, 0)
        self.assertIn(b"private-output", out)
        self.assertIn(b"reply-private-remote-input", out)
        self.assertIn(b"private-error", err)
        proc.terminate()
        self.assertEqual(proc.wait(8), 0)
        self.assertEqual(proc.stdout.read(), b"")
        feed = (self.base / "feed").read_bytes() + proc.stderr.read()
        self.assertIn(b"File download completed", feed)
        self.assertIn(b"Child exited (status 0)", feed)
        journal, = (state / "logs").glob("supervisor-*.log")
        self.assertEqual(journal.stat().st_mode & 0o777, 0o600)
        data = journal.read_bytes()
        self.assertNotIn(b"private-", data + feed)
        events = [json.loads(row) for row in data.splitlines()]
        self.assertEqual({e["supervisor_id"] for e in events}, {journal.stem.removeprefix("supervisor-")})
        self.assertTrue(any(e["event"] == "channel" and e["operation"] == "fs/get" and
                            e["outcome"] == "completed" and e["session_id"] == sid for e in events))

    def test_concurrent_supervisor_journals(self):
        state = self.base / "supervisor"
        procs = [self.spawn([SUP, "-state", state, "-once", "/bin/cat"]) for _ in range(2)]
        for proc in procs:
            while b"Child started" not in (msg := line(proc.stderr)):
                self.assertTrue(msg)
        self.assertEqual(len(json.loads(self.run_cli(self.a + ["list"]).stdout)), 2)
        for proc in procs:
            proc.terminate()
            proc.wait(8)
        paths = list((state / "logs").glob("*.log"))
        self.assertEqual(len(paths), 2)
        for path in paths:
            events = [json.loads(row) for row in path.read_text().splitlines()]
            self.assertEqual({e["supervisor_id"] for e in events}, {path.stem.removeprefix("supervisor-")})
            self.assertEqual(sum(e["event"] == "child_exited" for e in events), 1)

    def test_supervisor_activity_terminal(self):
        state = self.base / "supervisor"
        ui = DashboardTerminal(self, [SUP, "-once", "-state", state, "/bin/sh", "-c",
                                      'test -t 0 || exit 9; echo private-output; read x; echo "reply-$x"'])
        start = ui.until(b"Child started")
        ui.send(b"private-local-input\r")
        sid, = json.loads(self.run_cli(self.a + ["list"]).stdout)
        child = self.spawn(self.a + ["attach", sid["ID"]])
        self.assertIn(b"private-output", line(child.stdout))
        child.stdin.write(b"remote\n")
        output, _ = child.communicate(timeout=8)
        self.assertIn(b"reply-remote", output)
        self.assertNotIn(b"private-local-input", output)
        finish = ui.until(b"Child exited")
        self.assertNotIn(b"private-", start + finish)
        self.assertNotIn(b"\x1b[?1049h", start + finish)
        self.assertEqual(ui.process.wait(8), 0)

    def test_supervisor_journal_unavailable(self):
        state = self.base / "supervisor"
        (state / "logs").write_text("not a directory")
        result = self.run_cli([SUP, "-state", state, "-once", "/bin/echo", "private-output"])
        self.assertEqual(result.stdout, b"")
        self.assertIn(b"log unavailable", result.stderr)
        self.assertIn(b"Child exited (status 0)", result.stderr)
        self.assertNotIn(b"private-output", result.stderr)

    def test_supervisor_console_reconnect(self):
        self.daemon.terminate()
        self.daemon.wait(8)
        launches = self.base / "launches"
        script = 'echo $$ >> "$1"; printf "\\033[2J\\033[H"; echo LOCAL-READY; while read v; do [ "$v" = end ] && exit 23; echo "reply:$v"; done'
        console = DashboardTerminal(self, [SUP, "--interactive", "-state", self.base / "supervisor",
                                           "/bin/sh", "-c", script, "sh", launches])
        console.until(b"CLIStream Supervisor")
        console.until(b"Reconnecting")
        self.assertFalse(launches.exists())
        self.start_server()
        console.until(b"LOCAL-READY")
        first, = json.loads(self.run_cli(self.a + ["list"]).stdout)
        for attempt in range(2):
            self.daemon.terminate()
            self.daemon.wait(8)
            console.until(b"Reconnecting")
            word = f"offline-{attempt}".encode()
            console.send(word + b"\r")
            console.until(b"reply:" + word)
            if attempt == 1:
                console.send(b"end\r")
                console.until(b"exit 23")
            journal = self.base / "server/server.log"
            offset = journal.stat().st_size
            self.start_server()
            deadline = time.monotonic() + 15
            while b'"event":"supervisor_publish"' not in journal.read_bytes()[offset:]:
                self.assertLess(time.monotonic(), deadline, "supervisor failed to republish")
                time.sleep(0.05)
            sessions = json.loads(self.run_cli(self.a + ["list"]).stdout) or []
            if attempt == 0:
                self.assertEqual([item["ID"] for item in sessions], [first["ID"]])
                viewer = self.spawn(self.a + ["attach", first["ID"]])
                viewer.stdin.write(b"remote\n")
                console.until(b"reply:remote")
                viewer.terminate()
                viewer.wait(8)
            else:
                self.assertFalse(sessions)
        journal, = (self.base / "supervisor/logs").glob("*.log")
        events = [json.loads(row)["event"] for row in journal.read_text().splitlines()]
        self.assertEqual(events.count("child_started"), 1)
        self.assertEqual(events.count("registered"), 3)
        self.assertEqual(len(launches.read_text().splitlines()), 1)
        console.send(b"\x03")
        self.assertEqual(console.process.wait(8), 0)

    def test_supervisor_console_input(self):
        ui = DashboardTerminal(self, [*self.supervisor, sys.executable, "-c", PTY_INPUT_PROBE])
        ui.until(b"VIEWPORT")
        ui.send(b"s\x03\x1b[200~q\x1ds\x1b[201~\x1b[<0;5;8M!")
        ui.until(b"SIZE=100,22")
        sid = json.loads(self.run_cli(self.a + ["list"]).stdout)[0]["ID"]
        remote = self.spawn(self.a + ["attach", sid])
        output = bytearray()
        while b"SIZE=100,22" not in output:
            packet = line(remote.stdout)
            self.assertTrue(packet, "child exited before reporting its viewport size")
            output.extend(packet)
        ui.send(b"x")
        remote.communicate(timeout=8)
        self.assertIn(b"[73][03][71][1d][73]", output)
        self.assertIn(b"[1b][5b][3c][30][3b][35][3b][36][4d]", output)
        self.assertEqual(ui.process.wait(8), 19)

    def test_supervisor_console_read_only_restart(self):
        seen = self.base / "seen"
        script = 'echo WATCHING; while read word; do echo "$word" >> "$1"; done'
        local = DashboardTerminal(self, [SUP, "--interactive", "-local-input=false", "-state", self.base / "supervisor",
                                         "/bin/sh", "-c", script, "sh", seen])
        local.until(b"WATCHING")
        local.send(b"ignored\r")
        sid = json.loads(self.run_cli(self.a + ["list"]).stdout)[0]["ID"]
        viewer = self.spawn(self.a + ["attach", sid])
        viewer.stdin.write(b"remote-input\n")
        deadline = time.monotonic() + 5
        while not seen.exists() or not seen.read_bytes():
            self.assertLess(time.monotonic(), deadline)
            time.sleep(0.03)
        self.assertEqual(seen.read_text(), "remote-input\n")
        control = DashboardTerminal(self, self.b)
        control.until(b"R Restart")
        control.send(b"r")
        control.until(b"Enter Restart & attach")
        control.send(b"\x15/bin/cat\r")
        control.until(b"Ctrl-G Home")
        control.send(b"replacement-visible\r")
        local.until(b"replacement-visible")
        self.assertEqual(viewer.wait(8), 128)
        local.send(b"\x03")
        self.assertEqual(local.process.wait(8), 0)
        journal, = (self.base / "supervisor/logs").glob("*.log")
        events = [json.loads(row) for row in journal.read_bytes().splitlines()]
        runs = {e["session_id"] for e in events if e["event"] == "child_started"}
        self.assertEqual(len(runs), 2)
        self.assertIn(sid, runs)
        self.assertTrue(any(e["event"] == "remote_request" and e["operation"] == "run-command"
                            and e["outcome"] == "completed" for e in events))
        self.assertNotIn(b"replacement-visible", journal.read_bytes())

    def test_supervisor_refuses_wrong_pin(self):
        path = self.base / "supervisor/server.json"
        profile = json.loads(path.read_text())
        profile["Pin"] = "0" * 64
        path.write_text(json.dumps(profile))
        for flags in ([], ["-once"]):
            result = self.run_cli([SUP, "-state", path.parent, *flags, "/bin/true"], check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b"mismatch", result.stderr)
            self.assertNotIn(b"Reconnecting", result.stderr)

    def test_supervisor_console_attach_only(self):
        script = 'stty size; read x; stty size; read x; exit 17'
        ui = DashboardTerminal(self, [*self.supervisor, "/bin/sh", "-c", script])
        ui.until(b"22 100")
        termios.tcsetwinsize(ui.fd, (18, 73))
        os.kill(ui.process.pid, signal.SIGWINCH)
        time.sleep(0.15)
        ui.send(b"hello\r")
        ui.until(b"16 73")
        self.daemon.terminate()
        self.daemon.wait(8)
        ui.until(b"Reconnecting")
        ui.send(b"\x04")
        self.assertEqual(ui.process.wait(5), 17)

    def test_reconnect(self):
        target, old_id = self.session([], ["/bin/cat"])
        target.stdin.write(b"before-restart\n")
        self.assertEqual(line(target.stdout), b"before-restart\n")
        self.daemon.terminate()
        self.daemon.wait(5)
        self.start_server()
        # A lost close packet can defer recovery until QUIC's one-minute idle timeout.
        deadline = time.monotonic() + 75
        sessions = []
        while time.monotonic() < deadline:
            result = self.run_cli(self.a + ["list"], check=False)
            if result.returncode:
                self.assertIn(b"timeout", result.stderr)
                continue
            sessions = json.loads(result.stdout) or []
            if sessions:
                break
            time.sleep(0.1)
        self.assertTrue(sessions)
        new_id = sessions[0]["ID"]
        self.assertNotEqual(new_id, old_id)
        remote = self.spawn(self.a + ["attach", new_id])
        self.assertEqual(line(remote.stdout), b"before-restart\n")
        self.daemon.terminate()
        self.daemon.wait(8)
        events = [json.loads(row) for row in (self.base / "server/server.log").read_text().splitlines()]
        registrations = [e for e in events if e["event"] == "session_register" and e["outcome"] == "ok"]
        self.assertEqual([e["session_id"] for e in registrations], [old_id, new_id])
        self.assertEqual(len({e["device"] for e in registrations}), 1)
        self.assertEqual(len({e["connection_id"] for e in registrations}), 2)

    def supported_executable(self, name):
        banners = {"claude": b"2.1.329 (Claude Code)", "codex": b"codex-cli 0.170.0"}
        executable = shutil.which(name)
        if (
            executable
            and self.run_cli([executable, "--version"], check=False).stdout.strip()
            == banners[name]
        ):
            return executable
        return None

    def test_driver_catalog(self):
        unpaired = self.base / "unused-profile"
        result = self.run_cli([SUP, "-once", "-state", unpaired, "-drivers"])
        self.assertFalse(unpaired.exists())
        drivers = json.loads(result.stdout)
        for name, version, default in [
            ("generic", None, "pipe"),
            ("codex", "0.170.0", "pty"),
            ("claude", "2.1.329", "pty"),
        ]:
            release = next(r for r in drivers[name] if r.get("version") == version)
            self.assertEqual(release["defaultMode"], default)
            self.assertEqual(set(release["modes"]), {"pipe", "pty"})

    def test_unsupported_version_and_generic_override(self):
        exe = self.work / "codex"
        exe.write_text(
            "#!/bin/sh\nif test \"$1\" = --version; then echo 'codex-cli 0.0.0'; else exec cat; fi\n"
        )
        exe.chmod(0o700)
        refused = self.run_cli(self.supervisor + ["-driver", "codex", "--", exe], check=False)
        self.assertNotEqual(refused.returncode, 0)
        self.assertIn(b"-driver generic", refused.stderr)
        _, sid = self.session(["-local-input=false"], [exe])
        item = next(
            x
            for x in json.loads(self.run_cli(self.a + ["list"]).stdout)
            if x["ID"] == sid
        )
        self.assertEqual((item["Driver"], item["Mode"]), ("generic", "pipe"))
        remote = self.spawn(self.a + ["-eof", "attach", sid])
        stdout, _ = remote.communicate(b"override works\n", timeout=8)
        self.assertEqual((remote.returncode, stdout), (0, b"override works\n"))

    def test_default_launch_never_probes_named_programs(self):
        for name in ("claude", "codex", "my-cli"):
            with self.subTest(name=name):
                exe = self.work / name
                exe.write_text('#!/bin/sh\nprintf "%s\\n" "$@" >> "$0.log"\nprintf "%s" "$1"\n')
                exe.chmod(0o700)
                result = self.run_cli(self.supervisor + [exe, "--my-argument"])
                self.assertEqual(result.stdout, b"--my-argument")
                self.assertEqual(pathlib.Path(str(exe) + ".log").read_text(), "--my-argument\n")

    def test_interactive_driver_terminals(self):
        tested = []
        for name in ("claude", "codex"):
            executable = self.supported_executable(name)
            if not executable:
                continue
            with self.subTest(name=name):
                config = self.base / (name + "-profile")
                config.mkdir()
                env = {
                    **os.environ,
                    "TERM": "xterm-256color",
                    "CLAUDE_CONFIG_DIR": str(config),
                    "CODEX_HOME": str(config),
                    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
                }
                _, sid = self.session(["-driver", name, "-local-input=false"], [executable], env=env)
                row = next(
                    s
                    for s in json.loads(self.run_cli(self.a + ["list"]).stdout)
                    if s["ID"] == sid
                )
                self.assertEqual((row["Driver"], row["Mode"]), (name, "pty"))
                remote = self.spawn(self.a + ["attach", sid])
                output = bytearray()
                until = time.monotonic() + 20
                with selectors.DefaultSelector() as sel:
                    sel.register(remote.stdout, selectors.EVENT_READ)
                    while name.encode() not in output.lower() or b"\x1b[" not in output:
                        self.assertTrue(
                            sel.select(max(0, until - time.monotonic())),
                            "missing interactive startup screen",
                        )
                        packet = os.read(remote.stdout.fileno(), 8192)
                        self.assertTrue(
                            packet, "interactive CLI closed before rendering"
                        )
                        output.extend(packet)
                        if b"\x1b[6n" in packet:
                            remote.stdin.write(b"\x1b[1;1R")
                remote.stdin.write(b"\x03")
                tested.append(name)
        if not tested:
            self.skipTest("supported CLI executables unavailable")

    def test_detected_drivers(self):
        found = 0
        for name in ("claude", "codex"):
            executable = self.supported_executable(name)
            if executable:
                r = self.run_cli(
                    self.supervisor
                    + ["-driver", name, "-local-input=false", "--", executable, "--version"]
                )
                self.assertIn(b"session ", r.stderr)
                self.assertTrue(r.stdout.strip())
                found += 1
        if not found:
            self.skipTest("Supported Claude Code and Codex CLI versions unavailable")

    def remote_task(self, name, args, prompt, expected, **options):
        executable = shutil.which(name)
        self.assertIsNotNone(executable, name + " installation required")
        proc, sid = self.session(
            ["-driver", name, "-local-input=false"], [executable, *args], **options
        )
        items = json.loads(self.run_cli(self.a + ["list"]).stdout)
        self.assertEqual(next(s["Driver"] for s in items if s["ID"] == sid), name)
        viewer = self.spawn(self.a + ["-eof", "attach", sid])
        output, diagnostics = viewer.communicate(prompt, timeout=120)
        self.assertEqual(viewer.returncode, 0, (output[-500:], diagnostics[-500:]))
        self.assertEqual(output.strip(), expected)
        self.assertEqual(proc.wait(5), 0)

    @unittest.skipUnless(
        os.environ.get("CLISTREAM_LIVE_DRIVERS") == "1",
        "requires authenticated Codex CLI",
    )
    def test_live_codex_task(self):
        self.remote_task(
            "codex",
            CODEX_ARGS,
            b"Compute 713 times 29. Reply with just the integer. Do not use tools.\n",
            b"20677",
        )

    @unittest.skipUnless(
        os.environ.get("CLISTREAM_LIVE_CLAUDE") == "1", "requires Claude provider quota"
    )
    def test_live_claude_task(self):
        self.remote_task(
            "claude",
            ["-p", "--model", "sonnet", "--tools", "", "--no-session-persistence"],
            b"Compute 713 times 29. Reply with just the integer. Do not use tools.\n",
            b"20677",
        )

    def test_claude_with_emulated_provider(self):
        if not self.supported_executable("claude"):
            self.skipTest("requires the supported Claude Code version")
        with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Messages) as service:
            service.challenge = os.urandom(12).hex()
            service.answer = "accepted-" + os.urandom(12).hex()
            service.seen = threading.Event()
            thread = threading.Thread(target=service.serve_forever, daemon=True)
            thread.start()
            env = claude_environment(self.base / "claude", service)
            try:
                self.remote_task(
                    "claude",
                    CLAUDE_ARGS,
                    service.challenge.encode() + b"\n",
                    service.answer.encode(),
                    env=env,
                )
                self.assertTrue(service.seen.is_set())
            finally:
                service.shutdown()
                thread.join(3)

    def test_retry_lost_handshake(self):
        with NetworkPath(int(self.address.split(":")[1]), drop_first_peer=True) as path:
            profile = self.base / "a/server.json"
            config = json.loads(profile.read_text())
            config["Address"] = path.address
            profile.write_text(json.dumps(config))
            self.run_cli(self.a + ["list"])
            self.assertGreater(path.dropped, 0)

    def test_network_path(self):
        target, sid = self.session([], ["/bin/cat"])
        with NetworkPath(int(self.address.split(":")[1])) as path:
            profile = self.base / "a/server.json"
            config = json.loads(profile.read_text())
            config["Address"] = path.address
            profile.write_text(json.dumps(config))
            viewer = self.spawn(self.a + ["attach", sid])
            viewer.stdin.write(b"before-rebinding\n")
            self.assertEqual(line(viewer.stdout), b"before-rebinding\n")
            path.rebind.set()
            self.assertTrue(path.rebound.wait(3))
            end = time.monotonic() + float(
                os.environ.get("CLISTREAM_SOAK_SECONDS", "2")
            )
            seq = 0
            while time.monotonic() < end or seq < 40:
                message = f"{seq:09d}:".encode() + b"x" * 256 + b"\n"
                viewer.stdin.write(message)
                self.assertEqual(line(viewer.stdout), message)
                self.assertEqual(
                    line(target.stdout), message if seq else b"before-rebinding\n"
                )
                if seq == 0:
                    self.assertEqual(line(target.stdout), message)
                seq += 1
            self.assertGreater(path.dropped, 0)
            print(
                f"verified {seq} round trips, {path.dropped} dropped datagrams and a changed source port",
                flush=True,
            )

    def test_detach_and_replay(self):
        target, sid = self.session(
            ["-mode", "pty"],
            ["/bin/sh", "-c", "stty -echo; echo ready; read v; echo result-$v"],
        )
        master, slave = os.openpty()
        self.addCleanup(os.close, master)
        self.addCleanup(os.close, slave)
        client = self.spawn(self.a + ["attach", sid], stdin=slave)
        self.assertEqual(line(client.stdout), b"ready\r\n")
        os.write(master, bytes([0x1D]))
        self.assertEqual(client.wait(8), 0)
        self.assertIsNone(target.poll())
        second = self.spawn(self.b + ["attach", sid])
        self.assertEqual(line(second.stdout), b"ready\r\n")
        second.stdin.write(b"joined\n")
        self.assertEqual(line(second.stdout), b"result-joined\r\n")
        self.assertEqual(second.wait(8), 0)

    def test_large_binary_stdio(self):
        target, sid = self.session(["-local-input=false"], ["/bin/cat"])
        local = bytearray()
        drain = threading.Thread(
            target=lambda: local.extend(target.stdout.read()), daemon=True
        )
        drain.start()
        viewer = self.spawn(self.a + ["-eof", "attach", sid])
        data = os.urandom(4 * 1024 * 1024)
        output, diagnostics = viewer.communicate(data, timeout=40)
        self.assertEqual(viewer.returncode, 0, diagnostics)
        self.assertTrue(output == data, "binary stream differs")
        self.assertEqual(diagnostics, b"")
        drain.join(5)
        self.assertTrue(local == data, "local output differs")

    def test_active_revocation(self):
        _, sid = self.session([], ["/bin/sh", "-c", "echo ready; exec cat"])
        before = set(self.run_cli(self.server + ["devices"]).stdout.splitlines())
        guest = [CLI, "-state", self.base / "guest"]
        link = self.run_cli(self.server + ["invite"]).stdout
        self.run_cli(guest + ["pair"], data=link)
        new = set(self.run_cli(self.server + ["devices"]).stdout.splitlines()) - before
        self.assertEqual(len(new), 1)
        revoked = self.spawn(guest + ["attach", sid])
        retained = self.spawn(self.b + ["attach", sid])
        self.assertEqual(line(revoked.stdout), b"ready\n")
        self.assertEqual(line(retained.stdout), b"ready\n")
        self.run_cli(self.server + ["revoke", new.pop().split()[0]])
        self.assertNotEqual(revoked.wait(10), 0)
        self.assertNotEqual(self.run_cli(guest + ["list"], check=False).returncode, 0)
        self.assertIn(b"no longer authorized", self.run_cli(guest, check=False).stderr)
        self.run_cli(guest + ["pair"], data=link)
        self.run_cli(guest + ["list"])
        retained.stdin.write(b"still-enrolled\n")
        self.assertEqual(line(retained.stdout), b"still-enrolled\n")
        self.daemon.terminate()
        self.daemon.wait(8)
        events = [json.loads(row) for row in (self.base / "server/server.log").read_text().splitlines()]
        self.assertTrue(any(e["event"] == "connection_close" and e["outcome"] == "revoked" for e in events))

    def test_concurrent_uploads(self):
        _, sid = self.session([], ["/bin/cat"])
        writers = []
        for i, client in enumerate((self.a, self.b)):
            path = self.base / f"upload-{i}"
            data = os.urandom(256001)
            path.write_bytes(data)
            writers.append((self.spawn(client + ["put", sid, path, "winner"]), data))
        winners = [data for writer, data in writers if writer.wait(12) == 0]
        self.assertEqual(len(winners), 1)
        self.assertTrue((self.work / "winner").read_bytes() == winners[0])
        self.assertEqual(list(self.work.glob(".clistream-upload-*")), [])

    def test_authorization(self):
        unknown = self.base / "stranger"
        unknown.mkdir(mode=0o700)
        shutil.copy(self.base / "a" / "server.json", unknown / "server.json")
        self.assertNotEqual(
            self.run_cli([CLI, "-state", unknown, "list"], check=False).returncode, 0
        )
        profile = json.loads((unknown / "server.json").read_text())
        profile["Pin"] = "0" * 64
        (unknown / "server.json").write_text(json.dumps(profile))
        self.assertIn(
            b"mismatch",
            self.run_cli([CLI, "-state", unknown, "list"], check=False).stderr,
        )
        self.assertIn(b"mismatch", self.run_cli([CLI, "-state", unknown], check=False).stderr)
        self.run_cli([CLI, "-state", self.base / "supervisor", "list"])
        self.daemon.terminate()
        self.daemon.wait(5)
        self.start_server()
        self.run_cli(self.a + ["list"])
        devices = self.run_cli(self.server + ["devices"]).stdout.decode().splitlines()
        for device in devices:
            if device.endswith(" device"):
                self.run_cli(self.server + ["revoke", device.split()[0]])
        self.assertNotEqual(self.run_cli(self.a + ["list"], check=False).returncode, 0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
