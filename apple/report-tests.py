"""Publish individual XCTest outcomes with the failed interaction, excluding device credentials."""
import json
import re
import subprocess
import sys
from pathlib import Path

def results(kind, bundle, *options):
    return json.loads(subprocess.check_output([
        "xcrun", "xcresulttool", "get", "test-results", kind, "--path", str(bundle), *options
    ]))

def nodes(value):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from nodes(child)
    elif isinstance(value, list):
        for child in value:
            yield from nodes(child)

def report(bundle):
    cases = [
        {"test": n["nodeIdentifier"], "result": n["result"], "duration": n.get("duration")}
        for n in nodes(results("tests", bundle)) if n.get("nodeType") == "Test Case"
    ]
    summary = results("summary", bundle)
    failures = []
    for failure in summary["testFailures"]:
        identifier = failure.get("testIdentifierString", failure["testName"])
        activities = results("activities", bundle, "--test-id", identifier)
        steps = []
        for n in nodes(activities):
            title = n.get("title", "")
            if re.match(r'^(Waiting .* for "|Find the "|Tap "|Click "|Double Click "|Type key )', title):
                steps.append(title)
            if n.get("isAssociatedWithFailure"):
                break
        # Only selected activity titles and the first line of the issue leave CI.
        # Raw UIKit trees, environment variables and screenshots stay on the runner.
        def clean(s):
            return re.sub(r'(?:clistream|https?)://\S+|[a-fA-F0-9]{24,}', "<redacted>", s)[:250]
        failures.append({"test": identifier, "issue": clean(failure["failureText"].splitlines()[0]),
                         "interactions": [clean(s) for s in steps[-5:]]})
    return {"cases": cases, "failures": failures}

if __name__ == "__main__":
    print(json.dumps(report(Path(sys.argv[1])), indent=2))
