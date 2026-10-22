//go:build pprof

package main

// pprof.go serves net/http/pprof on CLIAMP_PPROF (e.g. 127.0.0.1:6060) in
// builds made with -tags pprof. Release builds do not include it.

import (
	"net/http"
	_ "net/http/pprof"
	"os"
)

func init() {
	addr := os.Getenv("CLIAMP_PPROF")
	if addr == "" {
		return
	}
	go func() { _ = http.ListenAndServe(addr, nil) }()
}
