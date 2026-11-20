package main

import "github.com/specialistvlad/clistream/internal/cliopts"

var commands = map[string]cliopts.Command{
	"invite": {}, "devices": {}, "revoke": {Arguments: "FINGERPRINT", Min: 1, Max: 1},
}

func help() {
	cliopts.Help("clistreamd — Connect devices to shared terminal sessions.",
		"clistreamd [OPTIONS] [COMMAND]",
		cliopts.Section{Title: "General", Body: `  clistreamd    Start the server. By default, only this computer can connect.
                Run again in a terminal to reopen the running server's pairing display.
  --help        Show this help.`},
		cliopts.Section{Title: "Networking", Body: `  --interface NAME       Accept connections on this interface. Repeat to select
                         more than one. Use loopback to include this computer.
  --listen HOST:PORT     Set the local UDP listening address (default 127.0.0.1:7443).
  --advertise HOST:PORT  Set the endpoint devices receive in pairing links.
                         Use with --listen instead of selecting interfaces.

  clistreamd --interface loopback --interface en0
      Allow connections from this computer and its LAN. Replace en0 with
      the name of your network interface.`},
		cliopts.Section{Title: "Pairing and devices", Body: `  clistreamd invite               Print a pairing link valid for three minutes.
  clistreamd devices              List enrolled devices and their key fingerprints.
  clistreamd revoke FINGERPRINT   Disconnect the device and remove its enrollment.
  --qr=false                      Hide QR codes in the terminal pairing display.
  clistreamd --qr invite          Include a QR code with a scripted invitation.`},
		cliopts.Section{Title: "Logging", Body: `  --no-log-file    Write activity to stderr only. By default the server also
                   writes to server.log in its private state directory.`})
}
