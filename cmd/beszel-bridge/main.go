// Command beszel-bridge translates a service's health status in a
// self-hosted Beszel instance (https://github.com/henrygd/beszel) into a
// plain HTTP status code, for status-check widgets (e.g. Dashy) that expect
// one.
//
// Dashy only looks at the HTTP status code of whatever URL you give it.
// Beszel's own API always returns 200 with a JSON body describing status,
// which Dashy can't interpret. This bridge asks Beszel "is this container
// healthy?" and answers with a plain HTTP status code instead:
//
//	200  -> container/system is up
//	503  -> container/system is down or unhealthy
//	404  -> couldn't find a system/container with that name
//	502  -> couldn't reach Beszel at all
//
// Usage from Dashy conf.yml:
//
//	statusCheckUrl: http://beszel-bridge:8123/status/<system_name>/<container_name>
//
// Stdlib only - no third-party dependencies.
package main

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"beszel-bridge/internal/beszel"
)

// handleStatus serves GET /status/<system_name>/<container_name>
func handleStatus(client *beszel.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "status" {
			http.Error(w, "usage: /status/<system_name>/<container_name>", http.StatusBadRequest)
			return
		}

		system, err1 := url.PathUnescape(parts[1])
		container, err2 := url.PathUnescape(parts[2])
		if err1 != nil || err2 != nil {
			http.Error(w, "invalid path segment", http.StatusBadRequest)
			return
		}

		w.WriteHeader(client.CheckContainer(beszel.Target{System: system, Container: container}))
	}
}

func main() {
	cfg := loadConfig()

	client := beszel.New(cfg.BeszelURL, cfg.BeszelCreds)
	if err := client.Authenticate(); err != nil {
		log.Fatalf("authenticate with beszel: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status/", handleStatus(client))

	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("beszel-bridge listening on %s, backed by %s", addr, cfg.BeszelURL)
	log.Fatal(srv.ListenAndServe())
}
