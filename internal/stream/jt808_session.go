package stream

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"jt808-broker/internal/protocol"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// JT808Session handles JT808 protocol communication
type JT808Session struct {
	Conn            net.Conn
	DeviceID        string
	Parser          *protocol.JT808Parser
	VideoHandler    *VideoFrameHandler
	AudioHandler    *AudioFrameHandler
	MultimediaStore *MultimediaStore
	Registry        *DeviceRegistry
	messageCount    map[uint16]int // Track message types received
	outSeq          uint16         // Sequence for platform-initiated commands
	registered      bool           // Registration complete (0x0100 → 0x8100)
	authenticated   bool           // Authentication complete (0x0102 ACK)
}

// IsRegistered returns true if device has completed registration (0x0100)
func (s *JT808Session) IsRegistered() bool {
	return s.registered
}

// IsAuthenticated returns true if device has completed authentication (0x0102)
func (s *JT808Session) IsAuthenticated() bool {
	return s.authenticated
}

func (s *JT808Session) Run() {
	log.Printf("[JT808_SESSION] Starting JT808 session from %s\n", s.Conn.RemoteAddr())
	s.messageCount = make(map[uint16]int)

	// Set TCP_NODELAY to ensure immediate packet transmission
	if tcpConn, ok := s.Conn.(*net.TCPConn); ok {
		if err := tcpConn.SetNoDelay(true); err != nil {
			log.Printf("[JT808_SESSION] Error setting TCP_NODELAY: %v\n", err)
		}
		// Set read timeout to 90 seconds (devices should send heartbeat every 30 seconds)
		if err := tcpConn.SetReadDeadline(time.Now().Add(90 * time.Second)); err != nil {
			log.Printf("[JT808_SESSION] Error setting read deadline: %v\n", err)
		} else {
			log.Printf("[JT808_SESSION] Read timeout set to 90 seconds\n")
		}
	}

	reader := bufio.NewReader(s.Conn)

	for {
		buf := make([]byte, 4096)
		n, err := reader.Read(buf)
		if err != nil {
			log.Printf("[JT808_SESSION] Connection closed: %v\n", err)
			return
		}

		log.Printf("[JT808_SESSION] Received %d bytes from %s\n", n, s.Conn.RemoteAddr())
		log.Printf("[JT808_SESSION] First 32 bytes (hex): % X\n", buf[:min(n, 32)])
		log.Printf("[JT808_SESSION] First 32 bytes (ascii): %q\n", string(buf[:min(n, 32)]))

		// Reset read deadline after receiving data
		if tcpConn, ok := s.Conn.(*net.TCPConn); ok {
			if err := tcpConn.SetReadDeadline(time.Now().Add(90 * time.Second)); err != nil {
				log.Printf("[JT808_SESSION] Error resetting read deadline: %v\n", err)
			} else {
				log.Printf("[JT808_SESSION] Read deadline reset to 90 seconds\n")
			}
		}

		messages := s.Parser.Push(buf[:n])
		log.Printf("[JT808_SESSION] Parsed %d messages\n", len(messages))

		for _, msg := range messages {
			s.messageCount[msg.MessageID]++
			s.handleMessage(msg)
		}

		// Update session device ID if identified
		if s.DeviceID == "" && s.Parser.GetDeviceID() != "" {
			s.DeviceID = s.Parser.GetDeviceID()
			log.Printf("[JT808_SESSION] Device identified: %s\n", s.DeviceID)
			if s.Registry != nil {
				s.Registry.Register(s.DeviceID, s)
			}
		}
	}

	// Print session summary on disconnect
	log.Printf("[JT808_SESSION] Message summary:\n")
	for msgID, count := range s.messageCount {
		log.Printf("[JT808_SESSION]   0x%04X (%s): %d messages\n", msgID, protocol.GetMessageTypeName(msgID), count)
	}
	if len(s.messageCount) == 1 {
		if _, ok := s.messageCount[0x0002]; ok {
			log.Printf("[JT808_SESSION] WARNING: Only heartbeats received. Device may not be sending data or video.\n")
		}
	}

	// Unregister device on disconnect
	if s.DeviceID != "" && s.Registry != nil {
		s.Registry.Unregister(s.DeviceID)
	}
}

