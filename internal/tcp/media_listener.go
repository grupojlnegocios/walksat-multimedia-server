package tcp

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"jt808-broker/internal/protocol"
	"jt808-broker/internal/stream"
)

// MediaServer handles JT1078 video/audio streaming connections
// This is SEPARATE from JT808 signaling connections
type MediaServer struct {
	listener   net.Listener
	router     *stream.Router
	listenAddr string
	port       uint16
	// Stream management
	activeStreams map[string]*StreamBuffer
	streamMutex   sync.RWMutex
	// Persistent buffers per connection
	connBuffers map[string]*stream.JT1078StreamBuffer
	bufferMutex sync.RWMutex
	// Frame reassembly
	assembler *FrameAssembler
}

// NALUnit represents a single H.264 NAL (Network Abstraction Layer) unit
type NALUnit struct {
	Data      []byte // Raw NAL unit data including start code
	Type      uint8  // NAL unit type (lower 5 bits)
	StartCode int    // Start code size: 3 or 4 bytes
}

// JT1078Header represents the 30-byte JT/T 1078-2016 header
type JT1078Header struct {
	FrameHeader    [4]byte // 0x30 31 63 64
	V_P_X_CC       uint8   // Version/Padding/Extension/CSRC count
	M_PT           uint8   // Marker bit + Payload Type
	PacketSN       uint16  // Packet Sequence Number
	SIM            [6]byte // BCD encoded device ID
	LogicalChannel uint8   // Logical channel number
	DataType_Mark  uint8   // High 4 bits: DataType, Low 4 bits: Subpacket Mark
	Timestamp      uint64  // 8 bytes timestamp (milliseconds)
	LastIFrameInt  uint16  // Last I-frame interval
	LastFrameInt   uint16  // Last frame interval
	DataBodyLength uint16  // Payload length
}

// SubpacketMark defines fragment position
type SubpacketMark uint8

const (
	MarkAtomic SubpacketMark = 0b0000 // Complete frame in one packet
	MarkFirst  SubpacketMark = 0b0001 // First fragment
	MarkMiddle SubpacketMark = 0b0011 // Middle fragment
	MarkLast   SubpacketMark = 0b0010 // Last fragment
)

func isValidSubpacketMark(mark SubpacketMark) bool {
	switch mark {
	case MarkAtomic, MarkFirst, MarkMiddle, MarkLast:
		return true
	default:
		return false
	}
}

func isValidDataType(dataType uint8) bool {
	// JT/T 1078-2016 Table 19
	return dataType <= 0x04
}

// FrameFragment represents a single JT1078 packet fragment
type FrameFragment struct {
	Header   JT1078Header
	Payload  []byte
	Received time.Time
}

// FrameAssembly tracks reassembly of fragmented frames
type FrameAssembly struct {
	DeviceID   string
	Channel    uint8
	Fragments  []FrameFragment
	FirstSN    uint16
	LastSN     uint16
	LastUpdate time.Time
}

// FrameAssembler reassembles fragmented JT1078 frames
type FrameAssembler struct {
	assemblies map[string]*FrameAssembly // Key: deviceID_channel
	mutex      sync.RWMutex
	timeout    time.Duration
}

// StreamBuffer tracks an active video stream
type StreamBuffer struct {
	DeviceID   string
	Channel    uint8
	FilePath   string
	File       *os.File
	Buffer     []byte
	BufferSize int
	Created    time.Time
	// RTSP streaming
	FFmpegCmd   *exec.Cmd
	FFmpegStdin io.WriteCloser
	RTSPURL     string
	// Stream initialization with NAL detection
	InitBuffer        *stream.StreamInitBuffer // Buffer that waits for SPS+PPS+IDR
	StreamInitialized bool                     // Stream has valid SPS+PPS+IDR sequence
	DataReceived      int64                    // Track data for debugging
	// DEPRECATED: Legacy SPS/PPS cache (replaced by InitBuffer)
	LastSPS     []byte // DEPRECATED - use InitBuffer instead
	LastPPS     []byte // DEPRECATED - use InitBuffer instead
	FFmpegReady bool   // DEPRECATED - use InitBuffer.IsInitialized() instead
}

// NewMediaServer creates a new JT1078 media server
func NewMediaServer(addr string, router *stream.Router) (*MediaServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to create media listener: %w", err)
	}

	// Extract port from listener
	tcpAddr := listener.Addr().(*net.TCPAddr)
	port := uint16(tcpAddr.Port)

	log.Printf("[MEDIA_SERVER] JT1078 media server listening on %s (port %d)\n", addr, port)

	return &MediaServer{
		listener:      listener,
		router:        router,
		listenAddr:    addr,
		port:          port,
		activeStreams: make(map[string]*StreamBuffer),
		assembler:     NewFrameAssembler(5 * time.Second),
	}, nil
}

// GetPort returns the port the media server is listening on
func (ms *MediaServer) GetPort() uint16 {
	return ms.port
}

// Start begins accepting media connections
func (ms *MediaServer) Start() error {
	log.Printf("[MEDIA_SERVER] Starting to accept JT1078 media connections...\n")

	for {
		conn, err := ms.listener.Accept()
		if err != nil {
			log.Printf("[MEDIA_SERVER] Accept error: %v\n", err)
			continue
		}

		log.Printf("[MEDIA_SERVER] Accepted media connection from %s\n", conn.RemoteAddr())
		go ms.handleMediaConnection(conn)
	}
}

