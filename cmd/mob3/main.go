package main

import (
	"os"

	"browser/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
