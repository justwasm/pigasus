package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/justwasm/pigasus/internal/ui/livechat"
)

func main() {
	if err := livechat.Run(os.Args[1:]); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}
