package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/docker/machine/libmachine/drivers/plugin"
)

func main() {
	version := flag.Bool("v", false, "prints current docker-machine-driver-kubernetes version")
	flag.Parse()
	if *version {
		fmt.Printf("Version: %s\n", "1.0.0")
		os.Exit(0)
	}
	plugin.RegisterDriver(NewDriver("",""))
}
