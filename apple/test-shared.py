"""Exercise concurrent Apple and CLI control of the same child on one private broker."""

import concurrent.futures
import json
import os
import platform
import selectors
import subprocess
import sys
import tempfile
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
        for device in ["ipad", "iphone"]:
            udid = cls.destinations[device][1].split("id=")[1]
            subprocess.run(["xcrun", "simctl", "bootstatus", udid, "-b"], check=True)
        # The same iOS test bundle runs on both simulated devices.
        for device in ["mac", "iphone"]:
            subprocess.run(
                cls.command(device, "build-for-testing"), cwd=APP, check=True
            )

    @classmethod
    def command(cls, device, action):
        system, destination = cls.destinations[device]
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

        def apple(device):
            system = self.destinations[device][0]
            tests = "CLIStream-MacTests" if system == "macOS" else "CLIStream-iOSTests"
            link = (
                self.run_cli(self.server + ["invite", "client"]).stdout.decode().strip()
            )
            args = self.command(device, "test-without-building") + [
                "-only-testing:" + tests + "/ClientTests/testSharedPipe",
                "-parallel-testing-enabled",
                "NO",
                "-resultBundlePath",
                str(reports / (device + ".xcresult")),
            ]
            env = {
                **os.environ,
                "TEST_RUNNER_CLISTREAM_SESSION": sid,
                "TEST_RUNNER_CLISTREAM_PIPE_LINK": link,
                "TEST_RUNNER_CLISTREAM_PARTICIPANT": device,
            }
            subprocess.run(
                args, env=env, cwd=APP, check=True, timeout=180, capture_output=True
            )
            report = subprocess.check_output(
                [
                    "xcrun",
                    "xcresulttool",
                    "get",
                    "test-results",
                    "summary",
                    "--path",
                    str(reports / (device + ".xcresult")),
                ]
            )
            counts = json.loads(report)
            self.assertEqual(
                (counts["passedTests"], counts["skippedTests"], counts["failedTests"]),
                (1, 0, 0),
                device,
            )

        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as runners:
            jobs = [runners.submit(apple, name) for name in self.destinations]
            # Every Apple attachment waits for the CLI's last line before leaving.
            expected = {
                f"out:hosted-{device}-echo\n".encode() for device in self.destinations
            }
            seen = set()
            until = time.monotonic() + 90
            with selectors.DefaultSelector() as sel:
                sel.register(observer.stdout, selectors.EVENT_READ)
                while seen != expected:
                    self.assertTrue(
                        sel.select(max(0, until - time.monotonic())),
                        "waiting for all three Apple clients",
                    )
                    record = observer.stdout.readline()
                    self.assertIn(record, expected)
                    seen.add(record)
            observer.stdin.write(b"hosted-cli-echo\n")
            for job in jobs:
                job.result()
        child.stdin.close()
        self.assertEqual(child.wait(8), 0)
        stdout, stderr = observer.communicate(timeout=8)
        self.assertEqual(observer.returncode, 0)
        self.assertEqual(stdout, b"out:hosted-cli-echo\n")
        expected.add(stdout)
        self.assertEqual(set(child.stdout.read().splitlines(keepends=True)), expected)
        errors = {entry.replace(b"out:", b"err:", 1) for entry in expected}
        self.assertEqual(set(stderr.splitlines(keepends=True)), errors)
        self.assertEqual(set(child.stderr.read().splitlines(keepends=True)), errors)
        print("Verified four simultaneous clients. XCTest results in", reports, flush=True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
