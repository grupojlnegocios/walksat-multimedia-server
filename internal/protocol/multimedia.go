package protocol

import (
	"encoding/binary"
	"fmt"
	"log"
)

// MultimediaEvent represents a 0x0800 multimedia event message
type MultimediaEvent struct {
	MultimediaID uint32
	MediaType    byte
	MediaFormat  byte
	EventCode    byte
	ChannelID    byte
}

// MultimediaData represents a 0x0801 multimedia data upload message
type MultimediaData struct {
	MultimediaID   uint32
	MediaType      byte
	MediaFormat    byte
	EventCode      byte
	ChannelID      byte
	LocationReport []byte
	DataPacket     []byte
}

// ParseMultimediaEvent parses a 0x0800 multimedia event message
func ParseMultimediaEvent(body []byte) (*MultimediaEvent, error) {
	if len(body) < 8 {
		return nil, fmt.Errorf("multimedia event body too short: %d bytes", len(body))
	}

	event := &MultimediaEvent{
		MultimediaID: binary.BigEndian.Uint32(body[0:4]),
		MediaType:    body[4],
		MediaFormat:  body[5],
		EventCode:    body[6],
		ChannelID:    body[7],
	}

	log.Printf("[MULTIMEDIA] Event parsed - ID: %d, Type: %s, Format: %s, Event: %s, Channel: %d\n",
		event.MultimediaID,
		GetMediaTypeName(event.MediaType),
		GetMediaFormatName(event.MediaType, event.MediaFormat),
		GetEventCodeName(event.EventCode),
		event.ChannelID)

	return event, nil
}

// ParseMultimediaData parses a 0x0801 multimedia data upload message
func ParseMultimediaData(body []byte) (*MultimediaData, error) {
	if len(body) < 8 {
		return nil, fmt.Errorf("multimedia data body too short: %d bytes", len(body))
	}

	data := &MultimediaData{
		MultimediaID: binary.BigEndian.Uint32(body[0:4]),
		MediaType:    body[4],
		MediaFormat:  body[5],
		EventCode:    body[6],
		ChannelID:    body[7],
	}

	offset := 8

	// Location report (28 bytes if present)
	if len(body) >= offset+28 {
		data.LocationReport = body[offset : offset+28]
		offset += 28
		log.Printf("[MULTIMEDIA] Location report included: %d bytes\n", len(data.LocationReport))
	}

	// Data packet (remaining bytes)
	if len(body) > offset {
		data.DataPacket = body[offset:]
		log.Printf("[MULTIMEDIA] Data packet: %d bytes\n", len(data.DataPacket))
	}

	log.Printf("[MULTIMEDIA] Data parsed - ID: %d, Type: %s, Format: %s, Event: %s, Channel: %d, Total: %d bytes\n",
		data.MultimediaID,
		GetMediaTypeName(data.MediaType),
		GetMediaFormatName(data.MediaType, data.MediaFormat),
		GetEventCodeName(data.EventCode),
		data.ChannelID,
		len(data.DataPacket))

	return data, nil
}

// GetMediaFormatName returns human-readable media format
func GetMediaFormatName(mediaType, format byte) string {
	if mediaType == 0 { // Image formats
		formats := map[byte]string{
			0: "JPEG",
			1: "TIF",
			2: "MP3",
			3: "WAV",
			4: "WMV",
		}
		if name, ok := formats[format]; ok {
			return name
		}
	} else if mediaType == 1 { // Audio formats
		formats := map[byte]string{
			0: "WAV",
			1: "MP3",
		}
		if name, ok := formats[format]; ok {
			return name
		}
	} else if mediaType == 2 { // Video formats
		formats := map[byte]string{
			0: "AVI",
			1: "WMV",
			2: "RMVB",
			3: "FLV",
			4: "MP4",
			5: "H.264",
		}
		if name, ok := formats[format]; ok {
			return name
		}
	}
	return fmt.Sprintf("Unknown(%d)", format)
}

// BuildMultimediaUploadResponse builds response for 0x0800/0x0801
func BuildMultimediaUploadResponse(deviceID string, seqNum uint16, multimediaID uint32, retransmitList []uint16) ([]byte, error) {
	// Response message ID: 0x8800
	// Body: [MultimediaID(4)] [RetransmitPacketCount(1)] [RetransmitPacketIDs(2*n)]

	bodyLen := 5 + len(retransmitList)*2
	body := make([]byte, bodyLen)

	binary.BigEndian.PutUint32(body[0:4], multimediaID)
	body[4] = byte(len(retransmitList))

	offset := 5
	for _, packetID := range retransmitList {
		binary.BigEndian.PutUint16(body[offset:offset+2], packetID)
		offset += 2
	}

	log.Printf("[MULTIMEDIA] Building upload response - MultimediaID: %d, Retransmit count: %d\n",
		multimediaID, len(retransmitList))

	return BuildResponse(0x8800, deviceID, seqNum, body)
}

// GetFileExtension returns file extension based on media type and format
func GetFileExtension(mediaType, format byte) string {
	if mediaType == 0 { // Image
		formats := map[byte]string{
			0: ".jpg",
			1: ".tif",
		}
		if ext, ok := formats[format]; ok {
			return ext
		}
		return ".jpg"
	} else if mediaType == 1 { // Audio
		formats := map[byte]string{
			0: ".wav",
			1: ".mp3",
		}
		if ext, ok := formats[format]; ok {
			return ext
		}
		return ".mp3"
	} else if mediaType == 2 { // Video
		formats := map[byte]string{
			0: ".avi",
			1: ".wmv",
			2: ".rmvb",
			3: ".flv",
			4: ".mp4",
			5: ".h264",
		}
		if ext, ok := formats[format]; ok {
			return ext
		}
		return ".mp4"
	}
	return ".bin"
}
