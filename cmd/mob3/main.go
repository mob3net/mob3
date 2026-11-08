package main

import (
	"os"

	"browser/internal/browser"
)

func main() {
	os.Exit(browser.Run(os.Args[1:]))
}
