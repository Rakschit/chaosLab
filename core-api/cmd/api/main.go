package main

import (
	"log"
	"net/http"
	"os"
	"time"

	router "core-api"
)

// Vercel's Go preset runs this binary and routes every request to it.
// The server must listen on the PORT environment variable.
func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           router.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}
