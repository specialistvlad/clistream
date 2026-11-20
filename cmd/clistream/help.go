package main

import "github.com/specialistvlad/clistream/internal/cliopts"

var commands = map[string]cliopts.Command{
	"list": {}, "pair": {},
	"attach":  {Arguments: "ID", Min: 1, Max: 1},
	"ls":      {Arguments: "ID [REMOTE_DIR]", Min: 1, Max: 2},
	"get":     {Arguments: "ID REMOTE_FILE LOCAL_FILE", Min: 3, Max: 3},
	"put":     {Arguments: "ID LOCAL_FILE REMOTE_FILE", Min: 3, Max: 3},
	"forward": {Arguments: "ID NAME LOCAL_HOST:PORT", Min: 3, Max: 3},
}

func help() {
	cliopts.Help("clistream — Use a terminal session shared by another computer.",
		"clistream [OPTIONS] [PAIRING_LINK | COMMAND [ARG ...]]",
		cliopts.Section{Title: "Getting started", Body: `  clistream 'PAIRING_LINK'    Pair using the link printed by clistreamd.
  clistream                   Reconnect and choose a running program.
                              With redirected input or output, print a JSON list.
  --help                      Show this help.`},
		cliopts.Section{Title: "Sessions and terminal", Body: `  clistream list              List running programs and their session IDs.
  clistream attach ID         Open a program's terminal. In the interactive client,
                              press Ctrl-G to return to the session list.
  clistream --send-eof attach ID
      In pipe mode, close the program's shared input when local input ends.
      This affects everyone attached to that program.`},
		cliopts.Section{Title: "Filesystem", Body: `  ID selects the running program from 'clistream list'. Remote paths start
  inside the folder its supervisor shares.

  List shared files           clistream ls ID
  List a shared subfolder     clistream ls ID reports
  Download to this computer   clistream get ID reports/sales.csv ./sales.csv
  Upload from this computer   clistream put ID ./notes.txt reports/notes.txt

  Give the source path first and the destination second. Existing files are preserved.`},
		cliopts.Section{Title: "Port forwarding", Body: `  clistream forward ID NAME LOCAL_HOST:PORT
      Open a service the supervisor shared by name. For a website called web:
        clistream forward ID web 127.0.0.1:8080
      Open http://127.0.0.1:8080 on this computer and keep the command running.`},
		cliopts.Section{Title: "Pairing from a script", Body: `  clistream pair    Read a pairing link from standard input.`})
}
