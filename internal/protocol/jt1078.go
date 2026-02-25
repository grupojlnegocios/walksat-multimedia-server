package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"time"
)

// ============================================================================
// JT1078 Video Protocol Constants
// ============================================================================

// JT1078 Message IDs (Server → Device for video streaming)
const (
	// Real-time video/audio stream request
	MsgVideoStreamStart   uint16 = 0x9101 // Start real-time video stream
	MsgVideoStreamStop    uint16 = 0x9102 // Stop real-time video stream
	MsgAudioStreamStart   uint16 = 0x9201 // Start real-time audio stream
	MsgAudioStreamStop    uint16 = 0x9202 // Stop real-time audio stream
	MsgScreenshotCommand  uint16 = 0x9201 // Take screenshot
	MsgVideoPlaybackStart uint16 = 0x9301 // Start playback
	MsgVideoPlaybackStop  uint16 = 0x9302 // Stop playback
)

// Video Stream Data Message (Device → Server)
const (
	MsgVideoData uint16 = 0x1001 // Video data frame
	MsgAudioData uint16 = 0x1002 // Audio data frame
)

// Frame Header Sizes
const (
	VideoFrameHeaderSize = 30 // Video frame header = 30 bytes
	AudioFrameHeaderSize = 25 // Audio frame header = 25 bytes
)

// Video Codec Types
const (
	VideoCodecH264 uint8 = 0x00
	VideoCodecH265 uint8 = 0x01
	VideoCodecMJPG uint8 = 0x02
	VideoCodecMPEG uint8 = 0x03
)

// Audio Codec Types
const (
	AudioCodecPCM  uint8 = 0x00
	AudioCodecAMR  uint8 = 0x01
	AudioCodecAAC  uint8 = 0x02
	AudioCodecG726 uint8 = 0x03
	AudioCodecG729 uint8 = 0x04
	AudioCodecOpus uint8 = 0x05
)

// Frame Types
const (
	FrameTypeVideo uint8 = 0x00
	FrameTypeAudio uint8 = 0x01
)

// Video Frame Types
const (
	VideoFrameTypeI      uint8 = 0x00 // I-frame (key frame)
	VideoFrameTypeP      uint8 = 0x01 // P-frame
	VideoFrameTypeB      uint8 = 0x02 // B-frame
	VideoFrameTypeAudio  uint8 = 0x03 // Audio
	VideoFrameTypeIFrame uint8 = 0x80 // Extended I-frame marker
)

// ============================================================================
// JT1078 Frame Structures
// ============================================================================

// VideoFrameHeader - Header para frames de vídeo (30 bytes)
type VideoFrameHeader struct {
	FrameType     uint8     // Tipo de frame (I/P/B/etc)
	CodecType     uint8     // Codec H264/H265/MJPG/MPEG
	FrameRate     uint8     // Frames por segundo
	VideoWidth    uint16    // Largura (pixels)
	VideoHeight   uint16    // Altura (pixels)
	Timestamp     uint32    // Timestamp em 90kHz
	SystemTime    time.Time // Hora do sistema
	FrameSequence uint16    // Número sequencial do frame
	StreamID      uint8     // ID do stream
	Reserved      uint8     // Reservado
	DataSize      uint32    // Tamanho dos dados do frame
	PTS           uint64    // Presentation Time Stamp
}

// AudioFrameHeader - Header para frames de áudio (25 bytes)
type AudioFrameHeader struct {
	CodecType     uint8     // Codec PCM/AMR/AAC/etc
	SampleRate    uint16    // Taxa de amostragem (Hz)
	SampleBits    uint8     // Bits por amostra (8/16/32)
	ChannelCount  uint8     // Número de canais (mono=1, estéreo=2)
	FrameSequence uint16    // Número sequencial
	Timestamp     uint32    // Timestamp em 90kHz
	SystemTime    time.Time // Hora do sistema
	StreamID      uint8     // ID do stream
	Reserved      uint8     // Reservado
	DataSize      uint32    // Tamanho dos dados do frame
	PTS           uint64    // Presentation Time Stamp
}

