"""Build deterministic CLI archives for each supported OS and CPU."""

import gzip
import hashlib
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile


def package(system, arch, out):
    name = f"clistream-{system}-{arch}"
    target = out / f"{name}.tar.gz"
    with tempfile.TemporaryDirectory() as tmp:
        stage = Path(tmp)
        env = {**os.environ, "GOOS": system, "GOARCH": arch, "CGO_ENABLED": "0"}
        subprocess.run(
            [
                "go",
                "build",
                "-trimpath",
                "-buildvcs=false",
                "-o",
                tmp + "/",
                "./cmd/...",
            ],
            env=env,
            check=True,
        )
        files = [*stage.glob("clistream*"), Path("LICENSE"), Path("README.md")]
        with (
            target.open("wb") as dest,
            gzip.GzipFile(filename="", fileobj=dest, mode="wb", mtime=0) as packed,
        ):
            with tarfile.open(fileobj=packed, mode="w") as tar:
                for item in sorted(files, key=lambda p: p.name):
                    info = tarfile.TarInfo(f"{name}/{item.name}")
                    info.size = item.stat().st_size
                    info.mode = 0o755 if item.parent == stage else 0o644
                    with item.open("rb") as source:
                        tar.addfile(info, source)
    with target.open("rb") as f:
        return f"{hashlib.file_digest(f, 'sha256').hexdigest()}  {target.name}"


if __name__ == "__main__":
    out = Path("dist")
    out.mkdir(exist_ok=True)
    hashes = [
        package(system, arch, out)
        for system in ("darwin", "linux")
        for arch in ("amd64", "arm64")
    ]
    (out / "SHA256SUMS").write_text("\n".join(hashes) + "\n")