func (s *JT808Session) handleMessage(msg *protocol.JT808Message) {
	log.Printf("[JT808_SESSION] Handling message 0x%04X (%s) from device %s\n",
		msg.MessageID, protocol.GetMessageTypeName(msg.MessageID), msg.DeviceID)

	var response []byte
	var followUp []byte

	switch msg.MessageID {
	case 0x0001: // Terminal General Response
		log.Printf("[JT808_SESSION] Terminal general response from device %s\n", msg.DeviceID)
		if len(msg.Body) >= 5 {
			respSeq := binary.BigEndian.Uint16(msg.Body[0:2])
			replyMsgID := binary.BigEndian.Uint16(msg.Body[2:4])
			result := msg.Body[4]

			// Decode result
			resultMsg := "unknown"
			switch result {
			case 0:
				resultMsg = "✓ Success"
			case 1:
				resultMsg = "✗ Failed"
			case 2:
				resultMsg = "✗ Message Invalid"
			case 3:
				resultMsg = "✗ NOT SUPPORTED (device doesn't support this command)"
			case 4:
				resultMsg = "✗ Alarm not confirmed"
			}

			log.Printf("[JT808_SESSION]   ReplySeq=%d, ReplyMsgID=0x%04X (%s), Result=%d (%s)\n",
				respSeq, replyMsgID, protocol.GetMessageTypeName(replyMsgID), result, resultMsg)

			if result == 3 && replyMsgID == 0x9101 {
				log.Printf("[JT808_SESSION] ⚠️  CRITICAL: Device does NOT support 0x9101 (live video request)\n")
				log.Printf("[JT808_SESSION]     Possible reasons:\n")
				log.Printf("[JT808_SESSION]     1. Device firmware doesn't have JT1078 support\n")
				log.Printf("[JT808_SESSION]     2. Command sent before authentication completed\n")
				log.Printf("[JT808_SESSION]     3. Device may only support 0x9102 or other video commands\n")
				log.Printf("[JT808_SESSION]     4. Device requires specific configuration/activation\n")
			}
		} else {
			log.Printf("[JT808_SESSION]   Invalid 0x0001 body length: %d\n", len(msg.Body))
		}
		response = nil

	case 0x0002: // Terminal Logout/Unregister
		log.Printf("[JT808_SESSION] Logout request from device %s\n", msg.DeviceID)
		// Clear registration flags
		s.registered = false
		s.authenticated = false
		log.Printf("[JT808_SESSION] ✗ Device %s logged out - flags cleared\n", msg.DeviceID)
		var err error
		response, err = protocol.BuildGeneralResponse(msg.DeviceID, msg.SeqNum, msg.MessageID, 0)
		if err != nil {
			log.Printf("[JT808_SESSION] Error building response: %v\n", err)
		}

	case 0x0100: // Terminal Registration
		log.Printf("[JT808_SESSION] Registration request from device %s\n", msg.DeviceID)
		response = s.handleRegistration(msg)
		s.registered = true
		log.Printf("[JT808_SESSION] ✓ Device %s is now REGISTERED\n", msg.DeviceID)

	case 0x0102: // Terminal Authentication
		log.Printf("[JT808_SESSION] Authentication from device %s\n", msg.DeviceID)
		var err error
		response, err = protocol.BuildGeneralResponse(msg.DeviceID, msg.SeqNum, msg.MessageID, 0)
		if err != nil {
			log.Printf("[JT808_SESSION] Error building authentication response: %v\n", err)
		}
		s.authenticated = true
		log.Printf("[JT808_SESSION] ✓ Device %s is now AUTHENTICATED\n", msg.DeviceID)

	case 0x0200: // Location Report
		log.Printf("[JT808_SESSION] Location report from device %s\n", msg.DeviceID)
		s.handleLocationReport(msg)
		var err error
		response, err = protocol.BuildGeneralResponse(msg.DeviceID, msg.SeqNum, msg.MessageID, 0)
		if err != nil {
			log.Printf("[JT808_SESSION] Error building location response: %v\n", err)
		}

	case 0x0704: // Batch Location Report
		log.Printf("[JT808_SESSION] Batch location report from device %s\n", msg.DeviceID)
		var err error
		response, err = protocol.BuildGeneralResponse(msg.DeviceID, msg.SeqNum, msg.MessageID, 0)
		if err != nil {
			log.Printf("[JT808_SESSION] Error building batch location response: %v\n", err)
		}

	case 0x0800: // Multimedia Event Upload
		log.Printf("[JT808_SESSION] Multimedia event from device %s\n", msg.DeviceID)
		var err error
		response, err = s.handleMultimediaEvent(msg)
		if err != nil {
			log.Printf("[JT808_SESSION] Error in multimedia event handler: %v\n", err)
		}

	case 0x0801: // Multimedia Data Upload
		log.Printf("[JT808_SESSION] Multimedia data upload from device %s\n", msg.DeviceID)
		var err error
		response, err = s.handleMultimediaData(msg)
		if err != nil {
			log.Printf("[JT808_SESSION] Error in multimedia data handler: %v\n", err)
		}

	case 0x0805: // Camera command response
		log.Printf("[JT808_SESSION] Camera response (0x0805) from device %s\n", msg.DeviceID)
		s.handleCameraResponse(msg)
		// Send ACK
		var err error
		response, err = protocol.BuildGeneralResponse(msg.DeviceID, msg.SeqNum, msg.MessageID, 0)
		if err != nil {
			log.Printf("[JT808_SESSION] Error building camera response ACK: %v\n", err)
		}

	default:
		log.Printf("[JT808_SESSION] Unhandled message type 0x%04X from device %s\n",
			msg.MessageID, msg.DeviceID)
		var err error
		response, err = protocol.BuildGeneralResponse(msg.DeviceID, msg.SeqNum, msg.MessageID, 0)
		if err != nil {
			log.Printf("[JT808_SESSION] Error building default response: %v\n", err)
		}
	}

	if response != nil {
		n, err := s.Conn.Write(response)
		if err != nil {
			log.Printf("[JT808_SESSION] Error sending response: %v\n", err)
		} else {
			log.Printf("[JT808_SESSION] Sent response: %d bytes\n", n)
		}
	}

	if followUp != nil {
		n, err := s.Conn.Write(followUp)
		if err != nil {
			log.Printf("[JT808_SESSION] Error sending live stream request: %v\n", err)
		} else {
			log.Printf("[JT808_SESSION] Sent live stream request: %d bytes\n", n)
		}
	}
}