// VideoFrame - Estrutura completa de frame de vídeo
type VideoFrame struct {
	Header    *VideoFrameHeader
	Data      []byte    // Dados comprimidos do frame
	Raw       []byte    // Frame bruto original
	DeviceID  string    // ID do dispositivo
	Timestamp time.Time // Tempo de recebimento
	Sequence  uint32    // Número sequencial global
}

// AudioFrame - Estrutura completa de frame de áudio
type AudioFrame struct {
	Header    *AudioFrameHeader
	Data      []byte    // Dados comprimidos do frame
	Raw       []byte    // Frame bruto original
	DeviceID  string    // ID do dispositivo
	Timestamp time.Time // Tempo de recebimento
	Sequence  uint32    // Número sequencial global
}

// ============================================================================
// JT1078Parser - Parser específico para protocolo JT1078
// ============================================================================

type JT1078Parser struct {
	*BaseParser
	deviceID      string
	videoSequence uint32
	audioSequence uint32
	activeStreams map[string]*StreamContext
	streamsMutex  map[string]interface{} // Placeholder para sync.Mutex
	lastFrameTime time.Time
	frameBuffer   bytes.Buffer
	maxFrameSize  int
}

// StreamContext - Contexto de stream ativo
type StreamContext struct {
	StreamID      string
	DeviceID      string
	StreamType    uint8 // 0=video, 1=audio
	CodecType     uint8 // Codec específico
	IsActive      bool
	StartTime     time.Time
	LastFrameTime time.Time
	FrameCount    int64
	BytesReceived int64
	Status        string // "active", "buffering", "error"
}

// NewJT1078Parser cria um novo parser JT1078
func NewJT1078Parser() *JT1078Parser {
	return &JT1078Parser{
		BaseParser:    NewBaseParser(),
		activeStreams: make(map[string]*StreamContext),
		streamsMutex:  make(map[string]interface{}),
		maxFrameSize:  10 * 1024 * 1024, // 10MB max frame size
		lastFrameTime: time.Now(),
	}
}

// SetDeviceID define o ID do dispositivo
func (p *JT1078Parser) SetDeviceID(deviceID string) {
	p.deviceID = deviceID
}

// GetDeviceID retorna o ID do dispositivo
func (p *JT1078Parser) GetDeviceID() string {
	return p.deviceID
}

// ============================================================================
// Video Frame Parsing
// ============================================================================

// ParseVideoFrame extrai e valida um frame de vídeo da PacketFrame
func (p *JT1078Parser) ParseVideoFrame(frame *PacketFrame) (*VideoFrame, error) {
	if len(frame.Body) < VideoFrameHeaderSize {
		return nil, fmt.Errorf("video frame too small: %d < %d", len(frame.Body), VideoFrameHeaderSize)
	}

	header := &VideoFrameHeader{}
	buf := bytes.NewReader(frame.Body[:VideoFrameHeaderSize])

	// Ler header estruturado
	if err := binary.Read(buf, binary.BigEndian, &header.FrameType); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.CodecType); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.FrameRate); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.VideoWidth); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.VideoHeight); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.Timestamp); err != nil {
		return nil, err
	}

	// Ler 6 bytes de timestamp BCD (YYMMDDHHMMSS)
	bcdTime := make([]byte, 6)
	if err := binary.Read(buf, binary.BigEndian, &bcdTime); err != nil {
		return nil, err
	}
	header.SystemTime, _ = decodeBCDTimestamp(bcdTime)

	if err := binary.Read(buf, binary.BigEndian, &header.FrameSequence); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.StreamID); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.Reserved); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.DataSize); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.PTS); err != nil {
		return nil, err
	}

	// Validar tamanho dos dados
	if header.DataSize > uint32(p.maxFrameSize) {
		return nil, fmt.Errorf("frame data exceeds max size: %d > %d", header.DataSize, p.maxFrameSize)
	}

	// Extrair dados do frame
	expectedDataStart := VideoFrameHeaderSize
	expectedDataEnd := expectedDataStart + int(header.DataSize)

	if expectedDataEnd > len(frame.Body) {
		return nil, fmt.Errorf("insufficient data: expected %d bytes, got %d", expectedDataEnd-expectedDataStart, len(frame.Body)-expectedDataStart)
	}

	frameData := frame.Body[expectedDataStart:expectedDataEnd]

	// Atualizar sequência
	p.videoSequence++

	// Validar frame type
	if !isValidVideoFrameType(header.FrameType) {
		log.Printf("[JT1078] WARNING: Invalid video frame type 0x%02x, treating as P-frame", header.FrameType)
		header.FrameType = VideoFrameTypeP
	}

	videoFrame := &VideoFrame{
		Header:    header,
		Data:      frameData,
		Raw:       frame.Body,
		DeviceID:  p.deviceID,
		Timestamp: frame.Timestamp,
		Sequence:  p.videoSequence,
	}

	return videoFrame, nil
}

