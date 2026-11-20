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
        self.supervisor = [SUP, "-state", self.base / "supervisor"]
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
        self.daemon = self.spawn(self.server + ["-listen", self.address])
        status_line(self.daemon.stderr, b"serving QUIC")

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


class Live(BrokerFixture):
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
        until(fd, b"Waiting for a supervisor")
        shared = self.spawn([SUP, "-state", self.base / "display-supervisor", fresh, "/bin/sh", "-c", "echo first-ready; while read -r x; do echo reply-$x; done"])
        status_line(shared.stderr, b"session ")
        until(fd, b"first-ready")
        os.write(fd, b"shared-link-input\n")
        until(fd, b"reply-shared-link-input")
        viewer.terminate()
        viewer.wait(5)
        self.assertEqual(len(json.loads(self.run_cli(guest).stdout)), 1)
        fd, viewer = terminal(guest)
        until(fd, b"first-ready")
        viewer.terminate()
        viewer.wait(5)
        self.session(["-mode", "pipe"], ["/bin/sh", "-c", "echo second-ready; cat"])
        fd, viewer = terminal(guest)
        until(fd, b"Choose a session")
        os.write(fd, b"2\n")
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
                sup = [SUP, link, "true"]
                if first == "concurrent":
                    with concurrent.futures.ThreadPoolExecutor(2) as pool:
                        jobs = [pool.submit(self.run_cli, cmd, env=env) for cmd in (cli, sup)]
                        for job in jobs:
                            job.result()
                else:
                    self.run_cli(cli if first == "client" else sup, env=env)
                self.run_cli([CLI], env=env)
                self.run_cli([SUP, "true"], env=env)
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
            self.run_cli([SUP, "true"], env=env)
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
                self.run_cli([SUP, "true"], env=env)
                self.assertEqual(key, (root / "supervisor/key.pem").read_bytes())

    def test_conflicting_default_servers(self):
        env, root = self.account("conflicting")
        second = [DAEMON, "-state", self.base / "second"]
        daemon = self.spawn(second + ["-listen", "127.0.0.1:0"])
        status_line(daemon.stderr, b"serving QUIC")
        a = self.run_cli(self.server + ["invite"]).stdout.strip()
        b = self.run_cli(second + ["invite"]).stdout.strip()
        self.run_cli([CLI, "-state", root / "client", a])
        self.run_cli([SUP, "-state", root / "supervisor", b, "true"])
        proc = self.spawn([SUP, "cat"], env=env)
        sid = status_line(proc.stderr, b"session ").split()[1].decode()
        self.assertFalse(json.loads(self.run_cli([CLI], env=env).stdout))
        self.assertEqual(json.loads(self.run_cli([CLI, b], env=env).stdout)[0]["ID"], sid)
        self.assertEqual(json.loads(self.run_cli([CLI], env=env).stdout)[0]["ID"], sid)
        self.assertFalse(json.loads(self.run_cli([CLI, "-state", root / "client"]).stdout))
        self.assertEqual(len(self.run_cli(second + ["devices"]).stdout.splitlines()), 1)

    def test_supervisor_positional_pair_and_launch(self):
        sup = [SUP, "-state", self.base / "new-supervisor"]
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
                    f"{proto}={proto}:127.0.0.1:{s.getsockname()[1]}",
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
                v = self.spawn(self.a + ["forward", sid, proto, "127.0.0.1:0"])
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
        result = self.run_cli([SUP, "-state", unpaired, "-drivers"])
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