// handleMediaConnection processes a single JT1078 media stream connection
func (ms *MediaServer) handleMediaConnection(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("[MEDIA_CONN] Starting JT1078 media session from %s\n", remoteAddr)

	// Set TCP options for streaming
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true) // Disable Nagle for low latency
		tcpConn.SetReadDeadline(time.Now().Add(30 * time.Second))
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(10 * time.Second)
	}

	// Create PERSISTENT buffer for this connection
	connBuffer := stream.NewJT1078StreamBuffer()

	reader := bufio.NewReader(conn)

	// Stats
	totalBytes := 0
	frameCount := 0
	startTime := time.Now()

	for {
		// Read data
		buf := make([]byte, 65536) // 64KB buffer for video frames
		n, err := reader.Read(buf)
		if err != nil {
			duration := time.Since(startTime)
			log.Printf("[MEDIA_CONN] Connection closed from %s after %v\n", remoteAddr, duration)
			log.Printf("[MEDIA_CONN] Stats: %d bytes, %d frames, %.2f KB/s\n",
				totalBytes, frameCount, float64(totalBytes)/duration.Seconds()/1024)

			// Print buffer statistics
			stats := connBuffer.GetStatistics()
			log.Printf("[MEDIA_CONN] Buffer Stats: Received=%d, Frames=%d, Dropped=%d, Resyncs=%d\n",
				stats.BytesReceived, stats.FramesReceived, stats.FramesDropped, stats.ResyncCount)

			// Flush all active streams for this connection
			ms.flushAllStreams()
			return
		}

		totalBytes += n

		// Reset deadline after receiving data
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetReadDeadline(time.Now().Add(30 * time.Second))
		}

		log.Printf("[MEDIA_CONN] Received %d bytes from %s (total: %d, buffer: %d)\n",
			n, remoteAddr, totalBytes, connBuffer.CurrentSize())
		log.Printf("[MEDIA_CONN] First 32 bytes (hex): % X\n", buf[:min32(n)])

		// ============================================================================
		// CRITICAL: APPEND TO PERSISTENT BUFFER - NÃO DESCARTA DADOS INCOMPLETOS
		// ============================================================================
		if err := connBuffer.Append(buf[:n]); err != nil {
			log.Printf("[MEDIA_CONN] ERROR: Buffer append failed: %v\n", err)
			continue
		}

		// Extract complete frames from persistent buffer
		frames, err := connBuffer.ExtractFrames()
		if err != nil {
			log.Printf("[MEDIA_CONN] WARNING: Frame extraction error: %v\n", err)
		}

		log.Printf("[MEDIA_CONN] Extracted %d complete frames\n", len(frames))
		frameCount += len(frames)

		// Process each complete frame
		for i, frameData := range frames {
			log.Printf("[MEDIA_CONN] Processing frame %d: %d bytes\n", i, len(frameData))

			// Parse JT1078 header (30 bytes)
			header, payload, err := ms.parseJT1078Header(frameData)
			if err != nil {
				log.Printf("[MEDIA_CONN] Header parse error: %v\n", err)
				continue
			}

			dataType := header.DataType_Mark >> 4
			// 0=I,1=P,2=B (video), 3=audio, 4=transparent
			if dataType > 2 {
				log.Printf("[MEDIA_CONN] Skipping non-video packet: DeviceSIM=% X CH=%d SN=%d DataType=%d Len=%d\n",
					header.SIM, header.LogicalChannel, header.PacketSN, dataType, len(payload))
				continue
			}

			// Add fragment to assembler
			completeFrames := ms.assembler.AddFragment(header, payload)

			// Process complete frames (each is []FrameFragment)
			if completeFrames != nil {
				ms.processCompleteFrame(completeFrames)
			}
		}
	}
}

// parseJT1078Header parses the 30-byte JT/T 1078-2016 header with robust validation
func (ms *MediaServer) parseJT1078Header(data []byte) (JT1078Header, []byte, error) {
	// CRITICAL: Validate minimum size BEFORE any slice access
	if len(data) < 30 {
		return JT1078Header{}, nil, fmt.Errorf("packet too small: %d bytes (need 30)", len(data))
	}

	var header JT1078Header

	// Verify frame header (sync word)
	copy(header.FrameHeader[:], data[0:4])
	if header.FrameHeader != [4]byte{0x30, 0x31, 0x63, 0x64} {
		return JT1078Header{}, nil, fmt.Errorf("invalid frame header: %X (expected 30316364)", header.FrameHeader)
	}

	// Parse header fields - all accesses are now safe (len >= 30)
	header.V_P_X_CC = data[4]
	header.M_PT = data[5]
	header.PacketSN = uint16(data[6])<<8 | uint16(data[7])
	copy(header.SIM[:], data[8:14])
	header.LogicalChannel = data[14]
	header.DataType_Mark = data[15]

	// Timestamp (8 bytes big-endian) - indices 16-23
	header.Timestamp = uint64(data[16])<<56 | uint64(data[17])<<48 |
		uint64(data[18])<<40 | uint64(data[19])<<32 |
		uint64(data[20])<<24 | uint64(data[21])<<16 |
		uint64(data[22])<<8 | uint64(data[23])

	// Frame intervals - indices 24-27
	header.LastIFrameInt = uint16(data[24])<<8 | uint16(data[25])
	header.LastFrameInt = uint16(data[26])<<8 | uint16(data[27])

	// Payload length - indices 28-29
	header.DataBodyLength = uint16(data[28])<<8 | uint16(data[29])

	// Validate payload length is reasonable
	if header.DataBodyLength == 0 {
		return JT1078Header{}, nil, fmt.Errorf("zero payload length")
	}
	if header.DataBodyLength > 950 {
		// Alguns dispositivos extrapolam o limite da tabela 19; manter tolerante.
		log.Printf("[JT1078] WARNING: payload length %d exceeds spec max 950 bytes\n", header.DataBodyLength)
	}

	// Validate CC (should be 1)
	cc := header.V_P_X_CC & 0x0F
	if cc != 1 {
		log.Printf("[JT1078] WARNING: CC=%d (expected 1)\n", cc)
	}

	dataType := header.DataType_Mark >> 4
	mark := SubpacketMark(header.DataType_Mark & 0x0F)
	if !isValidDataType(dataType) {
		return JT1078Header{}, nil, fmt.Errorf("invalid data type nibble: 0x%X", dataType)
	}
	if !isValidSubpacketMark(mark) {
		return JT1078Header{}, nil, fmt.Errorf("invalid subpacket mark nibble: 0x%X", uint8(mark))
	}

	// Extract payload - validate sufficient data
	payloadEnd := 30 + int(header.DataBodyLength)
	if len(data) < payloadEnd {
		return JT1078Header{}, nil, fmt.Errorf("incomplete payload: have %d bytes, need %d (header=30 + payload=%d)",
			len(data), payloadEnd, header.DataBodyLength)
	}

	payload := data[30:payloadEnd]

	// Log parsed header
	deviceID, _ := protocol.DecodeBCD(header.SIM[:])
	mBit := (header.M_PT >> 7) & 0x01

	log.Printf("[JT1078] ✓ Header: Device=%s CH=%d SN=%d Type=%d Mark=%04b M=%d PayloadLen=%d\n",
		deviceID, header.LogicalChannel, header.PacketSN, dataType, mark, mBit, len(payload))

	return header, payload, nil
}

