"""Exercise the previous release's binaries supplied in CLISTREAM_LEGACY_BIN."""
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

    def test_previous_tools(self):
        child = self.spawn([self.legacy / "clistream-supervise", "-state", self.base / "supervisor",
                            "/bin/cat"])
        sid = status_line(child.stderr, b"session ").split()[1]
        dashboard = DashboardTerminal(self)
        dashboard.until(b"Attach only")
        dashboard.send(b"\r")
        dashboard.until(b"Ctrl-G Home")
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
