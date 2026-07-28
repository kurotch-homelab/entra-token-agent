// Command entra-token-agent signs a user in to Microsoft Entra ID via the
// OAuth 2.0 device authorization grant and keeps a fresh bearer token written
// to a file for bearer_token_file consumers (Prometheus / Grafana Alloy).
//
// This is an early scaffold: the login/refresh loop is not implemented yet.
// See README.md and docs/design.md for the intended design.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	var (
		configPath  = flag.String("config", "entra-token-agent.yaml", "path to config file")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("entra-token-agent %s\n", version)
		return
	}

	fmt.Fprintf(os.Stderr, "entra-token-agent %s\n", version)
	fmt.Fprintf(os.Stderr, "config: %s\n", *configPath)
	fmt.Fprintln(os.Stderr, "not implemented yet — see README.md roadmap")
	// TODO: load config
	// TODO: MSAL device code flow login (cache refresh token in OS keystore)
	// TODO: refresh loop -> atomic write access token to token_file
	// TODO: expose self-metrics (last_refresh_success_timestamp, expiry, failures)
	os.Exit(0)
}