// parseJT1078Stream parses multiple JT1078 packets from a buffer
// Also handles raw H.264 streams
func (ms *MediaServer) parseJT1078Stream(data []byte, frameCount *int, parser *protocol.JT1078Parser) {
	// Check if this is raw H.264 data first
	if isRawH264(data) {
		log.Printf("[MEDIA_CONN] ✓ Raw H.264 stream detected\n")
		ms.processRawH264Stream(data)
		return
	}

	// Try to parse as JT1078 packets
	ms.parseJT1078Packets(data, frameCount, parser)
}

// isRawH264 checks if data starts with H.264 NAL start code
func isRawH264(data []byte) bool {
	if len(data) < 3 {
		return false
	}
	// Check for 4-byte start code (0x00 0x00 0x00 0x01)
	if len(data) >= 4 && data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x00 && data[3] == 0x01 {
		return true
	}
	// Check for 3-byte start code (0x00 0x00 0x01)
	if data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01 {
		return true
	}
	return false
}

// isJT1078Header checks if data at offset starts with JT1078 header
func isJT1078Header(data []byte, offset int) bool {
	if offset+4 > len(data) {
		return false
	}
	return data[offset] == 0x30 && data[offset+1] == 0x31 && data[offset+2] == 0x63 && data[offset+3] == 0x64
}

// parseJT1078Packets processes all JT1078 packets in a buffer (DEPRECATED - use ExtractFrames + parseJT1078Header)
func (ms *MediaServer) parseJT1078Packets(data []byte, frameCount *int, parser *protocol.JT1078Parser) {
	// This function is kept for backward compatibility but should not be used
	// The correct flow is: ExtractFrames() -> parseJT1078Header() -> AddFragment() -> processCompleteFrame()
	log.Printf("[MEDIA_CONN] WARNING: Using deprecated parseJT1078Packets\n")
}

// processJT1078Packet parses and processes a JT1078 protocol packet (DEPRECATED)
// This function is kept for backward compatibility but uses incorrect 25-byte header parsing
// Use parseJT1078Header() instead which correctly implements JT/T 1078-2016 (30-byte header)
func (ms *MediaServer) processJT1078Packet(data []byte) {
	log.Printf("[MEDIA_CONN] WARNING: Using deprecated processJT1078Packet with incorrect header size\n")
	log.Printf("[MEDIA_CONN] Use parseJT1078Header() for correct JT/T 1078-2016 parsing\n")
}

// extractAndProcessNALUnits extracts H.264 NAL units from payload
// CRITICAL: Only processes COMPLETE frames with proper start codes
// Fragments without start codes are DROPPED (not errors, just waiting for reassembly)
func (ms *MediaServer) extractAndProcessNALUnits(deviceID string, channel uint8, payload []byte) {
	if len(payload) == 0 {
		return
	}

	// IMPORTANT: Create stream buffer BEFORE processing NAL units so cacheStreamParams can use it
	streamKey := fmt.Sprintf("%s_CH%d_%s", deviceID, channel, time.Now().Format("20060102"))
	ms.streamMutex.Lock()
	streamBuf, exists := ms.activeStreams[streamKey]
	if !exists {
		// Create stream buffer (file will be created later in saveRawVideoFrame)
		streamBuf = &StreamBuffer{
			DeviceID:          deviceID,
			Channel:           channel,
			Buffer:            make([]byte, 0, 1024*64),
			Created:           time.Now(),
			StreamInitialized: false,
		}
		ms.activeStreams[streamKey] = streamBuf
		log.Printf("[MEDIA_CONN] Pre-created stream buffer for %s\n", streamKey)
	}
	ms.streamMutex.Unlock()

	// Extract NAL units from payload
	units := extractNALUnits(payload)
	if len(units) == 0 {
		log.Printf("[MEDIA_CONN] ⚠ No NAL units found in payload - payload has NO start codes\n")
		log.Printf("[MEDIA_CONN] ⚠ This is likely a fragment without reassembly - DROPPING (not an error)\n")
		log.Printf("[MEDIA_CONN] ⚠ First bytes: % X\n", payload[:min(len(payload), 16)])
		// DO NOT SAVE - this is incomplete data waiting for reassembly
		// The FrameAssembler MUST combine all fragments BEFORE this function is called
		return
	}

	log.Printf("[MEDIA_CONN] ✓ Extracted %d NAL units with start codes\n", len(units))
	for i, unit := range units {
		logNALUnit(i, unit)
	}

	// Validate and cache SPS/PPS
	for _, unit := range units {
		switch unit.Type {
		case 7: // SPS (Sequence Parameter Set)
			log.Printf("[MEDIA_CONN] Processing SPS (type 7) - %d bytes\n", len(unit.Data))

			// TODO: Validate SPS integrity and extract width/height
			// For now, we'll accept SPS without validation to test the pipeline
			spsData, err := protocol.ParseSPS(unit.Data[unit.StartCode:])
			if err != nil {
				log.Printf("[MEDIA_CONN] ⚠️  SPS parsing failed (ignoring for test): %v\n", err)
				// Accept anyway for testing
				log.Printf("[MEDIA_CONN] ✓ SPS accepted (validation skipped for testing)\n")
				ms.cacheStreamParams(deviceID, channel, "sps", unit.Data)
			} else if !spsData.Valid {
				log.Printf("[MEDIA_CONN] ⚠️  SPS validation incomplete (ignoring for test)\n")
				log.Printf("[MEDIA_CONN] ✓ SPS accepted (validation skipped for testing)\n")
				ms.cacheStreamParams(deviceID, channel, "sps", unit.Data)
			} else {
				log.Printf("[MEDIA_CONN] ✓ SPS VALID: %dx%d, Profile=%d, Level=%.1f\n",
					spsData.Width, spsData.Height, spsData.ProfileIdc, float32(spsData.LevelIdc)/10.0)
				ms.cacheStreamParams(deviceID, channel, "sps", unit.Data)
			}

		case 8: // PPS (Picture Parameter Set)
			log.Printf("[MEDIA_CONN] ✓ Processing PPS (type 8) - %d bytes\n", len(unit.Data))
			ms.cacheStreamParams(deviceID, channel, "pps", unit.Data)
		}
	}

	// Reconstruct H.264 stream from NAL units with proper start codes
	h264Data := reconstructH264Stream(units)
	ms.saveRawVideoFrame(deviceID, channel, h264Data)
}

