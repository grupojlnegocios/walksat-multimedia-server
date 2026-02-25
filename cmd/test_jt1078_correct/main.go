package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"
)

// JT1078 Media Test Client - CORRECTED
// Sends H.264 frames via JT/T 1078 protocol

// buildH264SPS creates a valid SPS NAL unit
// Uses a known-good SPS from FFmpeg that produces 640x480 video
func buildH264SPS() []byte {
	// This is a valid SPS extracted from FFmpeg samples
	// Baseline profile, Level 4.2, 640x480 resolution
	return []byte{
		0x00, 0x00, 0x00, 0x01, // Start code
		0x67,                                                       // NAL type 7 (SPS)
		0x42, 0xc0, 0x1f, 0xff, 0xe0, 0x50, 0x00, 0x00, 0xf4, 0x20, // Profile/Level + dimensions
		0x00, 0x62, 0x4b, 0x90, // Frame params
	}
}

// buildH264PPS creates a valid minimal PPS NAL unit
func buildH264PPS() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x01, // Start code (4 bytes)
		0x68, // NAL type 8 (PPS)
		0xae, 0x08,
	}
}

// buildIDRFrame creates a minimal IDR frame
func buildIDRFrame() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x01, // Start code (4 bytes)
		0x65, // NAL type 5 (IDR)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
}

// encodeDeviceID converts device ID string to BCD bytes
// Example: "011993493643" → 01 19 93 49 36 43 (6 bytes BCD)
func encodeDeviceID(id string) [6]byte {
	var result [6]byte
	// Convert first 12 characters of ID string to 6 BCD bytes
	// Each pair of characters becomes one BCD byte
	for i := 0; i < 6 && i*2+1 < len(id); i++ {
		// High nibble from first digit of pair
		h := id[i*2] - '0'
		// Low nibble from second digit of pair
		l := id[i*2+1] - '0'
		result[i] = (h << 4) | l
	}
	return result
}

// buildJT1078Header constructs a proper JT/T 1078-2016 header
// This must match what parseJT1078Header in media_listener.go expects (30 bytes total):
// [0-3]   Sync: 0x30 0x31 0x63 0x64
// [4]     V_P_X_CC (RTP version/padding/extension/CSRC count)
// [5]     M_PT (Marker/PayloadType)
// [6-7]   PacketSN (Sequence Number)
// [8-13]  Device ID (BCD)
// [14]    Channel ID
// [15]    DataType_Mark (data type + mark bits)
// [16-23] Timestamp (8 bytes)
// [24-25] LastIFrameInterval
// [26-27] LastFrameInterval
// [28-29] DataBodyLength (payload size)
func buildJT1078Header(mark uint8, payloadSize int) []byte {
	buf := new(bytes.Buffer)

	// [0-3] Sync word
	buf.Write([]byte{0x30, 0x31, 0x63, 0x64})

	// [4] V_P_X_CC: Version(2)=2, Padding(1)=0, Extension(1)=0, CC(4)=1
	buf.WriteByte(0x81) // 10000001

	// [5] M_PT: Marker(1)=0, PayloadType(7)=0x20
	buf.WriteByte(0x20)

	// [6-7] PacketSN (sequence number)
	binary.Write(buf, binary.BigEndian, uint16(0))

	// [8-13] Device ID (BCD: 011993493643)
	deviceID := encodeDeviceID("011993493643")
	buf.Write(deviceID[:])

	// [14] Channel ID
	buf.WriteByte(1) // Channel 1

	// [15] DataType_Mark (bit 7-4: DataType, bit 3-0: Mark)
	buf.WriteByte(mark)

	// [16-23] Timestamp (8 bytes) - milliseconds
	binary.Write(buf, binary.BigEndian, uint64(time.Now().UnixMilli()))

	// [24-25] LastIFrameInterval (0 if none)
	binary.Write(buf, binary.BigEndian, uint16(0))

	// [26-27] LastFrameInterval (0 if none)
	binary.Write(buf, binary.BigEndian, uint16(0))

	// [28-29] DataBodyLength (payload size)
	binary.Write(buf, binary.BigEndian, uint16(payloadSize))

	return buf.Bytes()
}

// sendFrame sends a frame (SPS, PPS, or IDR) via JT1078
func sendFrame(conn net.Conn, payload []byte, frameType string) error {
	const maxFragmentSize = 1400 // MTU - header size

	// Calculate fragments
	totalFragments := (len(payload) + maxFragmentSize - 1) / maxFragmentSize

	log.Printf("📨 Sending %s: %d bytes in %d fragments\n", frameType, len(payload), totalFragments)

	for i := 0; i < totalFragments; i++ {
		start := i * maxFragmentSize
		end := start + maxFragmentSize
		if end > len(payload) {
			end = len(payload)
		}
		fragment := payload[start:end]

		// Mark field (JT1078 fragmentation):
		// 0x0: Atomic (not fragmented)
		// 0x1: First fragment
		// 0x3: Middle fragment
		// 0x2: Last fragment
		var mark uint8
		if totalFragments == 1 {
			mark = 0x0 // Atomic
		} else if i == 0 {
			mark = 0x1 // First
		} else if i == totalFragments-1 {
			mark = 0x2 // Last
		} else {
			mark = 0x3 // Middle
		}

		header := buildJT1078Header(mark, len(fragment))
		packet := append(header, fragment...)

		if _, err := conn.Write(packet); err != nil {
			return fmt.Errorf("send error: %v", err)
		}

		log.Printf("  Fragment %d/%d: Mark=0x%x, Size=%d bytes\n", i+1, totalFragments, mark, len(fragment))
		time.Sleep(50 * time.Millisecond)
	}

	return nil
}

func main() {
	// Connect to media server
	conn, err := net.Dial("tcp", "127.0.0.1:6208")
	if err != nil {
		log.Fatalf("❌ Failed to connect to media server on 127.0.0.1:6208\n")
		log.Fatalf("   Error: %v\n", err)
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
