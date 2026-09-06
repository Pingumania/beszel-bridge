package main

import (
	"log"
	"os"
)

type config struct {
	BeszelURL      string
	BeszelEmail    string
	BeszelPassword string
	Port           string
}

func loadConfig() config {
	return config{
		BeszelURL:      getenv("BESZEL_URL", "http://beszel:8090"),
		BeszelEmail:    mustGetenv("BESZEL_EMAIL"),
		BeszelPassword: mustGetenv("BESZEL_PASSWORD"),
		Port:           getenv("PORT", "8123"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetenv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required environment variable %s", key)
	}
	return v
}