// ParseAudioFrame extrai e valida um frame de áudio
func (p *JT1078Parser) ParseAudioFrame(frame *PacketFrame) (*AudioFrame, error) {
	if len(frame.Body) < AudioFrameHeaderSize {
		return nil, fmt.Errorf("audio frame too small: %d < %d", len(frame.Body), AudioFrameHeaderSize)
	}

	header := &AudioFrameHeader{}
	buf := bytes.NewReader(frame.Body[:AudioFrameHeaderSize])

	// Ler header estruturado
	if err := binary.Read(buf, binary.BigEndian, &header.CodecType); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.SampleRate); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.SampleBits); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.ChannelCount); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.FrameSequence); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.Timestamp); err != nil {
		return nil, err
	}

	// Ler 6 bytes de timestamp BCD (YYMMDDHHMMSS)
	bcdTime := make([]byte, 6)
	if err := binary.Read(buf, binary.BigEndian, &bcdTime); err != nil {
		return nil, err
	}
	header.SystemTime, _ = decodeBCDTimestamp(bcdTime)

	if err := binary.Read(buf, binary.BigEndian, &header.StreamID); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.Reserved); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.DataSize); err != nil {
		return nil, err
	}
	if err := binary.Read(buf, binary.BigEndian, &header.PTS); err != nil {
		return nil, err
	}

	// Validar tamanho dos dados
	if header.DataSize > uint32(p.maxFrameSize) {
		return nil, fmt.Errorf("audio frame data exceeds max size: %d > %d", header.DataSize, p.maxFrameSize)
	}

	// Extrair dados do frame
	expectedDataStart := AudioFrameHeaderSize
	expectedDataEnd := expectedDataStart + int(header.DataSize)

	if expectedDataEnd > len(frame.Body) {
		return nil, fmt.Errorf("insufficient audio data: expected %d bytes, got %d", expectedDataEnd-expectedDataStart, len(frame.Body)-expectedDataStart)
	}

	frameData := frame.Body[expectedDataStart:expectedDataEnd]

	// Atualizar sequência
	p.audioSequence++

	audioFrame := &AudioFrame{
		Header:    header,
		Data:      frameData,
		Raw:       frame.Body,
		DeviceID:  p.deviceID,
		Timestamp: frame.Timestamp,
		Sequence:  p.audioSequence,
	}

	return audioFrame, nil
}

// ============================================================================
// Stream Management
// ============================================================================

