package main

import (
	"log"

	"jt808-broker/internal/config"
	"jt808-broker/internal/http"
	"jt808-broker/internal/stream"
	"jt808-broker/internal/tcp"
)

func main() {
	log.Println("[MAIN] Starting JT808 broker server...")
	cfg := config.Default()
	log.Printf("[MAIN] Configuration loaded: TCPAddr=%s, StreamRoot=%s\n", cfg.TCPListenAddr, cfg.StreamRoot)

	router := stream.NewRouter(cfg.StreamRoot)

	// Start JT1078 Media Server (SEPARATE from JT808 signaling)
	mediaServer, err := tcp.NewMediaServer(":6208", router)
	if err != nil {
		log.Fatalf("[MAIN] Failed to create media server: %v\n", err)
	}
	go func() {
		log.Printf("[MAIN] Starting JT1078 media server on port %d...\n", mediaServer.GetPort())
		if err := mediaServer.Start(); err != nil {
			log.Printf("[MAIN] Media server error: %v\n", err)
		}
	}()

	// Store media port in router for building 0x9101 commands
	router.MediaPort = mediaServer.GetPort()

	// Start HTTP API
	api := http.NewAPI(router)
	go func() {
		if err := api.Start(":8189"); err != nil {
			log.Printf("[MAIN] HTTP API error: %v\n", err)
		}
	}()

	log.Println("[MAIN] Router initialized, starting TCP listener...")
	log.Fatal(tcp.Listen(cfg.TCPListenAddr, router))
}
