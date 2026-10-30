package prompt

import "golang.org/x/sys/unix"

// fread is FREAD from <sys/fcntl.h>: TIOCFLUSH with it flushes only the input side.
const fread = 0x1

// flushInput drops input the terminal has received but nobody has read yet.
func flushInput(fd int) {
	unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, fread)
}