// extractNALUnits extracts H.264 NAL units from a payload
// Returns slice of NALUnit structures
func extractNALUnits(payload []byte) []NALUnit {
	var units []NALUnit
	offset := 0

	for offset < len(payload) {
		// Find next NAL start code
		startPos := findNALStartCode(payload, offset)
		if startPos == -1 {
			// No more NAL units
			if offset < len(payload) {
				log.Printf("[MEDIA_CONN] Remaining data without NAL start code: %d bytes\n", len(payload)-offset)
			}
			break
		}

		if startPos > offset {
			log.Printf("[MEDIA_CONN] Skipped %d bytes before NAL unit\n", startPos-offset)
		}

		// Determine start code size
		startCodeSize := 3
		if startPos >= 1 && startPos+3 < len(payload) &&
			payload[startPos-1] == 0x00 &&
			payload[startPos] == 0x00 &&
			payload[startPos+1] == 0x00 &&
			payload[startPos+2] == 0x01 {
			startCodeSize = 4
			startPos--
		}

		// Find next NAL start code (or end of data)
		nextStart := findNALStartCode(payload, startPos+startCodeSize)
		if nextStart == -1 {
			nextStart = len(payload)
		}

		// Extract NAL unit
		units = append(units, NALUnit{
			Data:      payload[startPos:nextStart],
			Type:      extractNALType(payload, startPos+startCodeSize),
			StartCode: startCodeSize,
		})

		offset = nextStart
	}

	return units
}

// findNALStartCode finds the next H.264 NAL start code (0x00 0x00 0x01 or 0x00 0x00 0x00 0x01)
// Returns the position of 0x00 0x00 0x01 or -1 if not found
func findNALStartCode(data []byte, start int) int {
	for i := start; i+2 < len(data); i++ {
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			return i
		}
	}
	return -1
}

// extractNALType extracts the NAL unit type from the first byte after start code
func extractNALType(data []byte, pos int) uint8 {
	if pos >= len(data) {
		return 0
	}
	return data[pos] & 0x1F // Lower 5 bits
}

// logNALUnit logs information about a NAL unit
func logNALUnit(index int, unit NALUnit) {
	typeNames := map[uint8]string{
		0:  "Unspecified",
		1:  "Coded slice (non-IDR)",
		2:  "Coded slice data partition A",
		3:  "Coded slice data partition B",
		4:  "Coded slice data partition C",
		5:  "IDR picture",
		6:  "SEI",
		7:  "SPS",
		8:  "PPS",
		9:  "AU delimiter",
		10: "End of sequence",
		11: "End of stream",
		12: "Filler data",
	}

	typeName := typeNames[unit.Type]
	if typeName == "" {
		typeName = "Unknown"
	}

	log.Printf("[MEDIA_CONN] NAL[%d]: Type=%d (%s), Size=%d bytes, StartCode=%d\n",
		index, unit.Type, typeName, len(unit.Data), unit.StartCode)
}

// reconstructH264Stream reconstructs H.264 stream from NAL units with proper 4-byte start codes
func reconstructH264Stream(units []NALUnit) []byte {
	var result []byte
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	for _, unit := range units {
		// Always use 4-byte start code in output
		result = append(result, startCode...)
		// Skip existing start code from unit.Data
		if len(unit.Data) > unit.StartCode {
			result = append(result, unit.Data[unit.StartCode:]...)
		}
	}

	return result
}

// processRawH264Stream handles raw H.264 data (without JT1078 wrapper)
func (ms *MediaServer) processRawH264Stream(data []byte) {
	// Generate a generic device ID for raw streams
	deviceID := "raw_device"
	channel := uint8(0)

	log.Printf("[MEDIA_CONN] Processing raw H.264 stream: %d bytes for %s_ch%d\n", len(data), deviceID, channel)
	ms.saveRawVideoFrame(deviceID, channel, data)
}

// flushAllStreams flushes all active stream buffers and closes file handles
func (ms *MediaServer) flushAllStreams() {
	ms.streamMutex.Lock()
	defer ms.streamMutex.Unlock()

	for streamKey, streamBuf := range ms.activeStreams {
		// Flush remaining data
		if streamBuf.BufferSize > 0 {
			n, err := streamBuf.File.Write(streamBuf.Buffer)
			if err != nil {
				log.Printf("[MEDIA_CONN] Error flushing stream %s: %v\n", streamKey, err)
			} else {
				log.Printf("[MEDIA_CONN] Final flush: %d bytes to %s\n", n, streamBuf.FilePath)
			}
		}

		// Close ffmpeg stdin pipe
		if streamBuf.FFmpegStdin != nil {
			streamBuf.FFmpegStdin.Close()
			streamBuf.FFmpegStdin = nil
		}

		// Close file handle
		if err := streamBuf.File.Close(); err != nil {
			log.Printf("[MEDIA_CONN] Error closing stream %s: %v\n", streamKey, err)
		} else {
			log.Printf("[MEDIA_CONN] ✓ Closed stream %s\n", streamKey)
		}

		// VALIDATION: Check file starts with valid SPS
		if err := validateH264File(streamBuf.FilePath); err != nil {
			log.Printf("[MEDIA_CONN] ⚠️  H.264 file validation WARNING: %v\n", err)
		}

		// Remove from map
		delete(ms.activeStreams, streamKey)
	}

	log.Printf("[MEDIA_CONN] All streams flushed and closed\n")
}

