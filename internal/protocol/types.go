package protocol

import (
	"errors"
	"time"
)

// ============================================================================
// Constantes de Message IDs (JT808)
// ============================================================================

// Login/Logout/Heartbeat Messages
const (
	MsgLogin     uint16 = 0x0001 // Terminal Login
	MsgLogout    uint16 = 0x0002 // Terminal Logout
	MsgHeartbeat uint16 = 0x0003 // Terminal Heartbeat
	MsgAuth      uint16 = 0x0102 // Terminal Authentication
)

// Positioning and Location Messages
const (
	MsgLocationReport  uint16 = 0x0200 // Position Report Upload
	MsgLocationInquiry uint16 = 0x8602 // Query Location (Platform → Device)
)

// Device Information Messages
const (
	MsgDeviceInfo uint16 = 0x0104 // Device Information Report
)

// Event and Alert Messages
const (
	MsgEventReport uint16 = 0x0301 // Event Report
	MsgAlarmReport uint16 = 0x0302 // Alarm Report
)

// Multimedia Messages (JT808 Extension)
const (
	MsgMultimediaEvent uint16 = 0x0800 // Multimedia Event Upload
	MsgMultimediaData  uint16 = 0x0801 // Multimedia Data Upload
	MsgCameraResponse  uint16 = 0x0805 // Camera command response (device → platform)
)

// Platform Response Messages (0x8xxx)
const (
	MsgGeneralResponse    uint16 = 0x8001 // General Response
	MsgControlDevice      uint16 = 0x8201 // Control Device Command
	MsgSetParameters      uint16 = 0x8301 // Set Parameters Command
	MsgMultimediaResponse uint16 = 0x8800 // Multimedia Upload Response
)

// ============================================================================
// Constantes de Propriedades
// ============================================================================

// Properties Bit Masks
const (
	PropEncryptionMask       uint16 = 0x1000 // Bit 12: Encryption flag
	PropResponseRequiredMask uint16 = 0x4000 // Bit 14: Response required
	PropBodyLengthMask       uint16 = 0x03FF // Bits 0-9: Body length (0-1023)
)

// Encryption Types
const (
	EncryptionNone = iota
	EncryptionRSA
	EncryptionAES
)

// ============================================================================
// Tipos de Estrutura de Pacote
// ============================================================================

// PacketFrame representa um quadro completo recebido/enviado
type PacketFrame struct {
	Flag      byte          // Delimitador 0x7E
	Header    *PacketHeader // Header de 13 bytes
	Body      []byte        // Dados variáveis (0 a 1023+)
	Checksum  byte          // Validação XOR
	Timestamp time.Time     // Hora de recebimento
	Raw       []byte        // Dados brutos original (antes de unescape)
}

// PacketHeader contém os 13 bytes de header estruturados
type PacketHeader struct {
	MsgID       uint16 // Message ID (2 bytes, big-endian)
	Properties  uint16 // Flags e comprimento (2 bytes, big-endian)
	DeviceID    string // ID do terminal (BCD decodificado - 6 bytes)
	SequenceNum uint16 // Número de sequência (2 bytes, big-endian)
}

// GetBodyLength extrai o comprimento do body a partir de Properties
func (h *PacketHeader) GetBodyLength() uint16 {
	return h.Properties & PropBodyLengthMask
}

// IsResponseRequired verifica se resposta é necessária
func (h *PacketHeader) IsResponseRequired() bool {
	return (h.Properties & PropResponseRequiredMask) != 0
}

// IsEncrypted verifica se body é criptografado
func (h *PacketHeader) IsEncrypted() bool {
	return (h.Properties & PropEncryptionMask) != 0
}

// ============================================================================
// Tipos de Mensagem Específicas
// ============================================================================

// LoginMessage - Estrutura do body para 0x0001
type LoginMessage struct {
	ProvinceCode uint16 // 2 bytes
	CityCode     uint16 // 2 bytes
	CompanyID    uint16 // 2 bytes
	TerminalType string // 5 bytes ASCII
	TerminalID   string // 20 bytes ASCII
	PlateColor   byte   // 1 byte
	LicensePlate string // 12 bytes ASCII
}

