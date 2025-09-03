package main

import (
	"log"

	"github.com/austindbirch/harbor_hook/cmd/harborctl/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