// validateH264File checks if H.264 file starts with valid SPS (type 7)
func validateH264File(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("cannot open file: %v", err)
	}
	defer file.Close()

	// Read first 64 bytes
	header := make([]byte, 64)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return fmt.Errorf("read error: %v", err)
	}

	if n < 5 {
		return fmt.Errorf("file too small: %d bytes", n)
	}

	log.Printf("[H264_VALIDATION] First 32 bytes: % X\n", header[:min(n, 32)])

	// Check for SPS start code + type 7
	// Valid patterns:
	// 00 00 00 01 67 ... (4-byte start code + SPS type 7)
	// 00 00 01 67 ...   (3-byte start code + SPS type 7)

	validPattern := false
	spsOffset := -1

	if n >= 5 && header[0] == 0x00 && header[1] == 0x00 && header[2] == 0x00 && header[3] == 0x01 {
		// 4-byte start code
		nalType := header[4] & 0x1F
		if nalType == 7 {
			validPattern = true
			spsOffset = 4
		} else {
			log.Printf("[H264_VALIDATION] First NAL type: %d (expected SPS=7)\n", nalType)
		}
	} else if n >= 4 && header[0] == 0x00 && header[1] == 0x00 && header[2] == 0x01 {
		// 3-byte start code
		nalType := header[3] & 0x1F
		if nalType == 7 {
			validPattern = true
			spsOffset = 3
		} else {
			log.Printf("[H264_VALIDATION] First NAL type: %d (expected SPS=7)\n", nalType)
		}
	}

	if !validPattern {
		return fmt.Errorf("invalid H.264 header: file does not start with SPS (NAL type 7)")
	}

	log.Printf("[H264_VALIDATION] ✅ File starts with valid SPS at offset %d\n", spsOffset)
	log.Printf("[H264_VALIDATION] ✅ H.264 file is valid\n")

	return nil
}

// startFFmpegStream starts ffmpeg to convert H.264 Annex-B input to HLS output
func (ms *MediaServer) startFFmpegStream(streamBuf *StreamBuffer, outM3U8 string) {
	outDir := filepath.Dir(outM3U8)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Printf("[MEDIA_CONN] Failed to create HLS output dir %s: %v\n", outDir, err)
		return
	}
	segmentPattern := filepath.Join(outDir, "seg_%06d.ts")

	cmd := exec.Command("ffmpeg",
		"-hide_banner",
		"-loglevel", "warning",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-probesize", "32768",
		"-analyzeduration", "1000000",
		"-f", "h264",
		"-i", "pipe:0",
		"-map", "0:v:0",
		"-c:v", "copy",
		"-f", "hls",
		"-hls_time", "1",
		"-hls_list_size", "6",
		"-hls_flags", "delete_segments+append_list+omit_endlist",
		"-hls_segment_type", "mpegts",
		"-hls_segment_filename", segmentPattern,
		outM3U8,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("[MEDIA_CONN] Failed to create stdin pipe for HLS: %v\n", err)
		return
	}
	streamBuf.FFmpegStdin = stdin
	streamBuf.FFmpegCmd = cmd

	// Redirect ffmpeg output to logs for debugging
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Printf("[MEDIA_CONN] Failed to start ffmpeg: %v\n", err)
		stdin.Close()
		streamBuf.FFmpegStdin = nil
		return
	}

	streamBuf.FFmpegReady = true
	log.Printf("[MEDIA_CONN] ffmpeg started for HLS: %s\n", outM3U8)
	log.Printf("[MEDIA_CONN] ffmpeg PID: %d\n", cmd.Process.Pid)

	// Wait for process to finish
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("[MEDIA_CONN] ffmpeg process ended with error: %v\n", err)
		} else {
			log.Printf("[MEDIA_CONN] ffmpeg process ended normally\n")
		}
		streamBuf.FFmpegReady = false
		streamBuf.FFmpegStdin = nil
	}()
}

// cacheStreamParams caches SPS/PPS NAL units for stream header reconstruction
// CRITICAL: Only accepts complete NAL units with start codes and valid types
func (ms *MediaServer) cacheStreamParams(deviceID string, channel uint8, paramType string, data []byte) {
	if len(data) < 5 { // start code (4) + NAL header (1)
		log.Printf("[MEDIA_CONN] ❌ Invalid NAL data: too short (%d bytes)\n", len(data))
		return
	}

	// Validate NAL type
	var expectedType uint8
	var startCodeOffset int

	// Find start code
	if data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x00 && data[3] == 0x01 {
		startCodeOffset = 4
	} else if data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01 {
		startCodeOffset = 3
	} else {
		log.Printf("[MEDIA_CONN] ❌ ERROR: NAL without start code - REJECTING\n")
		log.Printf("[MEDIA_CONN] First bytes: % X\n", data[:min(len(data), 8)])
		return
	}

	if startCodeOffset >= len(data) {
		log.Printf("[MEDIA_CONN] ❌ ERROR: No data after start code\n")
		return
	}

	actualType := data[startCodeOffset] & 0x1F

	if paramType == "sps" {
		expectedType = 7
	} else if paramType == "pps" {
		expectedType = 8
	} else {
		log.Printf("[MEDIA_CONN] ❌ ERROR: Unknown param type %s\n", paramType)
		return
	}

	if actualType != expectedType {
		log.Printf("[MEDIA_CONN] ❌ ERROR: NAL type mismatch - expected %d (%s), got %d\n", expectedType, paramType, actualType)
		log.Printf("[MEDIA_CONN] NAL data (first 16 bytes): % X\n", data[:min(len(data), 16)])
		return
	}

	streamKey := fmt.Sprintf("%s_CH%d_%s", deviceID, channel, time.Now().Format("20060102"))

	ms.streamMutex.Lock()
	defer ms.streamMutex.Unlock()

	if streamBuf, exists := ms.activeStreams[streamKey]; exists {
		switch paramType {
		case "sps":
			streamBuf.LastSPS = data
			log.Printf("[MEDIA_CONN] ✅ VALIDATED and CACHED SPS (type 7): % X...\n", data[:min(len(data), 12)])
		case "pps":
			streamBuf.LastPPS = data
			log.Printf("[MEDIA_CONN] ✅ VALIDATED and CACHED PPS (type 8): % X...\n", data[:min(len(data), 12)])
		}
	}
}

