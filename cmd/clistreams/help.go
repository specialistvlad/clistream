package main

import "github.com/specialistvlad/clistream/internal/cliopts"

const syntax = "clistreams [OPTIONS] [PAIRING_LINK] PROGRAM [ARG ...]"

func help() {
	cliopts.Help("clistreams — Run a program and choose what to share.", syntax,
		cliopts.Section{Title: "General", Body: `  clistreams /bin/sh    Start a shell for enrolled clients. The host shows activity.
  --help                Show this help. Options go before the program name.
  Arguments after PROGRAM go directly to the program, including --help.`},
		cliopts.Section{Title: "Pairing and connection", Body: `  clistreams 'PAIRING_LINK' PROGRAM    Pair and launch. Later, omit the link.
  --pair-stdin                         Read a pairing link from stdin and exit.
  --server-address HOST:PORT           Connect to another address for the same
                                       pinned server.`},
		cliopts.Section{Title: "Terminal and input", Body: `  --interactive          Use the child terminal locally, or its streams in a pipe.
  --local-input=false    With --interactive, view output without sending local input.
  --io-mode MODE         auto (default), pipe, or pty. Auto uses a PTY when both
                         stdin and stdout are terminals and pipes otherwise. Remote
                         starts use the requesting client’s terminal and size.`},
		cliopts.Section{Title: "Session lifetime", Body: `  By default, enrolled clients can start another run after the program exits.
  --once    Share one run and exit with the child's status.`},
		cliopts.Section{Title: "Filesystem", Body: `  By default, enrolled devices can browse, download, and upload files in the
  folder where you start clistreams.
  --workspace DIR    Share another folder. Relative paths start in the launch
                     directory. The program keeps its original working directory.
  --files=TRUE|FALSE  Enable shared file operations (default TRUE).

  clistreams --workspace ./reports my-program`},
		cliopts.Section{Title: "Port forwarding", Body: `  --share-port NAME=PROTO:LOOPBACK:PORT
      Share a named TCP or UDP service running on this host. Repeat for each
      service. By default, no ports are shared.

  On the program host, share the website running on port 3000:
    clistreams --share-port web=tcp:127.0.0.1:3000 my-program
  On the viewing device, use the session ID from 'clistream list':
    clistream forward ID web 127.0.0.1:8080
  Open http://127.0.0.1:8080 on the viewing device.`})
}
