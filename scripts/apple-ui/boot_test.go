package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSimulatorMigrationFailureWithSuccessfulExit(t *testing.T) {
	for _, test := range []struct {
		name, runner, second string
		wantCalls            string
		wantSuccess          bool
	}{
		{"hosted recovery", "github-hosted", "Finished", "bootstatus shutdown erase bootstatus", true},
		{"repeated failure", "github-hosted", "Data Migration Failed", "bootstatus shutdown erase bootstatus", false},
		{"preserve local simulator", "", "Finished", "bootstatus", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "calls")
			t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
			t.Setenv("RUNNER_ENVIRONMENT", test.runner)
			t.Setenv("SIM_CALLS", log)
			t.Setenv("SECOND_STATUS", test.second)
			const simctl = `#!/bin/sh
[ "$1" = simctl ] && [ "$3" = test-device ] || exit 2
echo "$2" >> "$SIM_CALLS"
if [ "$2" = bootstatus ]; then
  if grep -q erase "$SIM_CALLS"; then echo "$SECOND_STATUS"; else echo 'Data Migration Failed'; fi
fi
`
			if err := os.WriteFile(filepath.Join(dir, "xcrun"), []byte(simctl), 0700); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command("bash", "../../apple/boot-simulator.sh", "test-device").CombinedOutput()
			if (err == nil) != test.wantSuccess {
				t.Fatalf("boot result: %v, %s", err, output)
			}
			calls, err := os.ReadFile(log)
			if err != nil || strings.Join(strings.Fields(string(calls)), " ") != test.wantCalls {
				t.Fatalf("simctl calls: %q, %v", calls, err)
			}
		})
	}
}
