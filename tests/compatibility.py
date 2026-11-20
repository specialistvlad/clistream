"""Exercise pre-managed binaries in CLISTREAM_LEGACY_BIN and managed v1 in CLISTREAM_MANAGED_V1_BIN."""
import json
import os
import unittest
from pathlib import Path

from e2e import BrokerFixture, DashboardTerminal, SUP, status_line, line


class Compatibility(BrokerFixture):
    legacy = Path(os.environ["CLISTREAM_LEGACY_BIN"])

    def test_old_daemon(self):
        self.daemon.terminate()
        self.daemon.wait(8)
        self.server[0] = self.legacy / "clistreamd"
        self.start_server()
        child = self.spawn([SUP, "-state", self.base / "supervisor", "/bin/sh", "-c",
                            "echo legacy-ready; read value; exit 17"])
        status_line(child.stderr, b"Server supports attach only")
        status_line(child.stderr, b"session ")
        screen = DashboardTerminal(self)
        screen.until(b"Attach only")
        screen.send(b"\r")
        screen.until(b"legacy-ready")
        screen.send(b"done\r")
        self.assertEqual(child.wait(8), 17)
        screen.until(b"exit 17")

    @unittest.skipUnless(os.environ.get("CLISTREAM_MANAGED_V1_BIN"), "requires managed v1 binaries")
    def test_previous_managed_peers(self):
        previous = Path(os.environ["CLISTREAM_MANAGED_V1_BIN"])
        for old_daemon, old_supervisor, old_dashboard in (
                (False, True, False), (False, False, True), (True, False, False)):
            with self.subTest(daemon=old_daemon, supervisor=old_supervisor, dashboard=old_dashboard):
                if old_daemon:
                    self.daemon.terminate()
                    self.daemon.wait(8)
                    self.server[0] = previous / "clistreamd"
                    self.start_server()
                supervisor = previous / "clistream-supervise" if old_supervisor else SUP
                child = self.spawn([supervisor, "-state", self.base / "supervisor", "/bin/cat"])
                old = status_line(child.stderr, b"session ").split()[1]
                cli = [previous / "clistream", "-state", self.base / "a"] if old_dashboard else self.a
                dashboard = DashboardTerminal(self, cli)
                dashboard.until(b"R Restart")
                dashboard.send(b"e")
                dashboard.until(b"Executable")
                dashboard.send(b"\x12")
                dashboard.until(b"[Cancel]")
                dashboard.send(b"\t\r")
                new = status_line(child.stderr, b"session ").split()[1]
                self.assertNotEqual(old, new)
                dashboard.process.terminate()
                dashboard.process.wait(8)
                child.terminate()
                child.wait(8)

    def test_previous_tools(self):
        child = self.spawn([self.legacy / "clistream-supervise", "-state", self.base / "supervisor",
                            "/bin/cat"])
        sid = status_line(child.stderr, b"session ").split()[1]
        dashboard = DashboardTerminal(self)
        dashboard.until(b"Attach only")
        dashboard.send(b"\r")
        dashboard.until(b"Ctrl-G Disconnect")
        dashboard.send(b"old-supervisor\r")
        self.assertEqual(line(child.stdout), b"old-supervisor\n")
        child.terminate()
        child.wait(8)
        managed = self.spawn([SUP, "-state", self.base / "supervisor", "/bin/cat"])
        sid = status_line(managed.stderr, b"session ").split()[1]
        legacy = [self.legacy / "clistream", "-state", self.base / "a"]
        self.assertEqual(json.loads(self.run_cli(legacy + ["list"]).stdout)[0]["ID"].encode(), sid)
        result = self.run_cli(legacy + ["-eof", "attach", sid], data=b"strict decoder\n")
        self.assertEqual(result.stdout, b"strict decoder\n")


if __name__ == "__main__":
    unittest.main(verbosity=2)