func (s *JT808Session) buildLiveStreamRequest(deviceID string) []byte {
	host, port := s.localAddrHostPort()
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 6207
	}

	// Channel 0 (or 1), DataType=1 (video), StreamType=0 (main)
	// Signature: BuildRealTimeVideoRequest(deviceID, seqNum, serverIP, tcpPort, udpPort, channelID, dataType, streamType)
	response, err := protocol.BuildRealTimeVideoRequest(deviceID, s.nextSeq(), host, port, port, 0, 1, 0)
	if err != nil {
		log.Printf("[JT808_SESSION] Error building live stream request: %v\n", err)
		return nil
	}
	return response
}

func (s *JT808Session) buildFixedRealTimeVideoCommand(deviceID string) []byte {
	const commandHex = "7e91010016011993493643006bc9066d83183f0000010000004b7e"
	data, err := hex.DecodeString(commandHex)
	if err != nil {
		log.Printf("[JT808_SESSION] Error decoding fixed 0x9101 command: %v\n", err)
		return nil
	}
	if len(data) < 15 {
		log.Printf("[JT808_SESSION] Fixed 0x9101 command too short: %d bytes\n", len(data))
		return nil
	}

	frame := data
	if frame[0] == 0x7E && frame[len(frame)-1] == 0x7E {
		frame = frame[1 : len(frame)-1]
	}
	if len(frame) < 13 {
		log.Printf("[JT808_SESSION] Fixed 0x9101 frame too short after trimming: %d bytes\n", len(frame))
		return nil
	}

	props := binary.BigEndian.Uint16(frame[2:4])
	bodyLenProps := int(props & 0x03FF)
	actualBodyLen := len(frame) - 13
	if actualBodyLen != bodyLenProps {
		log.Printf("[JT808_SESSION] Fixed 0x9101 length mismatch: props=%d, actual=%d\n", bodyLenProps, actualBodyLen)
	}

	checksum := frame[len(frame)-1]
	calculated := byte(0)
	for _, b := range frame[:len(frame)-1] {
		calculated ^= b
	}
	if checksum != calculated {
		log.Printf("[JT808_SESSION] Fixed 0x9101 checksum mismatch: got=0x%02X expected=0x%02X\n", checksum, calculated)
	}

	body := frame[12 : len(frame)-1]
	cmd, err := protocol.BuildResponse(0x9101, deviceID, s.nextSeq(), body)
	if err != nil {
		log.Printf("[JT808_SESSION] Error building 0x9101 response: %v\n", err)
		return nil
	}
	log.Printf("[JT808_SESSION] Sending 0x9101 (rebuilt) hex: % X\n", cmd)
	return cmd
}

