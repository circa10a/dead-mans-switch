package server

import (
	"fmt"

	"github.com/fatih/color"
)

const logo = ` ___  __  __ ___
|   \|  \/  / __|
| |) | |\/| \__ \
|___/|_|  |_|___/`

func (s *Server) printBanner(addr string) {
	accent := color.New(color.FgHiCyan, color.Bold)
	name := color.New(color.FgHiWhite, color.Bold)
	label := color.New(color.FgHiBlack)
	val := color.New(color.FgHiGreen)

	fmt.Println()
	accent.Println(logo)
	name.Println("  Dead Man's Switch")
	fmt.Println()

	info := func(k, v string) {
		label.Printf("  %-10s", k)
		val.Println(v)
	}

	info("listen", addr)

	if s.Metrics {
		info("metrics", "/metrics")
	}
	if s.AuthEnabled {
		info("auth", s.AuthIssuerURL)
	}
	if s.AutoTLS {
		info("tls", "auto (Let's Encrypt)")
	} else if s.TLSCert != "" {
		info("tls", "custom certificate")
	}
	if s.DemoMode {
		info("demo", "enabled")
	}

	fmt.Println()
}
