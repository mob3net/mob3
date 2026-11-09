//go:build linux

package browser

import (
	"os"
	"syscall"
	"unsafe"
)

type termState struct {
	term syscall.Termios
}

type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, req, uintptr(arg), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// isTTY reports whether f refers to a terminal.
func isTTY(f *os.File) bool {
	var t syscall.Termios
	return ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&t)) == nil
}

// makeRaw puts the terminal into raw mode and returns the old state.
func makeRaw(f *os.File) (*termState, error) {
	var t syscall.Termios
	if err := ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&t)); err != nil {
		return nil, err
	}
	old := t
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
	if err := ioctl(f.Fd(), syscall.TCSETS, unsafe.Pointer(&t)); err != nil {
		return nil, err
	}
	return &termState{term: old}, nil
}

func restoreTerm(f *os.File, st *termState) {
	if st == nil {
		return
	}
	_ = ioctl(f.Fd(), syscall.TCSETS, unsafe.Pointer(&st.term))
}

// terminalSize returns the current terminal size in cells.
func terminalSize(f *os.File) (w, h int, ok bool) {
	var ws winsize
	if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return 0, 0, false
	}
	if ws.Col == 0 || ws.Row == 0 {
		return 0, 0, false
	}
	return int(ws.Col), int(ws.Row), true
}

// terminalCellPixels returns the approximate pixel size of one terminal cell,
// when the terminal reports pixel dimensions.
func terminalCellPixels(f *os.File) (w, h int, ok bool) {
	var ws winsize
	if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return 0, 0, false
	}
	if ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
		return 0, 0, false
	}
	w = int(ws.Xpixel) / int(ws.Col)
	h = int(ws.Ypixel) / int(ws.Row)
	if w < 1 || h < 1 {
		return 0, 0, false
	}
	return w, h, true
}
