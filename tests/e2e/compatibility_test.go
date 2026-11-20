//go:build compatibility

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreviousPeers(t *testing.T) {
	for _, env := range []string{"CLISTREAM_LEGACY_BIN", "CLISTREAM_MANAGED_V1_BIN"} {
		t.Run(env, func(t *testing.T) {
			dir := os.Getenv(env)
			if dir == "" {
				t.Fatalf("set %s to the previous release's binary directory", env)
			}
			dir, err := filepath.Abs(dir)
			check(t, err)
			f := setup(t)
			oldCLI := filepath.Join(dir, client)
			oldSup := filepath.Join(dir, "clistream-supervise")
			oldDaemon := filepath.Join(dir, daemon)
			legacyClient := []string{"-state", filepath.Join(f.config("a"), "device")}
			attach := func(id string) {
				t.Helper()
				v := f.start("a", oldCLI, append(legacyClient, "-eof", "attach", id)...)
				v.send("mixed versions\n")
				v.input.Close()
				v.expect(0)
				equal(t, v.out.contents(), "mixed versions\n")
			}
			// Previous clients accept session records without the retired fields.
			p, id := f.session(nil, "cat")
			contains(t, f.run("a", oldCLI, append(legacyClient, "list")...), id)
			attach(id)
			p.expect(0)
			// The new broker strictly rejects obsolete fields in an old registration.
			oldArgs := []string{"-state", filepath.Join(f.config("sup"), "device")}
			if env == "CLISTREAM_MANAGED_V1_BIN" {
				oldArgs = append(oldArgs, "-once")
			}
			f.reject("sup", oldSup, append(oldArgs, "true")...)
			equal(t, len(f.sessions("a")), 0)
			f.server.stop()
			f.server = f.start("srv", oldDaemon, "-state", filepath.Join(f.config("srv"), "server"), "-qr=false", "-listen", f.address)
			f.server.diagnostics.until(t, "serving QUIC")
			// Enroll through the old executable under this account, then launch the
			// renamed supervisor from the same saved identity without pairing again.
			link := f.run("srv", oldDaemon, "invite")
			pair := f.start("upgrade", oldSup, "-pair")
			pair.send(link)
			pair.input.Close()
			pair.expect(0)
			p = f.start("upgrade", supervisor, "--interactive", "--local-input=false", "cat")
			id = p.sessionID()
			contains(t, f.run("a", oldCLI, append(legacyClient, "list")...), id)
			// Previous brokers reserialize their wider session struct in list responses.
			contains(t, f.reject("a", client, "list"), "unknown field")
			attach(id)
			if env == "CLISTREAM_LEGACY_BIN" {
				p.expect(0)
			} else {
				p.stop()
			}
		})
	}
}
