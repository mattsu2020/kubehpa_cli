package main

import (
	"os"

	"github.com/matsui/kubectl-hpa-status/cmd"
)

func main() {
	if err := cmd.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