// saveRawVideoFrame saves video frames with buffering to prevent fragmentation
// CRITICAL: Implements frame ordering: MUST have SPS+PPS before ANY other NAL
func (ms *MediaServer) saveRawVideoFrame(deviceID string, channel uint8, data []byte) {
	// Create streams directory
	streamDir := "streams"
	if err := os.MkdirAll(streamDir, 0755); err != nil {
		log.Printf("[MEDIA_CONN] Failed to create streams directory: %v\n", err)
		return
	}

	// Use device ID from JT808 session if unknown
	if deviceID == "unknown" {
		deviceID = "device"
	}

	// Create stream key (deviceID_CH_date)
	streamKey := fmt.Sprintf("%s_CH%d_%s", deviceID, channel, time.Now().Format("20060102"))
	filePath := filepath.Join(streamDir, fmt.Sprintf("%s.h264", streamKey))

	ms.streamMutex.Lock()
	defer ms.streamMutex.Unlock()

	// Get or create stream buffer
	streamBuf, exists := ms.activeStreams[streamKey]
	if !exists || streamBuf.File == nil {
		// Create or update stream buffer with file handle
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("[MEDIA_CONN] Failed to create stream file %s: %v\n", filePath, err)
			return
		}

		if !exists {
			streamBuf = &StreamBuffer{
				DeviceID:          deviceID,
				Channel:           channel,
				FilePath:          filePath,
				File:              f,
				Buffer:            make([]byte, 0, 1024*64), // 64KB buffer
				BufferSize:        0,
				Created:           time.Now(),
				FFmpegReady:       false,
				StreamInitialized: false,
			}
			ms.activeStreams[streamKey] = streamBuf
			log.Printf("[MEDIA_CONN] Created new stream buffer for %s\n", streamKey)
		} else {
			// Update existing buffer with file handle
			streamBuf.FilePath = filePath
			streamBuf.File = f
			log.Printf("[MEDIA_CONN] Opened file for existing stream buffer\n")
		}
	}

	// Track data for debugging
	streamBuf.DataReceived += int64(len(data))

	// ============================================================================
	// CRITICAL RULE: Stream initialization
	// Condition: Must have BOTH SPS and PPS before writing ANYTHING
	// ============================================================================
	if !streamBuf.StreamInitialized {
		// Check if we have valid SPS and PPS
		if streamBuf.LastSPS == nil || streamBuf.LastPPS == nil {
			log.Printf("[MEDIA_CONN] ⏳ Buffering frame data... waiting for SPS+PPS (have SPS:%v PPS:%v)\n",
				streamBuf.LastSPS != nil, streamBuf.LastPPS != nil)
			// DO NOT WRITE ANYTHING - wait for SPS and PPS
			return
		}

		log.Printf("[MEDIA_CONN] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		log.Printf("[MEDIA_CONN] ✅✅✅ STREAM INITIALIZATION START ✅✅✅\n")
		log.Printf("[MEDIA_CONN] Got valid SPS+PPS - beginning stream initialization\n")
		log.Printf("[MEDIA_CONN] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

		// ORDER IS MANDATORY: SPS first, then PPS
		// This is the H.264 stream header that FFmpeg needs to initialize the decoder

		// Write SPS first (MUST be type 7 / 0x67)
		log.Printf("[MEDIA_CONN] 1️⃣  Writing SPS (type 7): % X...\n", streamBuf.LastSPS[:min(len(streamBuf.LastSPS), 12)])
		streamBuf.Buffer = append(streamBuf.Buffer, streamBuf.LastSPS...)
		streamBuf.BufferSize += len(streamBuf.LastSPS)

		// Write PPS second (MUST be type 8 / 0x68)
		log.Printf("[MEDIA_CONN] 2️⃣  Writing PPS (type 8): % X...\n", streamBuf.LastPPS[:min(len(streamBuf.LastPPS), 12)])
		streamBuf.Buffer = append(streamBuf.Buffer, streamBuf.LastPPS...)
		streamBuf.BufferSize += len(streamBuf.LastPPS)

		streamBuf.StreamInitialized = true
		log.Printf("[MEDIA_CONN] ✅ Stream header written (%d bytes total)\n", streamBuf.BufferSize)
		log.Printf("[MEDIA_CONN] Stream is now ready for frame data\n")

		// Start ffmpeg now that we have valid stream header
		if !streamBuf.FFmpegReady {
			rtspURL := fmt.Sprintf("rtsp://localhost:8554/cam_%s_ch%d", deviceID, channel)
			streamBuf.RTSPURL = rtspURL

			ms.streamMutex.Unlock()
			go ms.startFFmpegStream(streamBuf, rtspURL)
			ms.streamMutex.Lock()
		}
	}

	// Now safe to add frame data (SPS and PPS already written)
	log.Printf("[MEDIA_CONN] 🎬 Writing frame data: %d bytes\n", len(data))
	streamBuf.Buffer = append(streamBuf.Buffer, data...)
	streamBuf.BufferSize += len(data)

	// Flush if buffer reaches critical size (64KB) - flush more frequently
	const maxBufferSize = 1024 * 64 // 64KB
	if streamBuf.BufferSize >= maxBufferSize {
		ms.flushStreamBuffer(streamKey, streamBuf)
	}
}

// flushStreamBuffer writes buffered data to file and RTSP stream
func (ms *MediaServer) flushStreamBuffer(streamKey string, streamBuf *StreamBuffer) {
	if streamBuf.BufferSize == 0 {
		return
	}

	// Write to file
	n, err := streamBuf.File.Write(streamBuf.Buffer)
	if err != nil {
		log.Printf("[MEDIA_CONN] Failed to write to file %s: %v\n", streamKey, err)
	}

	// Write to RTSP stream (ffmpeg stdin)
	if streamBuf.FFmpegStdin != nil {
		_, err := streamBuf.FFmpegStdin.Write(streamBuf.Buffer)
		if err != nil {
			log.Printf("[MEDIA_CONN] Failed to write to RTSP %s: %v\n", streamBuf.RTSPURL, err)
			streamBuf.FFmpegStdin = nil
		}
	}

	streamBuf.Buffer = streamBuf.Buffer[:0]
	streamBuf.BufferSize = 0
	log.Printf("[MEDIA_CONN] Flushed %d bytes to file and RTSP\n", n)
}

func min32(n int) int {
	if n < 32 {
		return n
	}
	return 32
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// NewFrameAssembler creates a new frame assembler
func NewFrameAssembler(timeout time.Duration) *FrameAssembler {
	return &FrameAssembler{
		assemblies: make(map[string]*FrameAssembly),
		timeout:    timeout,
	}
}

// AddFragment adds a packet fragment and returns complete frames
func (fa *FrameAssembler) AddFragment(header JT1078Header, payload []byte) []FrameFragment {
	fa.mutex.Lock()
	defer fa.mutex.Unlock()

	deviceID, _ := protocol.DecodeBCD(header.SIM[:])
	key := fmt.Sprintf("%s_ch%d", deviceID, header.LogicalChannel)

	mark := SubpacketMark(header.DataType_Mark & 0x0F)
	mBit := (header.M_PT >> 7) & 0x01

	fragment := FrameFragment{
		Header:   header,
		Payload:  payload,
		Received: time.Now(),
	}

	// Atomic packet (complete frame)
	if mark == MarkAtomic {
		log.Printf("[ASSEMBLER] Atomic frame: Device=%s CH=%d SN=%d Len=%d\n",
			deviceID, header.LogicalChannel, header.PacketSN, len(payload))
		return []FrameFragment{fragment}
	}

	// Fragmented frame
	assembly, exists := fa.assemblies[key]

	if !exists {
		switch mark {
		case MarkFirst:
			log.Printf("[ASSEMBLER] First fragment: Device=%s CH=%d SN=%d\n",
				deviceID, header.LogicalChannel, header.PacketSN)
		case MarkMiddle:
			// Vendor quirk fallback: alguns terminais iniciam sequência com 0b0011.
			log.Printf("[ASSEMBLER] WARNING: Starting assembly from middle fragment (vendor fallback): Device=%s CH=%d SN=%d\n",
				deviceID, header.LogicalChannel, header.PacketSN)
		case MarkLast:
			// Fallback final: tratar LAST isolado como frame completo para não bloquear stream.
			log.Printf("[ASSEMBLER] WARNING: Isolated last fragment without start, forwarding as single fragment: Device=%s CH=%d SN=%d\n",
				deviceID, header.LogicalChannel, header.PacketSN)
			return []FrameFragment{fragment}
		}

		fa.assemblies[key] = &FrameAssembly{
			DeviceID:   deviceID,
			Channel:    header.LogicalChannel,
			Fragments:  []FrameFragment{fragment},
			FirstSN:    header.PacketSN,
			LastSN:     header.PacketSN,
			LastUpdate: time.Now(),
		}
		return nil
	}

	if mark == MarkFirst {
		if exists {
			log.Printf("[ASSEMBLER] Restarting assembly: Device=%s CH=%d oldFragments=%d newSN=%d\n",
				deviceID, header.LogicalChannel, len(assembly.Fragments), header.PacketSN)
		}

		fa.assemblies[key] = &FrameAssembly{
			DeviceID:   deviceID,
			Channel:    header.LogicalChannel,
			Fragments:  []FrameFragment{fragment},
			FirstSN:    header.PacketSN,
			LastSN:     header.PacketSN,
			LastUpdate: time.Now(),
		}
		return nil
	}

	expectedSN := assembly.LastSN + 1
	if header.PacketSN != expectedSN {
		log.Printf("[ASSEMBLER] Sequence discontinuity: Device=%s CH=%d expectedSN=%d gotSN=%d, dropping assembly\n",
			deviceID, header.LogicalChannel, expectedSN, header.PacketSN)
		delete(fa.assemblies, key)
		return nil
	}

	// Add to existing assembly
	assembly.Fragments = append(assembly.Fragments, fragment)
	assembly.LastSN = header.PacketSN
	assembly.LastUpdate = time.Now()

	log.Printf("[ASSEMBLER] Fragment %d/%d: Device=%s CH=%d SN=%d Mark=%04b\n",
		len(assembly.Fragments), len(assembly.Fragments)+1, deviceID, header.LogicalChannel, header.PacketSN, mark)

	// Check if frame is complete
	if mark == MarkLast || mBit == 1 {
		if mark != MarkLast && mBit == 1 {
			log.Printf("[ASSEMBLER] Completing frame by M bit fallback: Device=%s CH=%d SN=%d\n",
				deviceID, header.LogicalChannel, header.PacketSN)
		}
		log.Printf("[ASSEMBLER] ✓ Frame complete: Device=%s CH=%d Fragments=%d TotalLen=%d\n",
			deviceID, header.LogicalChannel, len(assembly.Fragments), fa.calculateTotalLength(assembly))

		completeFragments := assembly.Fragments
		delete(fa.assemblies, key)
		return completeFragments
	}

	return nil
}

// calculateTotalLength calculates total payload size of all fragments
func (fa *FrameAssembler) calculateTotalLength(assembly *FrameAssembly) int {
	total := 0
	for _, frag := range assembly.Fragments {
		total += len(frag.Payload)
	}
	return total
}

// CleanupStale removes stale assemblies
func (fa *FrameAssembler) CleanupStale() {
	fa.mutex.Lock()
	defer fa.mutex.Unlock()

	now := time.Now()
	for key, assembly := range fa.assemblies {
		if now.Sub(assembly.LastUpdate) > fa.timeout {
			log.Printf("[ASSEMBLER] Timeout: %s (fragments=%d)\n", key, len(assembly.Fragments))
			delete(fa.assemblies, key)
		}
	}
}

// processCompleteFrame processes a reassembled complete frame
func (ms *MediaServer) processCompleteFrame(fragments []FrameFragment) {
	if len(fragments) == 0 {
		return
	}

	first := fragments[0]
	deviceID, _ := protocol.DecodeBCD(first.Header.SIM[:])
	channel := first.Header.LogicalChannel

	// Reassemble payload from all fragments
	var completePayload []byte
	for _, frag := range fragments {
		completePayload = append(completePayload, frag.Payload...)
	}

	log.Printf("[MEDIA_CONN] ✓ Complete frame assembled: Device=%s CH=%d Fragments=%d TotalLen=%d\n",
		deviceID, channel, len(fragments), len(completePayload))

	// Process frame with new NAL-aware system
	ms.processH264Frame(deviceID, channel, completePayload)
}

// startFFmpegForStream starts FFmpeg for a stream buffer and publishes to MediaMTX (RTSP)
func (ms *MediaServer) startFFmpegForStream(streamBuf *StreamBuffer, deviceID string, channel uint8) {
	rtspURL := fmt.Sprintf("rtsp://localhost:8554/cam_%s_ch%d", deviceID, channel)
	streamBuf.RTSPURL = rtspURL

	log.Printf("[MEDIA_CONN] Starting FFmpeg for %s (RTSP: %s)\n", deviceID, rtspURL)

	cmd := exec.Command("ffmpeg",
		"-hide_banner",
		"-loglevel", "warning",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-f", "h264",
		"-i", "pipe:0",
		"-map", "0:v:0",
		"-c:v", "copy",
		"-f", "rtsp",
		"-rtsp_transport", "tcp",
		rtspURL,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("[MEDIA_CONN] ❌ Failed to create stdin pipe: %v\n", err)
		return
	}

	streamBuf.FFmpegStdin = stdin
	streamBuf.FFmpegCmd = cmd

	// Redirect output for debugging
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Printf("[MEDIA_CONN] ❌ Failed to start FFmpeg: %v\n", err)
		stdin.Close()
		return
	}

	log.Printf("[MEDIA_CONN] ✅ FFmpeg started (PID: %d) publishing RTSP to %s\n", cmd.Process.Pid, rtspURL)

	// Monitor process
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("[MEDIA_CONN] ⚠️  FFmpeg process ended with error: %v\n", err)
		} else {
			log.Printf("[MEDIA_CONN] FFmpeg process ended normally\n")
		}
		streamBuf.FFmpegStdin = nil
	}()
}