// StartStream inicia um novo stream
func (p *JT1078Parser) StartStream(streamID string, streamType, codecType uint8) *StreamContext {
	context := &StreamContext{
		StreamID:      streamID,
		DeviceID:      p.deviceID,
		StreamType:    streamType,
		CodecType:     codecType,
		IsActive:      true,
		StartTime:     time.Now(),
		LastFrameTime: time.Now(),
		FrameCount:    0,
		BytesReceived: 0,
		Status:        "active",
	}

	p.activeStreams[streamID] = context

	log.Printf("[JT1078] Stream started - ID: %s, Type: %s, Codec: %s, Device: %s",
		streamID,
		map[uint8]string{0: "Video", 1: "Audio"}[streamType],
		getCodecName(streamType, codecType),
		p.deviceID)

	return context
}

// StopStream para um stream ativo
func (p *JT1078Parser) StopStream(streamID string) error {
	context, ok := p.activeStreams[streamID]
	if !ok {
		return fmt.Errorf("stream %s not found", streamID)
	}

	context.IsActive = false
	context.Status = "stopped"

	duration := time.Since(context.StartTime)
	log.Printf("[JT1078] Stream stopped - ID: %s, Frames: %d, Bytes: %d, Duration: %v",
		streamID, context.FrameCount, context.BytesReceived, duration)

	delete(p.activeStreams, streamID)
	return nil
}

// GetStream obtém contexto de stream ativo
func (p *JT1078Parser) GetStream(streamID string) (*StreamContext, bool) {
	context, ok := p.activeStreams[streamID]
	return context, ok
}

// AddFrameToStream adiciona um frame a um stream
func (p *JT1078Parser) AddFrameToStream(streamID string, frameSize int) error {
	context, ok := p.activeStreams[streamID]
	if !ok {
		return fmt.Errorf("stream %s not found", streamID)
	}

	context.FrameCount++
	context.BytesReceived += int64(frameSize)
	context.LastFrameTime = time.Now()

	return nil
}

// ============================================================================
// Encode Methods
// ============================================================================

// EncodeVideoCommand cria comando para iniciar stream de vídeo
func (p *JT1078Parser) EncodeVideoCommand(channelID uint8, streamType uint8) ([]byte, error) {
	body := make([]byte, 2)
	body[0] = channelID
	body[1] = streamType // 0x00 = main stream, 0x01 = sub stream

	return p.encodeCommand(MsgVideoStreamStart, body)
}

// EncodeAudioCommand cria comando para iniciar stream de áudio
func (p *JT1078Parser) EncodeAudioCommand(channelID uint8) ([]byte, error) {
	body := make([]byte, 1)
	body[0] = channelID

	return p.encodeCommand(MsgAudioStreamStart, body)
}

// EncodeScreenshotCommand cria comando para capturar screenshot
func (p *JT1078Parser) EncodeScreenshotCommand(channelID uint8) ([]byte, error) {
	body := make([]byte, 1)
	body[0] = channelID

	return p.encodeCommand(MsgScreenshotCommand, body)
}

// encodeCommand cria um comando genérico JT1078
func (p *JT1078Parser) encodeCommand(msgID uint16, body []byte) ([]byte, error) {
	if p.deviceID == "" {
		return nil, errors.New("device ID not set")
	}

	// Criar PacketFrame
	frame := &PacketFrame{
		Flag:      FrameDelimiter,
		Checksum:  0,
		Timestamp: time.Now(),
	}

	// Codificar Device ID em BCD
	bcdDeviceID, err := EncodeBCD(p.deviceID)
	if err != nil {
		return nil, err
	}
	_ = bcdDeviceID // Used implicitly in header

	// Criar header
	frame.Header = &PacketHeader{
		MsgID:       msgID,
		Properties:  uint16(len(body)) | PropResponseRequiredMask,
		DeviceID:    p.deviceID,
		SequenceNum: 0,
	}

	frame.Body = body

	// Usar método Encode do BaseParser
	return p.Encode(frame)
}

// ============================================================================
// Helper Functions
// ============================================================================