// LocationReport - Estrutura do body para 0x0200
type LocationReport struct {
	Latitude  int32  // 4 bytes: graus * 1e6
	Longitude int32  // 4 bytes: graus * 1e6
	Elevation uint16 // 2 bytes: metros
	Speed     uint16 // 2 bytes: km/h
	Direction uint16 // 2 bytes: graus (0-359)
	Timestamp string // 6 bytes BCD: YYMMDDHHMMSS
	ExtraInfo []byte // Opcional: campos adicionais
}

// MultimediaEventMessage - Estrutura do body para 0x0800
type MultimediaEventMessage struct {
	MultimediaID uint32 // 4 bytes: ID único do arquivo
	MediaType    byte   // 1 byte: 0=Image, 1=Audio, 2=Video
	MediaFormat  byte   // 1 byte: formato específico
	EventCode    byte   // 1 byte: tipo de evento
	ChannelID    byte   // 1 byte: canal de câmera
}

// MultimediaDataMessage - Estrutura do body para 0x0801
type MultimediaDataMessage struct {
	MultimediaID uint32          // 4 bytes
	PacketID     uint16          // 2 bytes: identificador do pacote
	TotalPackets uint16          // 2 bytes: total de pacotes
	LocationInfo *LocationReport // Opcional: 28 bytes (se presente)
	Data         []byte          // Dados variáveis do arquivo
}

// GeneralResponse - Estrutura do body para 0x8001
type GeneralResponse struct {
	CommandSeqNum uint16 // 2 bytes: seq do comando respondido
	CommandID     uint16 // 2 bytes: ID do comando respondido
	ResultCode    byte   // 1 byte: 0=Success, 1=Fail, 2=Invalid, 3=Unsupported
}

// MultimediaResponse - Estrutura do body para 0x8800
type MultimediaResponse struct {
	MultimediaID   uint32   // 4 bytes
	RetransmitList []uint16 // Lista de IDs de pacotes para retransmissão
}

// ============================================================================
// Interfaces de Parser
// ============================================================================

// PacketParser interface para diferentes protocolos
type PacketParser interface {
	// Push adiciona novos dados ao buffer e retorna quadros completos
	Push(data []byte) ([]*PacketFrame, error)

	// Encode converte um PacketFrame em bytes para transmissão
	Encode(frame *PacketFrame) ([]byte, error)

	// SetDeviceID define ID do dispositivo (pode ser extraído do frame)
	SetDeviceID(deviceID string)

	// GetDeviceID retorna ID do dispositivo atual
	GetDeviceID() string
}

// ============================================================================
// Variáveis de Erros
// ============================================================================

var (
	ErrInvalidFrame     = errors.New("invalid frame: missing or incorrect delimiters")
	ErrInvalidHeader    = errors.New("invalid header: incorrect length")
	ErrInvalidChecksum  = errors.New("invalid checksum")
	ErrInvalidDeviceID  = errors.New("invalid device ID format")
	ErrInvalidMessageID = errors.New("invalid message ID")
	ErrBodyTooLarge     = errors.New("body exceeds maximum length")
	ErrIncompleteFrame  = errors.New("incomplete frame received")
	ErrInvalidBCD       = errors.New("invalid BCD encoding")
	ErrDecryptionFailed = errors.New("decryption failed")
)

// ============================================================================
// Constantes de Delimitadores e Escape
// ============================================================================

const (
	FrameDelimiter  byte = 0x7E // Delimitador de início/fim
	EscapeSequence  byte = 0x7D // Sequência de escape
	EscapeDelimiter byte = 0x02 // 0x7D 0x02 → 0x7E
	EscapeEscape    byte = 0x01 // 0x7D 0x01 → 0x7D
)

// ============================================================================
// Tipos de Mídia (para mensagens multimedia)
// ============================================================================

const (
	MediaTypeImage = iota
	MediaTypeAudio
	MediaTypeVideo
)

// ============================================================================
// Formatos de Mídia
// ============================================================================

