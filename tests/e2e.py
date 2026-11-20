"""Exercise installed binaries on real UDP sockets with isolated, temporary identities."""

import contextlib
import json
import os
import pathlib
import selectors
import shutil
import statistics
import socket
import termios
import subprocess
import tempfile
import threading
import time
import unittest

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


class Live(unittest.TestCase):
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
        for role, args in [
            ("client", self.a),
            ("client", self.b),
            ("supervisor", self.supervisor),
        ]:
            link = self.run_cli(self.server + ["invite", role]).stdout
            self.run_cli(
                args + (["-pair"] if role == "supervisor" else ["pair"]), data=link
            )
            self.assertNotEqual(
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
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
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

    def run_cli(self, args, data=None, check=True):
        result = subprocess.run(args, input=data, capture_output=True, timeout=12)
        if check:
            self.assertEqual(result.returncode, 0, result.stderr)
        return result

    def session(self, flags, cmd):
        p = self.spawn(self.supervisor + flags + ["--"] + cmd, cwd=self.work)
        msg = status_line(p.stderr, b"session ")
        return p, msg.split()[1].decode()

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
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            sessions = json.loads(self.run_cli(self.a + ["list"]).stdout) or []
            if sessions:
                break
            time.sleep(0.1)
        self.assertTrue(sessions)
        new_id = sessions[0]["ID"]
        self.assertNotEqual(new_id, old_id)
        remote = self.spawn(self.a + ["attach", new_id])
        self.assertEqual(line(remote.stdout), b"before-restart\n")

    def test_detected_drivers(self):
        found = 0
        for name, banner in [
            ("claude", b"2.1.329 (Claude Code)"),
            ("codex", b"codex-cli 0.170.0"),
        ]:
            executable = shutil.which(name)
            if (
                executable
                and self.run_cli([executable, "--version"]).stdout.strip() == banner
            ):
                r = self.run_cli(
                    self.supervisor
                    + ["-local-input=false", "--", executable, "--version"]
                )
                self.assertIn(b"session ", r.stderr)
                self.assertTrue(r.stdout.strip())
                found += 1
        if not found:
            self.skipTest("Supported Claude Code and Codex CLI versions unavailable")

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
        self.assertNotEqual(
            self.run_cli(
                [CLI, "-state", self.base / "supervisor", "list"], check=False
            ).returncode,
            0,
        )
        self.daemon.terminate()
        self.daemon.wait(5)
        self.start_server()
        self.run_cli(self.a + ["list"])
        devices = self.run_cli(self.server + ["devices"]).stdout.decode().splitlines()
        for device in devices:
            if device.endswith(" client"):
                self.run_cli(self.server + ["revoke", device.split()[0]])
        self.assertNotEqual(self.run_cli(self.a + ["list"], check=False).returncode, 0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
