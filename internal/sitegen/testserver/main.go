package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	dir := flag.String("dir", "_site", "directory to serve")
	port := flag.String("port", "8080", "port to listen on")
	flag.Parse()

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("failed to get absolute path: %v", err)
	}

	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		log.Fatalf("directory does not exist: %s", absDir)
	}

	fs := http.FileServer(http.Dir(absDir))
	http.Handle("/", fs)

	addr := "0.0.0.0:" + *port
	fmt.Printf("Serving %s at:\n", absDir)
	fmt.Printf("  - http://localhost:%s (from host)\n", *port)
	fmt.Printf("  - http://host.docker.internal:%s (from Docker)\n", *port)
	fmt.Println("Press Ctrl+C to stop")
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

