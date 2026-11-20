package termio

import "golang.org/x/sys/unix"

const getMode = unix.TCGETS
const setMode = unix.TCSETS
const restoreAndFlush = unix.TCSETSF
