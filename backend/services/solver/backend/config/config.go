// config/config.go

package config

import "os"

type Config struct {
	HTTPAddr   string
	SolverAddr string
}

func Load() Config {
	return Config{
		HTTPAddr:   envOr("ARRANGO_HTTP_ADDR", ":8080"),
		SolverAddr: envOr("ARRANGO_SOLVER_ADDR", "127.0.0.1:50061"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
