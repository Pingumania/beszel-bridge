package main

import (
	"log"
	"os"

	"beszel-bridge/internal/beszel"
)

type config struct {
	BeszelURL   string
	BeszelCreds beszel.Credentials
	Port        string
}

func loadConfig() config {
	return config{
		BeszelURL: getenv("BESZEL_URL", "http://beszel:8090"),
		BeszelCreds: beszel.Credentials{
			Email:    mustGetenv("BESZEL_EMAIL"),
			Password: mustGetenv("BESZEL_PASSWORD"),
		},
		Port: getenv("PORT", "8123"),
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