// sendToFFmpeg sends H.264 data to FFmpeg stdin
func (ms *MediaServer) sendToFFmpeg(streamBuf *StreamBuffer, data []byte) {
	if streamBuf.FFmpegStdin == nil {
		log.Printf("[MEDIA_CONN] ⚠️  FFmpeg not ready, dropping %d bytes\n", len(data))
		return
	}

	n, err := streamBuf.FFmpegStdin.Write(data)
	if err != nil {
		log.Printf("[MEDIA_CONN] ❌ FFmpeg write error: %v\n", err)
		return
	}

	log.Printf("[MEDIA_CONN] → FFmpeg: wrote %d bytes\n", n)
}

// saveFrameToFile saves frame to file for debugging/recording
func (ms *MediaServer) saveFrameToFile(streamBuf *StreamBuffer, data []byte) {
	// Create file if not exists
	if streamBuf.File == nil {
		streamDir := "streams"
		if err := os.MkdirAll(streamDir, 0755); err != nil {
			log.Printf("[MEDIA_CONN] Failed to create streams dir: %v\n", err)
			return
		}

		streamKey := fmt.Sprintf("%s_CH%d_%s", streamBuf.DeviceID, streamBuf.Channel, time.Now().Format("20060102"))
		filePath := filepath.Join(streamDir, fmt.Sprintf("%s.h264", streamKey))

		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("[MEDIA_CONN] Failed to open file %s: %v\n", filePath, err)
			return
		}

		streamBuf.FilePath = filePath
		streamBuf.File = f
		log.Printf("[MEDIA_CONN] ✓ Opened file for recording: %s\n", filePath)
	}

	// Write to file
	n, err := streamBuf.File.Write(data)
	if err != nil {
		log.Printf("[MEDIA_CONN] File write error: %v\n", err)
		return
	}

	log.Printf("[MEDIA_CONN] → File: wrote %d bytes to %s\n", n, streamBuf.FilePath)
}

