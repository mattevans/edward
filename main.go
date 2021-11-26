package main

import (
	"github.com/mattevans/edward/cmd"
)

func main() {
	// Initialization
	RegisterBackends()

	cmd.Execute()
}
