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
from e2e import BrokerFixture


class SharedAppleSession(BrokerFixture):
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls.destinations = {
            "mac": ("macOS", "platform=macOS,arch=" + platform.machine()),
            "iphone": (
                "iOS",
                "platform=iOS Simulator,id=" + os.environ["IPHONE_SIMULATOR_ID"],
            ),
            "ipad": (
                "iOS",
                "platform=iOS Simulator,id=" + os.environ["IPAD_SIMULATOR_ID"],
            ),
        }
        subprocess.run(["make", "build"], cwd=APP.parent, check=True)
        cls.manifests = {}
        # Build once before asking the hosted Mac to keep two simulators alive.
        for device, sdk in [("mac", "macosx"), ("iphone", "iphonesimulator")]:
            subprocess.run(cls.command(device, "build-for-testing"), cwd=APP, check=True)
            version = subprocess.check_output(
                ["xcrun", "--sdk", sdk, "--show-sdk-version"], text=True
            ).strip()
            system = cls.destinations[device][0]
            products = APP / ("build/accept-mac" if device == "mac" else "build/accept-ios") / "Build/Products"
            manifests = list(products.glob(f"CLIStream-{system}_{sdk}{version}-*.xctestrun"))
            if len(manifests) != 1:
                raise RuntimeError(f"Expected one {system} test manifest for {sdk} {version}")
            cls.manifests[system] = str(manifests[0])
        app = APP / "build/accept-ios/Build/Products/Debug-iphonesimulator/CLIStream.app"
        for device in ["iphone", "ipad"]:
            udid = cls.destinations[device][1].split("id=")[1]
            subprocess.run(["xcrun", "simctl", "bootstatus", udid, "-b"], check=True)
            subprocess.run(["xcrun", "simctl", "install", udid, str(app)], check=True)

    @classmethod
    def command(cls, device, action):
        system, destination = cls.destinations[device]
        if action == "test-without-building":
            return ["xcodebuild", "-quiet", action, "-xctestrun", cls.manifests[system],
                    "-destination", destination]
        derived = "build/accept-mac" if system == "macOS" else "build/accept-ios"
        return [
            "xcodebuild",
            "-quiet",
            action,
            "-scheme",
            "CLIStream-" + system,
            "-destination",
            destination,
            "-derivedDataPath",
            derived,
            "CLISTREAM_APP_ID=com.vlk.clistream.acceptance",
        ]

    def test_simultaneous_clients(self):
        child, sid = self.session(
            ["-mode", "pipe"],
            [
                "/bin/sh",
                "-c",
                'while IFS= read -r v; do echo "out:$v"; echo "err:$v" >&2; done',
            ],
        )
        observer = self.spawn(self.a + ["attach", sid])
        reports = Path(tempfile.mkdtemp(prefix="shared-", dir=APP / "build"))

        # XCTest may take longer to launch than an invitation stays valid.
        routes = {device: "/" + os.urandom(24).hex() for device in self.destinations}
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
        jobs = {}
        ready_lines = set()
        with selectors.DefaultSelector() as sel:
            sel.register(observer.stdout, selectors.EVENT_READ)
            for device, (system, _) in self.destinations.items():
                tests = "CLIStream-MacTests" if system == "macOS" else "CLIStream-iOSTests"
                args = self.command(device, "test-without-building") + [
                    "-only-testing:" + tests + "/ClientTests/testSharedPipe",
                    "-parallel-testing-enabled", "NO",
                    "-resultBundlePath", str(reports / (device + ".xcresult")),
                ]
                env = {
                    **os.environ,
                    "TEST_RUNNER_CLISTREAM_SESSION": sid,
                    "TEST_RUNNER_CLISTREAM_FIXTURE": f"http://127.0.0.1:{service.server_port}{routes[device]}",
                    "TEST_RUNNER_CLISTREAM_PARTICIPANT": device,
                }
                print("Starting", device, flush=True)
                with (self.base / (device + ".log")).open("wb") as log:
                    jobs[device] = self.spawn(args, env=env, cwd=APP, stdout=log, stderr=log)
                deadline = time.monotonic() + 600
                while not sel.select(1):
                    for name, job in jobs.items():
                        self.assertIsNone(job.poll(), f"{name} exited before {device} was ready")
                    self.assertLess(time.monotonic(), deadline, f"{device} did not attach")
                line = observer.stdout.readline()
                self.assertEqual(line, f"out:ready-{device}\n".encode())
                ready_lines.add(line)
                print(device, "attached to child", flush=True)

        # Only release the writers after all four clients are attached.
        observer.stdin.write(b"hosted-cli-echo\n")
        for device, job in jobs.items():
            self.assertEqual(job.wait(timeout=240), 0, device)
            report = subprocess.check_output([
                "xcrun", "xcresulttool", "get", "test-results", "summary", "--path",
                str(reports / (device + ".xcresult")),
            ])
            counts = json.loads(report)
            self.assertEqual(
                (counts["passedTests"], counts["skippedTests"], counts["failedTests"]),
                (1, 0, 0), device,
            )
        child.stdin.close()
        self.assertEqual(child.wait(8), 0)
        stdout, stderr = observer.communicate(timeout=8)
        self.assertEqual(observer.returncode, 0)
        echoes = [f"out:hosted-{name}-echo\n".encode() for name in [*jobs, "cli"]]
        self.assertCountEqual(stdout.splitlines(keepends=True), echoes)
        expected = ready_lines.union(echoes)
        self.assertCountEqual(child.stdout.read().splitlines(keepends=True), expected)
        errors = {entry.replace(b"out:", b"err:", 1) for entry in expected}
        self.assertCountEqual(stderr.splitlines(keepends=True), errors)
        self.assertCountEqual(child.stderr.read().splitlines(keepends=True), errors)
        print("Verified four simultaneous clients. XCTest results in", reports, flush=True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
