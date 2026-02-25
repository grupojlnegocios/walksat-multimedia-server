package stream

import (
	"bufio"
	"log"
	"net"
)

type Parser interface {
	Push([]byte) [][]byte
}

type VideoWriter interface {
	Write([]byte)
}

type Session struct {
	Conn     net.Conn
	DeviceID string
	Parser   Parser
	Writer   VideoWriter
}

func (s *Session) Run() {
	log.Printf("[SESSION] Starting session for device %s\n", s.DeviceID)
	reader := bufio.NewReader(s.Conn)

	for {
		buf := make([]byte, 4096)
		n, err := reader.Read(buf)
		if err != nil {
			log.Printf("[SESSION] Connection closed for device %s: %v\n", s.DeviceID, err)
			return
		}

		log.Printf("[SESSION] Received %d bytes from device %s\n", n, s.DeviceID)
		payloads := s.Parser.Push(buf[:n])
		log.Printf("[SESSION] Parsed %d video payloads from device %s\n", len(payloads), s.DeviceID)

		for _, p := range payloads {
			log.Printf("[SESSION] Writing %d bytes of video data for device %s\n", len(p), s.DeviceID)
			s.Writer.Write(p)
		}
	}
}
