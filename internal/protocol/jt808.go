package protocol

import (
	"encoding/binary"
	"log"
)

// JT808Parser implements the JT808 protocol parser
// Maintains compatibility with existing code while using new packet structure
type JT808Parser struct {
	*BaseParser
}

// JT808Message represents a parsed JT808 message (legacy compatibility)
// This structure is kept for backward compatibility with existing code
type JT808Message struct {
	MessageID  uint16
	Properties uint16
	DeviceID   string
	SeqNum     uint16
	Body       []byte
}

// NewJT808 creates a new JT808 parser instance
func NewJT808() *JT808Parser {
	return &JT808Parser{
		BaseParser: &BaseParser{
			buffer:   make([]byte, 0, 4096),
			deviceID: "",
		},
	}
}

// Push processes incoming data and returns JT808Messages
// This method bridges the new packet structure with legacy code
func (p *JT808Parser) Push(data []byte) []*JT808Message {
	log.Printf("[JT808] Received %d bytes, buffer size: %d\n", len(data), len(p.buffer))

	frames, err := p.BaseParser.Push(data)
	if err != nil {
		log.Printf("[JT808] Error during parsing: %v\n", err)
	}

	// Convert PacketFrames to JT808Messages for backward compatibility
	messages := make([]*JT808Message, 0, len(frames))
	for _, frame := range frames {
		if frame != nil && frame.Header != nil {
			msg := &JT808Message{
				MessageID:  frame.Header.MsgID,
				Properties: frame.Header.Properties,
				DeviceID:   frame.Header.DeviceID,
				SeqNum:     frame.Header.SequenceNum,
				Body:       frame.Body,
			}
			messages = append(messages, msg)
		}
	}

	log.Printf("[JT808] Returning %d messages, remaining buffer size: %d\n", len(messages), len(p.buffer))
	return messages
}

// ============================================================================
// Response Building Functions
// ============================================================================

// BuildResponse creates a response message for JT808
func BuildResponse(messageID uint16, deviceID string, seqNum uint16, body []byte) ([]byte, error) {
	log.Printf("[JT808] Building response - ID: 0x%04X (%s), DeviceID: %s, SeqNum: %d\n",
		messageID, GetMessageTypeName(messageID), deviceID, seqNum)

	frame := &PacketFrame{
		Flag: FrameDelimiter,
		Header: &PacketHeader{
			MsgID:       messageID,
			Properties:  uint16(len(body) & 0x03FF),
			DeviceID:    deviceID,
			SequenceNum: seqNum,
		},
		Body:     body,
		Checksum: 0, // Will be calculated during encoding
	}

	parser := &BaseParser{}
	encoded, err := parser.Encode(frame)
	if err != nil {
		log.Printf("[JT808] Error building response: %v\n", err)
		return nil, err
	}

	log.Printf("[JT808] Response built: %d bytes\n", len(encoded))
	return encoded, nil
}

// BuildGeneralResponse builds a general platform response (0x8001)
func BuildGeneralResponse(deviceID string, seqNum uint16, replyMsgID uint16, result byte) ([]byte, error) {
	body := make([]byte, 5)
	binary.BigEndian.PutUint16(body[0:2], seqNum)
	binary.BigEndian.PutUint16(body[2:4], replyMsgID)
	body[4] = result // 0=success, 1=failure, 2=message error, 3=not supported

	log.Printf("[JT808] Building general response - ReplyMsgID: 0x%04X (%s), Result: %d\n",
		replyMsgID, GetMessageTypeName(replyMsgID), result)
	return BuildResponse(0x8001, deviceID, seqNum, body)
}

// BuildCameraCommandImmediate builds a command to take photo immediately (0x8801)
func BuildCameraCommandImmediate(deviceID string, seqNum uint16, channelID byte, shotCount uint16, shotInterval uint16, saveFlag byte, resolution byte, quality byte) ([]byte, error) {
	// 0x8801: Camera Immediate Command
	// Body: [ChannelID(1)] [CommandWord(2)] [ShotInterval(2)] [SaveFlag(1)] [Resolution(1)] [Quality(1)] [Brightness(1)] [Contrast(1)] [Saturation(1)] [Chroma(1)]

	body := make([]byte, 12)
	body[0] = channelID
	binary.BigEndian.PutUint16(body[1:3], shotCount)    // 0=stop, 0xFFFF=video, 1-N=photo count
	binary.BigEndian.PutUint16(body[3:5], shotInterval) // Interval in seconds
	body[5] = saveFlag                                  // 1=save, 0=upload immediately
	body[6] = resolution                                // Resolution code
	body[7] = quality                                   // 1-10 (1=best)
	body[8] = 128                                       // Brightness (default 128)
	body[9] = 128                                       // Contrast (default 128)
	body[10] = 128                                      // Saturation (default 128)
	body[11] = 128                                      // Chroma (default 128)

	log.Printf("[JT808] Building camera command - Channel: %d, Shots: %d, Interval: %d, Resolution: %d, Quality: %d\n",
		channelID, shotCount, shotInterval, resolution, quality)

	return BuildResponse(0x8801, deviceID, seqNum, body)
}

