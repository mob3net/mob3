//go:build unix

package browser

import (
	"os"
	"os/signal"
	"syscall"
)

func startSignalHandlers(resize, quit chan<- os.Signal) {
	signal.Notify(resize, syscall.SIGWINCH)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
}

func stopSignalHandlers(resize, quit chan<- os.Signal) {
	signal.Stop(resize)
	signal.Stop(quit)
}
