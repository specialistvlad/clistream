package main

import "github.com/specialistvlad/clistream/internal/cliopts"

var commands = map[string]cliopts.Command{
	"list": {}, "pair": {},
	"attach":  {Arguments: "ID", Min: 1, Max: 1},
	"forward": {Arguments: "ID NAME LOCAL_HOST:PORT", Min: 3, Max: 3},
}

func help() {
	cliopts.Help("clistream — Use a terminal session shared by another computer.",
		"clistream [OPTIONS] [PAIRING_LINK] [COMMAND | SESSION_ID ACTION [ARG ...]]",
		cliopts.Section{Title: "Getting started", Body: `  clistream 'PAIRING_LINK'    Pair using the link printed by clistreamd.
  clistream                   Reconnect and choose a running program.
                              With redirected input or output, print a JSON list.
  --help                      Show this help.`},
		cliopts.Section{Title: "Sessions and terminal", Body: `  clistream list              List sessions, including stopped programs. ID identifies
                              the same supervisor across child restarts.
  clistream attach ID         Open a program's terminal. In the interactive client,
                              press Ctrl-G to return to the session list.
  clistream --send-eof attach ID
      In pipe mode, close the program's shared input when local input ends.
      This affects everyone attached to that program.`},
		cliopts.Section{Title: "Session controls", Body: `  clistream SESSION_ID stop                 Stop the running program.
  clistream SESSION_ID start                Start the saved program.
  clistream SESSION_ID restart              Restart the running program.
  clistream SESSION_ID edit -- PROGRAM [ARG ...]  Replace the command and launch it.
  Each command exits after the supervisor acknowledges it.`},
		cliopts.Section{Title: "Filesystem", Body: `  clistream SESSION_ID fs ls [--all] [PATH]  List names. --all includes hidden files.
  clistream SESSION_ID fs cp SRC DEST      Copy a regular file.
  clistream SESSION_ID fs mv SRC DEST      Rename a shared file or directory.
  clistream SESSION_ID fs rm FILE          Remove a shared regular file.
  clistream SESSION_ID fs mkdir DIR        Create a shared directory.
  clistream SESSION_ID fs rmdir DIR        Remove an empty shared directory.

  Paths are relative to the shared folder. For cp, prefix a path on this
  computer with local:. A directory destination receives the source basename.
  Copy and move replace an existing regular file.
    clistream SESSION_ID fs cp reports/sales.csv local:./sales.csv
    clistream SESSION_ID fs cp local:./notes.txt notes.txt`},
		cliopts.Section{Title: "Port forwarding", Body: `  clistream forward ID NAME LOCAL_HOST:PORT
      Open a service the supervisor shared by name. For a website called web:
        clistream forward ID web 127.0.0.1:8080
      Open http://127.0.0.1:8080 on this computer and keep the command running.`},
		cliopts.Section{Title: "Pairing from a script", Body: `  clistream pair    Read a pairing link from standard input.`})
}
