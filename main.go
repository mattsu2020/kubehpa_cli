package main

import (
	"os"

	"github.com/mattsu2020/kubectl-hpa-status/cmd"
)

func main() {
	if err := cmd.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
