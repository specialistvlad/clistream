"""Exercise the deployable binaries across separate Linux hosts on a private Docker network."""

import contextlib
import json
import os
import shlex
import subprocess
import tempfile
import time
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def docker(*args, data=None, check=True):
    return subprocess.run(
        ["docker", *args], input=data, capture_output=True, check=check, timeout=90
    )


def verify(root, resources):
    prefix = "clistream-test-" + uuid.uuid4().hex[:10]
    image = os.environ.get("CLISTREAM_TEST_IMAGE", "debian:bookworm-slim")
    arch = docker("info", "--format", "{{.Architecture}}").stdout.decode().strip()
    arch = {"aarch64": "arm64", "x86_64": "amd64"}.get(arch, arch)
    tools = root / "bin"
    subprocess.run(
        ["go", "build", "-trimpath", "-o", str(tools) + "/", "./cmd/..."],
        cwd=ROOT,
        check=True,
        env={**os.environ, "GOOS": "linux", "GOARCH": arch, "CGO_ENABLED": "0"},
    )
    docker("network", "create", "--internal", prefix)
    resources.callback(docker, "network", "rm", prefix)
    for name in ("broker", "alpha", "beta", "viewer"):
        mounts = []
        if name in ("alpha", "beta"):
            workspace = root / name
            workspace.mkdir()
            (workspace / "origin").write_text(name)
            mounts = ["-v", f"{workspace}:/workspace"]
        docker(
            "run",
            "-d",
            "--name",
            prefix + "-" + name,
            "--network",
            prefix,
            "--network-alias",
            name,
            "--hostname",
            name,
            "--read-only",
            "--cap-drop",
            "ALL",
            "--security-opt",
            "no-new-privileges",
            "--tmpfs",
            "/state",
            "--tmpfs",
            "/tmp",
            "-v",
            f"{tools}:/opt/cli:ro",
            *mounts,
            image,
            "sleep",
            "infinity",
        )
        resources.callback(docker, "rm", "-f", prefix + "-" + name)
    sentinel = root / "outside"
    sentinel.write_text("host-only")

    def run(host, executable, *args, data=None, background=False, check=True):
        working = "/workspace" if host in ("alpha", "beta") else "/tmp"
        return docker(
            "exec",
            "-d" if background else "-i",
            "-w",
            working,
            prefix + "-" + host,
            "/opt/cli/" + executable,
            "-state",
            "/state/identity",
            *args,
            data=data,
            check=check,
        )

    run(
        "broker",
        "clistreamd",
        "-listen",
        ":7443",
        "-advertise",
        "broker:7443",
        background=True,
    )

    def until(fn):
        end = time.monotonic() + 15
        while time.monotonic() < end:
            value = fn()
            if value:
                return value
            time.sleep(0.1)
        raise AssertionError("container service readiness timed out")

    until(lambda: run("broker", "clistreamd", "devices", check=False).returncode == 0)
    for host in ("alpha", "beta", "viewer"):
        role = "client" if host == "viewer" else "supervisor"
        link = run("broker", "clistreamd", "-qr=false", "invite", role).stdout
        if host == "viewer":
            run(host, "clistream", "pair", data=link)
        else:
            run(host, "clistream-supervise", "-pair", data=link)
            run(
                host,
                "clistream-supervise",
                "-local-input=false",
                "--",
                "/bin/sh",
                background=True,
            )

    def sessions():
        items = json.loads(run("viewer", "clistream", "list").stdout) or []
        return items if len(items) == 2 else None

    for item in until(sessions):
        host = item["Host"]
        assert host in ("alpha", "beta")
        sid = item["ID"]
        run("viewer", "clistream", "get", sid, "origin", "/tmp/" + host)
        got = docker("exec", prefix + "-viewer", "cat", "/tmp/" + host).stdout
        assert got.decode() == host
        denied = run(
            "viewer", "clistream", "get", sid, "../outside", "/tmp/private", check=False
        )
        assert denied.returncode != 0
        response = run(
            "viewer",
            "clistream",
            "-eof",
            "attach",
            sid,
            data=(
                "cat origin; "
                f"for path in /workspace/../outside {shlex.quote(str(sentinel))}; do "
                'test ! -r "$path" && ! (echo changed > "$path") || exit 8; '
                "done; echo confined\n"
            ).encode(),
        )
        assert response.stdout == (host + "confined\n").encode(), response.stdout
    assert sentinel.read_text() == "host-only"
    print(
        "PASS: remote registration, independent session I/O and workspace roots, host sentinel isolation"
    )


if __name__ == "__main__":
    with tempfile.TemporaryDirectory() as tmp, contextlib.ExitStack() as resources:
        verify(Path(tmp), resources)