const (
	// Image Formats
	MediaFormatJPEG uint8 = 0x00
	MediaFormatTIF  uint8 = 0x01

	// Audio Formats
	MediaFormatWAV uint8 = 0x00
	MediaFormatMP3 uint8 = 0x01

	// Video Formats
	MediaFormatAVI  uint8 = 0x00
	MediaFormatWMV  uint8 = 0x01
	MediaFormatRMVB uint8 = 0x02
	MediaFormatFLV  uint8 = 0x03
	MediaFormatMP4  uint8 = 0x04
	MediaFormatH264 uint8 = 0x05
	MediaFormatH265 uint8 = 0x06
)

// ============================================================================
// Códigos de Evento Multimedia
// ============================================================================

const (
	EventCodePlatformCommand   uint8 = 0x01
	EventCodeTimedAction       uint8 = 0x02
	EventCodeRobberyAlarm      uint8 = 0x03
	EventCodeCollisionRollover uint8 = 0x04
	EventCodeDoorOpenClose     uint8 = 0x05
	EventCodeDoorOpenThreshold uint8 = 0x06
	EventCodeDistanceCapture   uint8 = 0x07
)

// ============================================================================
// Helper Functions para Nomes de Tipos
// ============================================================================

// GetMessageTypeName retorna o nome legível de um Message ID
func GetMessageTypeName(msgID uint16) string {
	switch msgID {
	case MsgLogin:
		return "Login"
	case MsgLogout:
		return "Logout"
	case MsgHeartbeat:
		return "Heartbeat"
	case MsgAuth:
		return "Authentication"
	case MsgLocationReport:
		return "Location Report"
	case MsgEventReport:
		return "Event Report"
	case MsgMultimediaEvent:
		return "Multimedia Event"
	case MsgMultimediaData:
		return "Multimedia Data"
	case MsgCameraResponse:
		return "Camera Response"
	case MsgGeneralResponse:
		return "General Response"
	case MsgMultimediaResponse:
		return "Multimedia Response"
	case MsgControlDevice:
		return "Control Device"
	default:
		return "Unknown"
	}
}

// GetMediaTypeName retorna o nome legível de um tipo de mídia
func GetMediaTypeName(mediaType byte) string {
	switch mediaType {
	case MediaTypeImage:
		return "Image"
	case MediaTypeAudio:
		return "Audio"
	case MediaTypeVideo:
		return "Video"
	default:
		return "Unknown"
	}
}

// GetMediaFormatExt retorna a extensão do arquivo conforme o tipo e formato JT808/JT1078
func GetMediaFormatExt(mediaType, format byte) string {
	switch mediaType {
	case MediaTypeImage:
		switch format {
		case MediaFormatJPEG:
			return ".jpg"
		case MediaFormatTIF:
			return ".tif"
		}
	case MediaTypeAudio:
		switch format {
		case MediaFormatWAV:
			return ".wav"
		case MediaFormatMP3:
			return ".mp3"
		}
	case MediaTypeVideo:
		switch format {
		case MediaFormatAVI:
			return ".avi"
		case MediaFormatWMV:
			return ".wmv"
		case MediaFormatRMVB:
			return ".rmvb"
		case MediaFormatFLV:
			return ".flv"
		case MediaFormatMP4:
			return ".mp4"
		case MediaFormatH264:
			return ".h264"
		case MediaFormatH265:
			return ".h265"
		}
	}
	return ".bin"
}

// GetEventCodeName retorna o nome legível de um código de evento
func GetEventCodeName(code byte) string {
	switch code {
	case EventCodePlatformCommand:
		return "Platform Command"
	case EventCodeTimedAction:
		return "Timed Action"
	case EventCodeRobberyAlarm:
		return "Robbery Alarm"
	case EventCodeCollisionRollover:
		return "Collision/Rollover"
	case EventCodeDoorOpenClose:
		return "Door Open/Close"
	case EventCodeDoorOpenThreshold:
		return "Door Open Threshold"
	case EventCodeDistanceCapture:
		return "Distance Capture"
	default:
		return "Unknown"
	}
}
