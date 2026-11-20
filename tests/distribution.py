"""Verify a release archive's manifest and license, then test its extracted binaries."""
import hashlib
import os
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def verify(archive):
    checksums = {name: digest for digest, name in
                 (line.split() for line in (archive.parent / "SHA256SUMS").read_text().splitlines())}
    with archive.open("rb") as source:
        actual = hashlib.file_digest(source, "sha256").hexdigest()
    if checksums.get(archive.name) != actual:
        raise ValueError(f"checksum mismatch for {archive.name}")
    with tempfile.TemporaryDirectory() as tmp, tarfile.open(archive) as tar:
        prefix = archive.name.removesuffix(".tar.gz")
        expected = {"clistream", "clistreamd", "clistream-supervise", "README.md", "LICENSE"}
        if {m.name for m in tar} != {prefix + "/" + name for name in expected}:
            raise ValueError("unexpected archive contents")
        for member in tar:
            if not member.isfile():
                raise ValueError(f"unexpected entry type for {member.name}")
        tar.extractall(tmp, filter="data")
        commands = Path(tmp) / prefix
        if (commands / "LICENSE").read_bytes() != (ROOT / "LICENSE").read_bytes():
            raise ValueError("packaged MIT notice differs from the repository")
        subprocess.run([sys.executable, ROOT / "tests/e2e.py"], check=True,
                       env={**os.environ, "CLISTREAM_BIN": str(commands)})

if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit("usage: python3 tests/distribution.py dist/ARCHIVE.tar.gz [...]")
    for arg in sys.argv[1:]:
        verify(Path(arg))
