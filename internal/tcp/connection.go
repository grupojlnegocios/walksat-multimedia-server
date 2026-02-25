// internal/tcp/connection.go
package tcp

import (
	"log"
	"net"

	"jt808-broker/internal/stream"
)

func handle(conn net.Conn, router *stream.Router) {
	log.Printf("[TCP] New connection from %s\n", conn.RemoteAddr())
	defer func() {
		log.Printf("[TCP] Closing connection from %s\n", conn.RemoteAddr())
		conn.Close()
	}()
	router.Handle(conn)
}
