package config

import "os"

// DatabaseURL returns the Postgres DSN from the environment, falling back to
// a sane local-dev default so every service can be run without extra setup.
func DatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://linkstash:linkstash@localhost:5432/linkstash?sslmode=disable"
}

// Addr returns the listen address for an HTTP service, honoring PORT.
func Addr(defaultPort string) string {
	if v := os.Getenv("PORT"); v != "" {
		return ":" + v
	}
	return ":" + defaultPort
}
