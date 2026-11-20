package termio

import "golang.org/x/sys/unix"

const getMode = unix.TIOCGETA
const setMode = unix.TIOCSETA
const restoreAndFlush = unix.TIOCSETAF
