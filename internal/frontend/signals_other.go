//go:build !unix

package frontend

import "os"

func StartSignalHandlers(resize, quit chan<- os.Signal) {}

func StopSignalHandlers(resize, quit chan<- os.Signal) {}