func (s *JT808Session) nextSeq() uint16 {
	if s.outSeq == 0 {
		s.outSeq = 1
	}
	seq := s.outSeq
	s.outSeq++
	return seq
}

func (s *JT808Session) localAddrHostPort() (string, uint16) {
	addr := s.Conn.LocalAddr()
	if addr == nil {
		return "", 0
	}

	host, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "", 0
	}
	if host == "::" || host == "0.0.0.0" {
		host = ""
	}
	portVal, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 0
	}
	if portVal < 0 || portVal > 65535 {
		return host, 0
	}
	return strings.TrimSpace(host), uint16(portVal)
}

func (s *JT808Session) handleRegistration(msg *protocol.JT808Message) []byte {
	// Registration response: 0x8100
	// Body: [SeqNum(2)] [Result(1)] [AuthCode(variable)]
	// Result: 0=success, 1=vehicle registered, 2=no such vehicle, 3=terminal registered, 4=no such terminal

	authCode := []byte(msg.DeviceID)
	body := make([]byte, 0, 3+len(authCode))
	body = append(body, byte(msg.SeqNum>>8), byte(msg.SeqNum))
	body = append(body, 0) // Success
	body = append(body, authCode...)

	log.Printf("[JT808_SESSION] Registration approved for device %s, auth=%s\n", msg.DeviceID, string(authCode))
	response, err := protocol.BuildResponse(0x8100, msg.DeviceID, msg.SeqNum, body)
	if err != nil {
		log.Printf("[JT808_SESSION] Error building registration response: %v\n", err)
		return nil
	}
	return response
}

func (s *JT808Session) handleLocationReport(msg *protocol.JT808Message) {
	if len(msg.Body) < 28 {
		log.Printf("[JT808_SESSION] Location report too short: %d bytes\n", len(msg.Body))
		return
	}

	// Parse basic location info (simplified)
	// For full implementation, parse all fields according to JT808 spec
	log.Printf("[JT808_SESSION] Location data: %d bytes\n", len(msg.Body))
}

