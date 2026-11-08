//go:build !unix

package browser

import "os"

func startSignalHandlers(resize, quit chan<- os.Signal) {}

func stopSignalHandlers(resize, quit chan<- os.Signal) {}
