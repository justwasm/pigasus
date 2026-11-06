package main

import (
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/infra/agyproxy"
)

func main() {
	opts := agyproxy.DefaultOptions()
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"serve"}
	} else if strings.HasPrefix(args[0], "-") {
		args = append([]string{"serve"}, args...)
	}
	os.Exit(agyproxy.Admin(opts, args))
}