func (s *JT808Session) handleMultimediaEvent(msg *protocol.JT808Message) ([]byte, error) {
	event, err := protocol.ParseMultimediaEvent(msg.Body)
	if err != nil {
		log.Printf("[JT808_SESSION] Error parsing multimedia event: %v\n", err)
		return protocol.BuildGeneralResponse(msg.DeviceID, msg.SeqNum, msg.MessageID, 2) // Message error
	}

	log.Printf("[JT808_SESSION] Multimedia Event - ID: %d, Type: %d, Format: %d, Channel: %d\n",
		event.MultimediaID, event.MediaType, event.MediaFormat, event.ChannelID)

	// Initialize upload tracking
	if s.MultimediaStore != nil {
		s.MultimediaStore.StartUpload(
			msg.DeviceID,
			event.MultimediaID,
			event.MediaType,
			event.MediaFormat,
			event.EventCode,
			event.ChannelID,
		)
	}

	// Send upload response (no retransmissions needed initially)
	return protocol.BuildMultimediaUploadResponse(msg.DeviceID, msg.SeqNum, event.MultimediaID, nil)
}

func (s *JT808Session) handleMultimediaData(msg *protocol.JT808Message) ([]byte, error) {
	data, err := protocol.ParseMultimediaData(msg.Body)
	if err != nil {
		log.Printf("[JT808_SESSION] Error parsing multimedia data: %v\n", err)
		return protocol.BuildGeneralResponse(msg.DeviceID, msg.SeqNum, msg.MessageID, 2) // Message error
	}

	log.Printf("[JT808_SESSION] Multimedia Data - ID: %d, Type: %s, Format: %s, Size: %d bytes\n",
		data.MultimediaID, protocol.GetMediaTypeName(data.MediaType),
		protocol.GetMediaFormatName(data.MediaType, data.MediaFormat), len(data.DataPacket))

	// Store the data packet
	if s.MultimediaStore != nil && len(data.DataPacket) > 0 {
		// For single packet uploads, use packet ID 0
		err := s.MultimediaStore.AddDataPacket(msg.DeviceID, data.MultimediaID, 0, data.DataPacket)
		if err != nil {
			log.Printf("[JT808_SESSION] Error storing multimedia data: %v\n", err)
		} else {
			// Complete the upload (single packet)
			filePath, err := s.MultimediaStore.CompleteUpload(msg.DeviceID, data.MultimediaID, 1)
			if err != nil {
				log.Printf("[JT808_SESSION] Error completing multimedia upload: %v\n", err)
			} else {
				log.Printf("[JT808_SESSION] Multimedia file saved: %s\n", filePath)
			}
		}
	}

	// Send upload response (no retransmissions needed)
	return protocol.BuildMultimediaUploadResponse(msg.DeviceID, msg.SeqNum, data.MultimediaID, nil)
}

// handleCameraResponse processes 0x0805 camera command response
func (s *JT808Session) handleCameraResponse(msg *protocol.JT808Message) {
	if len(msg.Body) < 9 {
		log.Printf("[JT808_SESSION] Camera response too short: %d bytes\n", len(msg.Body))
		return
	}

	// Body format: [Channel(1)] [Command(2)] [Result(1)] [MediaID(4)] [Reserved(variable)]
	channel := msg.Body[0]
	commandID := binary.BigEndian.Uint16(msg.Body[1:3])
	result := msg.Body[3]

	// Parse result
	resultMsg := "unknown"
	switch result {
	case 0:
		resultMsg = "✓ Success"
	case 1:
		resultMsg = "✗ Failed"
	case 2:
		resultMsg = "✗ Channel not supported"
	case 3:
		resultMsg = "✗ Channel closed"
	case 4:
		resultMsg = "✗ Insufficient storage"
	case 5:
		resultMsg = "✗ Busy"
	}

	log.Printf("[JT808_SESSION] Camera Response - Channel: %d, Command: 0x%04X, Result: %s\n",
		channel, commandID, resultMsg)

	if len(msg.Body) >= 8 {
		mediaID := binary.BigEndian.Uint32(msg.Body[4:8])
		log.Printf("[JT808_SESSION]   Media ID: %d\n", mediaID)
	}

	// If success and this was a live stream request, device should now connect to media server
	if result == 0 && (commandID == 0x9101 || commandID == 0x9201) {
		log.Printf("[JT808_SESSION] ✓ Live stream accepted, waiting for JT1078 connection on port %d...\n", 6208)
	}
}
