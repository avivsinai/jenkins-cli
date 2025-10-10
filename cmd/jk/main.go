package main

import (
	"os"

	"github.com/your-org/jenkins-cli/internal/jkcmd"
)

func main() {
	os.Exit(jkcmd.Main())
}
