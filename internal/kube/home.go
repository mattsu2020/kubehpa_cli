package kube

import "os"

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