// BuildStoredMediaSearch builds a command to search stored multimedia (0x8802)
func BuildStoredMediaSearch(deviceID string, seqNum uint16, mediaType byte, channelID byte, eventCode byte, startTime []byte, endTime []byte) ([]byte, error) {
	// 0x8802: Stored Media Search
	// Body: [MediaType(1)] [ChannelID(1)] [EventCode(1)] [StartTime(6)] [EndTime(6)]

	body := make([]byte, 15)
	body[0] = mediaType // 0=image, 1=audio, 2=video
	body[1] = channelID
	body[2] = eventCode        // 0=all, 1=platform, 2=timer, etc.
	copy(body[3:9], startTime) // BCD format: YYMMDDhhmmss
	copy(body[9:15], endTime)

	log.Printf("[JT808] Building stored media search - Type: %s, Channel: %d, Event: %s\n",
		GetMediaTypeName(mediaType), channelID, GetEventCodeName(eventCode))

	return BuildResponse(0x8802, deviceID, seqNum, body)
}

// BuildMediaDataUploadRequest builds a command to request media upload (0x8803)
func BuildMediaDataUploadRequest(deviceID string, seqNum uint16, multimediaID uint32, deleteFlag byte) ([]byte, error) {
	// 0x8803: Media Data Upload Request
	// Body: [MultimediaID(4)] [DeleteFlag(1)]

	body := make([]byte, 5)
	binary.BigEndian.PutUint32(body[0:4], multimediaID)
	body[4] = deleteFlag // 0=keep, 1=delete after upload

	log.Printf("[JT808] Building media upload request - MultimediaID: %d, Delete: %v\n",
		multimediaID, deleteFlag == 1)

	return BuildResponse(0x8803, deviceID, seqNum, body)
}

// BuildRealTimeVideoRequest builds a real-time audio/video transmission request (0x9101)
// According to JT/T 1078-2016 Table 17, the correct order is:
// [IPLen(1)] [IP(n)] [TCPPort(2)] [UDPPort(2)] [ChannelID(1)] [DataType(1)] [StreamType(1)]
func BuildRealTimeVideoRequest(deviceID string, seqNum uint16, serverIP string, tcpPort, udpPort uint16, channelID, dataType, streamType byte) ([]byte, error) {
	ipBytes := []byte(serverIP)
	if len(ipBytes) > 255 {
		ipBytes = ipBytes[:255]
	}

	body := make([]byte, 0, 8+len(ipBytes))

	// 1. IP Address Length (1 byte)
	body = append(body, byte(len(ipBytes)))

	// 2. IP Address (n bytes)
	body = append(body, ipBytes...)

	// 3. TCP Port (2 bytes, big-endian)
	portBuf := make([]byte, 4)
	binary.BigEndian.PutUint16(portBuf[0:2], tcpPort)
	binary.BigEndian.PutUint16(portBuf[2:4], udpPort)
	body = append(body, portBuf...)

	// 4. Logical Channel Number (1 byte)
	body = append(body, channelID)

	// 5. Data Type (1 byte) - 0: audio/video, 1: video, 2: two-way, 3: listen, 4: broadcast, 5: transparent
	body = append(body, dataType)

	// 6. Stream Type (1 byte) - 0: main stream, 1: sub-stream
	body = append(body, streamType)

	log.Printf("[JT808] Building 0x9101 (JT1078 Table 17) - IP: %s, TCP: %d, UDP: %d, Ch: %d, DataType: %d, Stream: %d\n",
		serverIP, tcpPort, udpPort, channelID, dataType, streamType)

	return BuildResponse(0x9101, deviceID, seqNum, body)
}
