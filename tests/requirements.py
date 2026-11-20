"""Check that every documented requirement has a trace to existing executable tests."""
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def identifiers(text, separator):
    names = re.findall(r"^\| ([FN]\d+)" + separator, text, re.MULTILINE)
    if not names or len(names) != len(set(names)):
        raise ValueError("missing or repeated requirement identifiers")
    return set(names)

def verify():
    requirements = (ROOT / "docs/requirements.md").read_text()
    audit = (ROOT / "docs/test-results/requirements-audit.md").read_text()
    declared = identifiers(requirements, r" \|")
    covered = identifiers(audit, r"\.")
    if declared != covered:
        raise ValueError(f"requirement trace mismatch: {sorted(declared ^ covered)}")
    cases = set()
    for pattern in ["internal/**/*_test.go", "tests/*.py", "apple/*Tests/*.swift"]:
        for path in ROOT.glob(pattern):
            cases.update(re.findall(r"\b(?:def|func) ((?:test_|test[A-Z]|Test[A-Z])\w*)\(", path.read_text()))
    targets = set(re.findall(r"^([\w-]+):", (ROOT / "Makefile").read_text(), re.MULTILINE))
    for row in audit.splitlines():
        if not re.match(r"^\| [FN]\d+\.", row):
            continue
        references = re.findall(r"`([^`]+)`", row.split("|")[2])
        if not references:
            raise ValueError(f"missing test reference in {row}")
        for name in references:
            if name in cases or (name.startswith("make ") and name[5:] in targets):
                continue
            if name.startswith("apple/") and (ROOT / name).is_file():
                continue
            raise ValueError(f"unknown acceptance test {name}")
    functional = sum(name.startswith("F") for name in declared)
    print(f"Verified test references for {functional} functional and {len(declared) - functional} engineering requirements")

if __name__ == "__main__":
    verify()
