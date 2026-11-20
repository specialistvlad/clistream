"""Exercise concurrent Apple and CLI control of the same child on one private broker."""

import http.server
import json
import os
import platform
import selectors
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from pathlib import Path

APP = Path(__file__).resolve().parent
sys.path.insert(0, str(APP.parent / "tests"))
from e2e import BrokerFixture, status_line, DashboardTerminal, SUP, line


class SharedAppleSession(BrokerFixture):
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls.devices = {"ipad": os.environ["IPAD_SIMULATOR_ID"],
                       "iphone": os.environ["IPHONE_SIMULATOR_ID"]}
        cls.mobile_id = "com.vlk.clistream.shared-acceptance"
        cls.mac_options = [
            "-scheme", "CLIStream-macOS", "-destination", "platform=macOS,arch=" + platform.machine(),
            "-derivedDataPath", "build/accept-mac", "CLISTREAM_APP_ID=com.vlk.clistream.acceptance",
        ]
        subprocess.run(["make", "build"], cwd=APP.parent, check=True)
        subprocess.run(["xcodebuild", "-quiet", "build-for-testing", *cls.mac_options], cwd=APP, check=True)
        # Build before the hosted Mac has to keep both simulators in memory.
        subprocess.run(["xcodebuild", "-quiet", "build", "-scheme", "CLIStream-SharedTestHost",
                        "-destination", "generic/platform=iOS Simulator", "-derivedDataPath",
                        "build/accept-ios"], cwd=APP, check=True)
        cls.package = APP / "build/accept-ios/Build/Products/Debug-iphonesimulator/CLIStreamSharedTestHost.app"

    def test_simultaneous_clients(self):
        child = self.spawn([SUP, "--interactive", "-state", self.base / "supervisor", "-mode", "pipe", "/bin/sh", "-c",
                            'while IFS= read -r v; do echo "out:$v"; echo "err:$v" >&2; done'])
        first = status_line(child.stderr, b"session ").split()[1].decode()
        dashboard = DashboardTerminal(self)
        dashboard.until(b"R Restart")
        expected = self.exercise_run(first, child, dashboard)
        first_errors = [line(child.stderr) for _ in expected]
        self.assertCountEqual(first_errors, [entry.replace(b"out:", b"err:", 1) for entry in expected])
        second = status_line(child.stderr, b"session ").split()[1].decode()
        self.assertNotEqual(first, second)
        replacement_lines = self.exercise_run(second, child, None)
        child.terminate()
        self.assertEqual(child.wait(8), 0)
        self.assertCountEqual(child.stdout.read().splitlines(keepends=True), expected + replacement_lines)
        self.assertCountEqual(child.stderr.read().splitlines(keepends=True),
                              [entry.replace(b"out:", b"err:", 1) for entry in replacement_lines])

    def exercise_run(self, sid, child, dashboard):
        observer = self.spawn(self.a + ["attach", sid])
        reports = Path(tempfile.mkdtemp(prefix="shared-", dir=APP / "build"))

        # XCTest may take longer to launch than an invitation stays valid.
        routes = {device: "/" + os.urandom(24).hex() for device in [*self.devices, "mac"]}
        pending = {route + "/PIPE": device for device, route in routes.items()}

        def mint():
            return self.run_cli(self.server + ["invite", "client"]).stdout

        class Invitations(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                if self.path not in pending:
                    self.send_error(404)
                    return
                print(pending.pop(self.path), "requested its pairing link", flush=True)
                data = mint()
                self.send_response(200)
                self.send_header("Cache-Control", "no-store")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        service = http.server.HTTPServer(("127.0.0.1", 0), Invitations)
        self.addCleanup(service.server_close)
        threading.Thread(target=service.serve_forever, daemon=True).start()
        self.addCleanup(service.shutdown)
        def environment(name, prefix):
            return {
                **os.environ,
                prefix + "CLISTREAM_SESSION": sid,
                prefix + "CLISTREAM_EXPECT_EXIT": "128" if dashboard else "0",
                prefix + "CLISTREAM_PARTICIPANT": name,
                prefix + "CLISTREAM_FIXTURE": f"http://127.0.0.1:{service.server_port}{routes[name]}",
            }

        mac = None
        records = {}
        ready_lines = set()
        with selectors.DefaultSelector() as sel:
            sel.register(observer.stdout, selectors.EVENT_READ)
            for name in routes:
                if name in self.devices:
                    udid = self.devices[name]
                    # Launch each client as soon as its own runtime has migrated.
                    print("Booting", name, flush=True)
                    subprocess.run(["xcrun", "simctl", "bootstatus", udid, "-b"], check=True)
                    subprocess.run(["xcrun", "simctl", "install", udid, self.package], check=True)
                    container = subprocess.check_output(
                        ["xcrun", "simctl", "get_app_container", udid, self.mobile_id, "data"], text=True
                    ).strip()
                    records[name] = Path(container) / "tmp" / f"shared-{sid}.json"
                    subprocess.run(["xcrun", "simctl", "launch", "--terminate-running-process", udid,
                                    self.mobile_id], env=environment(name, "SIMCTL_CHILD_"),
                                   check=True, timeout=480)
                    self.addCleanup(subprocess.run, ["xcrun", "simctl", "terminate", udid, self.mobile_id],
                                    capture_output=True)
                else:
                    with (self.base / "mac.log").open("wb") as log:
                        mac = self.spawn([
                            "xcodebuild", "-quiet", "test-without-building", *self.mac_options,
                            "-only-testing:CLIStream-MacTests/ClientTests/testSharedPipe",
                            "-parallel-testing-enabled", "NO", "-resultBundlePath", str(reports / "mac.xcresult"),
                        ], env=environment("mac", "TEST_RUNNER_"), cwd=APP, stdout=log, stderr=log)
                deadline = time.monotonic() + 600
                while not sel.select(1):
                    if mac is not None:
                        self.assertIsNone(mac.poll(), f"Mac test exited before {name} attached")
                    if name in records and records[name].exists():
                        self.fail(f"{name}: {json.loads(records[name].read_text()).get('failedStage')}")
                    self.assertLess(time.monotonic(), deadline, f"{name} did not attach")
                observed = observer.stdout.readline()
                self.assertEqual(observed, f"out:ready-{name}\n".encode())
                ready_lines.add(observed)
                print(name, "attached to child", flush=True)

        # Only release the writers after all four clients are attached.
        observer.stdin.write(b"hosted-cli-echo\n")
        writers = [*routes, "cli"]
        received_lines = list(ready_lines)
        verified = set()
        expected = {f"out:verified-{name}\n".encode() for name in routes}
        with selectors.DefaultSelector() as sel:
            sel.register(observer.stdout, selectors.EVENT_READ)
            while verified != expected:
                self.assertTrue(sel.select(90), "participants did not verify the run")
                observed = observer.stdout.readline()
                self.assertTrue(observed)
                received_lines.append(observed)
                if observed in expected:
                    verified.add(observed)
        if dashboard:
            dashboard.send(b"r")
            dashboard.until(b"Enter Restart & attach")
            dashboard.send(b"\r")
            dashboard.until(b"Ctrl-G Home")
            dashboard.send(b"\x07q")
            self.assertEqual(dashboard.process.wait(8), 0)
        else:
            child.stdin.close()
        self.assertEqual(observer.wait(10), 128 if dashboard else 0)
        stdout, stderr = observer.communicate(timeout=8)
        received_lines.extend(stdout.splitlines(keepends=True))
        all_lines = list(ready_lines | expected) + [f"out:hosted-{name}-echo\n".encode() for name in writers]
        self.assertCountEqual(received_lines, all_lines)
        self.assertCountEqual(stderr.splitlines(keepends=True),
                              [entry.replace(b"out:", b"err:", 1) for entry in all_lines])
        for name, record in records.items():
            deadline = time.monotonic() + 120
            while not record.exists():
                self.assertLess(time.monotonic(), deadline, f"{name} did not record all writers")
                time.sleep(0.2)
            received = json.loads(record.read_text())
            self.assertNotIn("failedStage", received, name)
            for channel, prefix in [("stdout", "out"), ("stderr", "err")]:
                echoes = [s for s in received[channel].splitlines() if s.startswith(prefix + ":hosted-")]
                self.assertCountEqual(echoes, [f"{prefix}:hosted-{writer}-echo" for writer in writers])
            (reports / f"{name}.json").write_text(json.dumps({
                "device": name, "writers": writers, **received,
            }, indent=2) + "\n")
        self.assertEqual(mac.wait(timeout=240), 0, "Mac XCTest failed")
        summary = json.loads(subprocess.check_output([
            "xcrun", "xcresulttool", "get", "test-results", "summary", "--path", str(reports / "mac.xcresult")
        ]))
        self.assertEqual((summary["passedTests"], summary["skippedTests"], summary["failedTests"]), (1, 0, 0))
        events = [json.loads(row) for row in (self.base / "server/server.log").read_text().splitlines()]
        attachments = {e["connection_id"]: e["device"] for e in events
                       if e["event"] == "channel_open" and e["session_id"] == sid and e["operation"] == "stdin"}
        self.assertEqual(len(attachments), 4 if dashboard else 5)
        self.assertEqual(len(set(attachments.values())), 4)
        closed = {e["connection_id"] for e in events if e["event"] == "connection_close" and e["role"] == "client"}
        self.assertTrue(attachments.keys() <= closed)
        print("Verified four simultaneous clients. Results in", reports, flush=True)
        return all_lines


if __name__ == "__main__":
    unittest.main(verbosity=2)
