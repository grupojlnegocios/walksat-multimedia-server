package tcp

import (
	"log"
	"net"

	"jt808-broker/internal/stream"
)

func Listen(addr string, router *stream.Router) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	log.Printf("[LISTENER] TCP listening on %s\n", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[LISTENER] Error accepting connection: %v\n", err)
			continue
		}
		log.Printf("[LISTENER] Accepted connection from %s\n", conn.RemoteAddr())
		go handle(conn, router)
	}
}
