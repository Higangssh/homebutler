package cmd

import (
	"strconv"

	"github.com/Higangssh/homebutler/internal/config"
	"github.com/Higangssh/homebutler/internal/server"
)

func runServe(cfg *config.Config, version string) error {
	host := getFlag("--host", "127.0.0.1")
	port := 8080
	if v := getFlag("--port", ""); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return err
		}
		port = p
	}

	demo := hasFlag("--demo")

	srv := server.New(cfg, host, port, demo)
	srv.SetVersion(version)
	return srv.Run()
}
