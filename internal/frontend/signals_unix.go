//go:build unix

package frontend

import (
	"os"
	"os/signal"
	"syscall"
)

func StartSignalHandlers(resize, quit chan<- os.Signal) {
	signal.Notify(resize, syscall.SIGWINCH)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
}

func StopSignalHandlers(resize, quit chan<- os.Signal) {
	signal.Stop(resize)
	signal.Stop(quit)
}
