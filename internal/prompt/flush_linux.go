package prompt

import "golang.org/x/sys/unix"

// flushInput drops input the terminal has received but nobody has read yet.
func flushInput(fd int) {
	unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)
}
