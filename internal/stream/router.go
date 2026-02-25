// internal/stream/router.go
package stream

import (
	"log"
	"net"

	"jt808-broker/internal/protocol"
)

type Router struct {
	StreamRoot      string
	MultimediaStore *MultimediaStore
	DeviceRegistry  *DeviceRegistry
	MediaPort       uint16 // Port for JT1078 media server
}

func NewRouter(streamRoot string) *Router {
	return &Router{
		StreamRoot:      streamRoot,
		MultimediaStore: NewMultimediaStore(streamRoot),
		DeviceRegistry:  NewDeviceRegistry(),
		MediaPort:       6208, // Default media port
	}
}

func (r *Router) Handle(conn net.Conn) {
	log.Printf("[ROUTER] Handling connection from %s\n", conn.RemoteAddr())

	// Peek at first byte to determine protocol
	// JT808 and JT1078 both start with 0x7E
	// We'll try JT808 first and handle GPS tracking
	log.Println("[ROUTER] Creating JT808 parser for GPS tracking")

	parser := protocol.NewJT808()
	session := &JT808Session{
		Conn:            conn,
		Parser:          parser,
		MultimediaStore: r.MultimediaStore,
		Registry:        r.DeviceRegistry,
	}

	log.Printf("[ROUTER] Starting JT808 session for %s\n", conn.RemoteAddr())
	session.Run()
}

// HandleVideo handles video streaming connections (JT1078)
// Note: JT1078Parser implementation is in protocol/jt1078.go
// Integration with ffmpeg.Worker needs to be completed separately
func (r *Router) HandleVideo(conn net.Conn) {
	log.Printf("[ROUTER] Handling video connection from %s\n", conn.RemoteAddr())
	log.Printf("[ROUTER] Video streaming (JT1078) needs integration with VideoFrameHandler\n")
	conn.Close()
}
