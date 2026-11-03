// Command pigcap is the clone's standalone Agent Client Protocol (ACP) server:
// the reimplementation of Google's `agy-acp-server` (`agy_acp_server.par`).
//
// The reference binary is distributed via the official ACP registry:
// https://github.com/agentclientprotocol/registry/tree/main/antigravity-acp
//
// It provides a direct, standalone entrypoint for ACP clients (such as Zed Editor)
// without having to invoke `pigasus acp`. By default, it communicates via line-delimited
// JSON-RPC 2.0 over standard input and standard output.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/justwasm/pigasus/internal/cliapp"
	"github.com/justwasm/pigasus/internal/core/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	for _, a := range args {
		if a == "-v" || a == "--version" || a == "-version" {
			fmt.Fprintf(out, "pigcap %s (agy-acp-server clone)\n", version.Reference)
			return 0
		}
		if a == "-h" || a == "--help" || a == "-help" {
			printUsage(errOut)
			return 0
		}
		if a == "--notices" {
			printNotices(out)
			return 0
		}
	}

	if err := cliapp.RunAcpStandalone(args, in, out, errOut); err != nil {
		fmt.Fprintf(errOut, "pigcap: %v\n", err)
		return 1
	}
	return 0
}

func printNotices(w io.Writer) {
	fmt.Fprintf(w, `Google Antigravity Agent Client Protocol (ACP) Server
Copyright 2026 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Third-Party Notices and Licenses:
Pigasus incorporates open-source components including Agent Client Protocol
(ACP) specifications, ironpark/acp-go, and standard library transports.
`)
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: pigcap [flags]

Standalone Agent Client Protocol (ACP) server for Pigasus / Google Antigravity.
Speaks line-delimited JSON-RPC 2.0 over standard I/O (default) or TCP.

Flags:
  --stdio              Serve ACP over standard I/O (default)
  --addr <addr>        TCP bind address (e.g. 127.0.0.1:0)
  --port <port>        TCP port override
  --workspace, -w <dir> Root project workspace directory (default: current directory)
  --model <model>      Model name to serve
  --effort <effort>    Reasoning effort
  --acp-version <ver>  ACP protocol version (auto, 1, 2; default: auto)
  --uid <token>        Compatibility flag matching agy-acp-server Linux distribution
  --debug              Enable debug logging
  --notices            Print licenses and notices, then exit
  -v, --version        Show version
  -h, --help           Show help
`)
}
