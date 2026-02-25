package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"
)

// JT1078 Media Test Client
// Sends fragmentedH.264 stream data via JT/T 1078 protocol

// JT1078Header represents the 30-byte JT/T 1078-2016 header
type JT1078Header struct {
	FrameHeader    [4]byte // 0x30 0x31 0x63 0x64
	V_P_X_CC       uint8   // Version/Padding/Extension/CSRC count
	M_PT           uint8   // Marker bit + Payload Type
	PacketSN       uint16  // Packet Sequence Number
	SIM            [6]byte // BCD encoded device ID
	LogicalChannel uint8   // Logical channel number
	DataType_Mark  uint8   // High 4 bits: DataType, Low 4 bits: Subpacket Mark
	PTSMs          [4]byte // PTS in milliseconds
	Timescale      uint32  // Timescale
}

const (
	MarkFIRST  = 0b0001
	MarkMIDDLE = 0b0011
	MarkLAST   = 0b0010
	MarkATOMIC = 0b0000
)

func encodeDeviceID(id string) [6]byte {
	var result [6]byte
	for i := 0; i < len(id) && i < 6; i++ {
		h := id[i] / 10
		l := id[i] % 10
		result[i] = (h << 4) | l
	}
	return result
}

func buildH264SPS() []byte {
	// Valid minimal SPS
	return []byte{
		0x00, 0x00, 0x00, 0x01, // Start code (4 bytes)
		0x67, // NAL type 7 (SPS)
		0x42, 0x00, 0x0a, 0xff, 0xe1, 0x00, 0x16, 0x68,
		0xd9, 0x40, 0x50, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
}

func buildH264PPS() []byte {
	// Valid minimal PPS
	return []byte{
		0x00, 0x00, 0x00, 0x01, // Start code (4 bytes)
		0x68, // NAL type 8 (PPS)
		0xae, 0x08,
	}
}

func buildIDRFrame() []byte {
	// Minimal IDR frame
	return []byte{
		0x00, 0x00, 0x00, 0x01, // Start code (4 bytes)
		0x65, // NAL type 5 (IDR)
		// Dummy frame data
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
}

func buildJT1078Header(mark uint8, packetSN uint16) JT1078Header {
	return JT1078Header{
		FrameHeader:    [4]byte{0x30, 0x31, 0x63, 0x64}, // "01cd" marker
		V_P_X_CC:       0x80,                            // Version=2, others=0
		M_PT:           0x20 | mark,                     // Marker bit + payload type
		PacketSN:       packetSN,
		SIM:            encodeDeviceID("011993493643"), // Device ID
		LogicalChannel: 1,                              // Channel 1
		DataType_Mark:  0x01 | mark,                    // DataType=0 (video), Mark=mark
		PTSMs:          [4]byte{0, 0, 0, 0},
		Timescale:      90000,
	}
}

func serializeHeader(h JT1078Header, payloadSize int) []byte {
	buf := new(bytes.Buffer)
	buf.Write(h.FrameHeader[:])
	buf.WriteByte(h.V_P_X_CC)
	buf.WriteByte(h.M_PT)
	binary.Write(buf, binary.BigEndian, h.PacketSN)
	buf.Write(h.SIM[:])
	buf.WriteByte(h.LogicalChannel)
	buf.WriteByte(h.DataType_Mark)
	buf.Write(h.PTSMs[:])
	// Last 12 bytes: DataLength (4) + Timestamp (8)
	binary.Write(buf, binary.BigEndian, uint32(payloadSize))            // DataLength = payload size
	binary.Write(buf, binary.BigEndian, uint64(time.Now().UnixMilli())) // Timestamp in milliseconds
	return buf.Bytes()
}

func sendFrame(conn net.Conn, payload []byte, frameType string) error {
	// Fragment large payloads
	maxFragmentSize := 1400 // MTU - header size
	totalFragments := (len(payload) + maxFragmentSize - 1) / maxFragmentSize

	log.Printf("📨 Sending %s: %d bytes in %d fragments\n", frameType, len(payload), totalFragments)

	for i := 0; i < totalFragments; i++ {
		start := i * maxFragmentSize
		end := start + maxFragmentSize
		if end > len(payload) {
			end = len(payload)
		}
		fragment := payload[start:end]

		var mark uint8
		if i == 0 && totalFragments == 1 {
			mark = MarkATOMIC
		} else if i == 0 {
			mark = MarkFIRST
		} else if i == totalFragments-1 {
			mark = MarkLAST
		} else {
			mark = MarkMIDDLE
		}

		header := buildJT1078Header(mark, uint16(i))
		packet := append(serializeHeader(header, len(fragment)), fragment...)

		if _, err := conn.Write(packet); err != nil {
			return fmt.Errorf("send error: %v", err)
		}

		log.Printf("  Fragment %d/%d: Mark=0x%x, Size=%d bytes\n", i+1, totalFragments, mark, len(fragment))
		time.Sleep(50 * time.Millisecond) // Small delay between fragments
	}

	return nil
}

func main() {
	// Connect to media server
	conn, err := net.Dial("tcp", "127.0.0.1:6208")
	if err != nil {
		log.Fatalf("❌ Failed to connect to media server on 127.0.0.1:6208\n")
		log.Fatalf("   Error: %v\n", err)
		log.Printf("\n   Make sure the server is running:\n")
		log.Printf("   $ cd /home/grupo-jl/jt808-broker && go run ./cmd/server/main.go\n")
		return
	}
	defer conn.Close()

	log.Printf("✅ Connected to media server at 127.0.0.1:6208\n")

	// Send SPS
	if err := sendFrame(conn, buildH264SPS(), "SPS (type 7)"); err != nil {
		log.Fatalf("Error sending SPS: %v\n", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Send PPS
	if err := sendFrame(conn, buildH264PPS(), "PPS (type 8)"); err != nil {
		log.Fatalf("Error sending PPS: %v\n", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Send IDR frame
	if err := sendFrame(conn, buildIDRFrame(), "IDR frame (type 5)"); err != nil {
		log.Fatalf("Error sending IDR: %v\n", err)
	}

	log.Printf("\n✅ All frames sent successfully!\n")
	log.Printf("Check: ls -lh /home/grupo-jl/jt808-broker/streams/\n")
	log.Printf("Test: ffplay /home/grupo-jl/jt808-broker/streams/*.h264\n")
}
