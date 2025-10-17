package main

import (
	"os"

	"github.com/avivsinai/jenkins-cli/internal/jkcmd"
)

func main() {
	os.Exit(jkcmd.Main())
}