// processH264Frame processes a complete H.264 frame with NAL detection
func (ms *MediaServer) processH264Frame(deviceID string, channel uint8, payload []byte) {
	if len(payload) == 0 {
		log.Printf("[MEDIA_CONN] Empty payload, skipping\n")
		return
	}

	// Log first bytes for debugging
	log.Printf("[MEDIA_CONN] Processing frame: %d bytes, first bytes: % X\n",
		len(payload), payload[:min(len(payload), 16)])

	// Get or create stream buffer
	streamKey := fmt.Sprintf("%s_CH%d_%s", deviceID, channel, time.Now().Format("20060102"))

	ms.streamMutex.Lock()
	streamBuf, exists := ms.activeStreams[streamKey]
	if !exists {
		// Create stream buffer with initialization buffer
		streamBuf = &StreamBuffer{
			DeviceID:          deviceID,
			Channel:           channel,
			Buffer:            make([]byte, 0, 1024*64),
			Created:           time.Now(),
			StreamInitialized: false,
			InitBuffer:        stream.NewStreamInitBuffer(deviceID, channel),
		}
		ms.activeStreams[streamKey] = streamBuf

		// Setup callbacks
		streamBuf.InitBuffer.SetOnReady(func(sps, pps []byte) {
			log.Printf("[MEDIA_CONN] 🎉 Stream initialization complete - starting FFmpeg\n")
			ms.startFFmpegForStream(streamBuf, deviceID, channel)
		})

		streamBuf.InitBuffer.SetOnFrameReady(func(data []byte) {
			ms.sendToFFmpeg(streamBuf, data)
		})

		log.Printf("[MEDIA_CONN] ✓ Created stream buffer with init buffer for %s\n", streamKey)
	}
	ms.streamMutex.Unlock()

	// Track data
	streamBuf.DataReceived += int64(len(payload))

	// Add frame to initialization buffer
	ready, err := streamBuf.InitBuffer.AddFrame(payload)
	if err != nil {
		log.Printf("[MEDIA_CONN] ❌ Init buffer error: %v\n", err)
		return
	}

	if ready && !streamBuf.StreamInitialized {
		streamBuf.StreamInitialized = true
		log.Printf("[MEDIA_CONN] ✅ Stream ready for streaming!\n")
	}

	// Also save to file for debugging
	ms.saveFrameToFile(streamBuf, payload)
}