func isValidVideoFrameType(frameType uint8) bool {
	switch frameType {
	case VideoFrameTypeI, VideoFrameTypeP, VideoFrameTypeB,
		VideoFrameTypeAudio, VideoFrameTypeIFrame:
		return true
	default:
		return false
	}
}

func getCodecName(streamType uint8, codecType uint8) string {
	if streamType == FrameTypeVideo {
		switch codecType {
		case VideoCodecH264:
			return "H.264"
		case VideoCodecH265:
			return "H.265"
		case VideoCodecMJPG:
			return "MJPG"
		case VideoCodecMPEG:
			return "MPEG"
		default:
			return "Unknown"
		}
	} else {
		switch codecType {
		case AudioCodecPCM:
			return "PCM"
		case AudioCodecAMR:
			return "AMR"
		case AudioCodecAAC:
			return "AAC"
		case AudioCodecG726:
			return "G726"
		case AudioCodecG729:
			return "G729"
		case AudioCodecOpus:
			return "Opus"
		default:
			return "Unknown"
		}
	}
}

// decodeBCDTimestamp converte BCD timestamp em time.Time
func decodeBCDTimestamp(bcdTime []byte) (time.Time, error) {
	if len(bcdTime) < 6 {
		return time.Time{}, fmt.Errorf("invalid BCD timestamp length: %d", len(bcdTime))
	}

	// BCD: YYMMDDHHMMSS
	year := int(bcdTime[0]>>4)*10 + int(bcdTime[0]&0x0F) + 2000
	month := int(bcdTime[1]>>4)*10 + int(bcdTime[1]&0x0F)
	day := int(bcdTime[2]>>4)*10 + int(bcdTime[2]&0x0F)
	hour := int(bcdTime[3]>>4)*10 + int(bcdTime[3]&0x0F)
	minute := int(bcdTime[4]>>4)*10 + int(bcdTime[4]&0x0F)
	second := int(bcdTime[5]>>4)*10 + int(bcdTime[5]&0x0F)

	return time.Date(year, time.Month(month), day, hour, minute, second, 0, time.Local), nil
}

// IsKeyFrame verifica se frame é I-frame
func (vf *VideoFrame) IsKeyFrame() bool {
	return vf.Header.FrameType == VideoFrameTypeI || vf.Header.FrameType == VideoFrameTypeIFrame
}

// GetFrameTypeString retorna tipo de frame legível
func (vf *VideoFrame) GetFrameTypeString() string {
	switch vf.Header.FrameType {
	case VideoFrameTypeI, VideoFrameTypeIFrame:
		return "I-Frame"
	case VideoFrameTypeP:
		return "P-Frame"
	case VideoFrameTypeB:
		return "B-Frame"
	default:
		return fmt.Sprintf("Unknown (0x%02x)", vf.Header.FrameType)
	}
}

// GetVideoCodecString retorna codec legível
func (vf *VideoFrame) GetVideoCodecString() string {
	switch vf.Header.CodecType {
	case VideoCodecH264:
		return "H.264"
	case VideoCodecH265:
		return "H.265"
	case VideoCodecMJPG:
		return "MJPEG"
	case VideoCodecMPEG:
		return "MPEG"
	default:
		return fmt.Sprintf("Unknown (0x%02x)", vf.Header.CodecType)
	}
}

// GetAudioCodecString retorna codec de áudio legível
func (af *AudioFrame) GetAudioCodecString() string {
	switch af.Header.CodecType {
	case AudioCodecPCM:
		return "PCM"
	case AudioCodecAMR:
		return "AMR"
	case AudioCodecAAC:
		return "AAC"
	case AudioCodecG726:
		return "G.726"
	case AudioCodecG729:
		return "G.729"
	case AudioCodecOpus:
		return "Opus"
	default:
		return fmt.Sprintf("Unknown (0x%02x)", af.Header.CodecType)
	}
}
