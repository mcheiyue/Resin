package main

import (
	"log"

	"github.com/Resinat/Resin/desktop/internal/wailsapp"
)

func main() {
	shell := wailsapp.NewShell()
	log.Printf("%s skeleton initialized with placeholder frontend at %s", shell.Name, shell.FrontendDir)
}
