package stream

import (
	"fmt"
	"log"
	"time"

	"jt808-broker/internal/protocol"
)

// ============================================================================
// Video Frame Handler - Processa frames de vídeo recebidos
// ============================================================================

// VideoFrameHandler coordena recebimento e processamento de frames de vídeo
type VideoFrameHandler struct {
	StreamManager    *StreamManager
	ActiveStreams    map[string]*ActiveVideoStream
	MaxBufferSize    int
	BufferTimeout    time.Duration
	IncompleteFrames map[string]*IncompleteFrame // Frames em construção
}

// ActiveVideoStream rastreia um stream ativo
type ActiveVideoStream struct {
	DeviceID      string
	StreamID      string
	MediaType     uint8
	CodecType     uint8
	StartTime     time.Time
	LastFrameTime time.Time
	FrameCount    int64
	KeyFrameCount int64
	BytesReceived int64
	FrameRate     uint8
	VideoWidth    uint16
	VideoHeight   uint16
	Converter     *StreamConverter
	Status        string // "active", "buffering", "waiting_keyframe"
}

// IncompleteFrame rastreia frames em construção
type IncompleteFrame struct {
	StreamID     string
	DeviceID     string
	Sequence     uint32
	FrameType    uint8
	DataReceived []byte
	MaxSize      uint32
	StartTime    time.Time
	IsComplete   bool
}

// NewVideoFrameHandler cria um novo handler
func NewVideoFrameHandler(streamManager *StreamManager) *VideoFrameHandler {
	return &VideoFrameHandler{
		StreamManager:    streamManager,
		ActiveStreams:    make(map[string]*ActiveVideoStream),
		MaxBufferSize:    10 * 1024 * 1024, // 10MB
		BufferTimeout:    30 * time.Second,
		IncompleteFrames: make(map[string]*IncompleteFrame),
	}
}

// HandleVideoStreamStart inicia um stream de vídeo
func (vfh *VideoFrameHandler) HandleVideoStreamStart(deviceID string, parser *protocol.JT1078Parser, channelID uint8) error {
	streamID := fmt.Sprintf("video_ch%d", channelID)

	// Parar stream anterior se existir
	if _, exists := vfh.ActiveStreams[streamID]; exists {
		if err := vfh.StopVideoStream(deviceID, streamID); err != nil {
			log.Printf("[VIDEO_HANDLER] Warning stopping existing stream: %v", err)
		}
		log.Printf("[VIDEO_HANDLER] Restarting stream %s for device %s", streamID, deviceID)
	}

	// Enviar comando para iniciar stream no dispositivo
	cmd, err := parser.EncodeVideoCommand(channelID, 0) // 0 = main stream
	if err != nil {
		return fmt.Errorf("failed to encode video command: %v", err)
	}
	_ = cmd // Command would be sent via session

	// Criar conversor
	converter, err := vfh.StreamManager.CreateConverter(deviceID, streamID, protocol.FrameTypeVideo, "h264")
	if err != nil {
		return fmt.Errorf("failed to create converter: %v", err)
	}

	// Registrar stream ativo
	stream := &ActiveVideoStream{
		DeviceID:      deviceID,
		StreamID:      streamID,
		MediaType:     protocol.FrameTypeVideo,
		CodecType:     protocol.VideoCodecH264,
		StartTime:     time.Now(),
		LastFrameTime: time.Now(),
		FrameCount:    0,
		KeyFrameCount: 0,
		BytesReceived: 0,
		Converter:     converter,
		Status:        "waiting_keyframe",
	}

	vfh.ActiveStreams[streamID] = stream

	log.Printf("[VIDEO_HANDLER] Stream started - Device: %s, Stream: %s, Channel: %d",
		deviceID, streamID, channelID)

	return nil
}

// HandleVideoFrame processa um frame de vídeo recebido
func (vfh *VideoFrameHandler) HandleVideoFrame(deviceID string, frame *protocol.VideoFrame) error {
	streamID := fmt.Sprintf("video_ch%d", frame.Header.StreamID)

	// Validações
	if len(frame.Data) > vfh.MaxBufferSize {
		return fmt.Errorf("frame data exceeds max buffer: %d > %d", len(frame.Data), vfh.MaxBufferSize)
	}

	// Obter stream ativo
	stream, exists := vfh.ActiveStreams[streamID]
	if !exists {
		return fmt.Errorf("stream %s not found for device %s", streamID, deviceID)
	}

	// Validar codec e resolução
	if stream.CodecType != frame.Header.CodecType {
		log.Printf("[VIDEO_HANDLER] WARNING: Codec change detected - %s → %s",
			frame.GetVideoCodecString(), stream.GetVideoCodecString())
		stream.CodecType = frame.Header.CodecType
	}

	if frame.Header.VideoWidth > 0 && frame.Header.VideoHeight > 0 {
		if stream.VideoWidth != frame.Header.VideoWidth || stream.VideoHeight != frame.Header.VideoHeight {
			log.Printf("[VIDEO_HANDLER] Resolution change: %dx%d → %dx%d",
				stream.VideoWidth, stream.VideoHeight,
				frame.Header.VideoWidth, frame.Header.VideoHeight)
			stream.VideoWidth = frame.Header.VideoWidth
			stream.VideoHeight = frame.Header.VideoHeight
		}
	}

	// Tracking de frames
	stream.FrameCount++
	stream.BytesReceived += int64(len(frame.Data))
	stream.LastFrameTime = time.Now()

	if frame.IsKeyFrame() {
		stream.KeyFrameCount++
		if stream.Status == "waiting_keyframe" {
			stream.Status = "active"
			log.Printf("[VIDEO_HANDLER] First keyframe received - Stream ready")
		}
	}

	// Guardar frame rate se disponível
	if frame.Header.FrameRate > 0 {
		stream.FrameRate = frame.Header.FrameRate
	}

	// Adicionar ao conversor
	if err := stream.Converter.AddVideoFrame(frame); err != nil {
		log.Printf("[VIDEO_HANDLER] ERROR adding frame to converter: %v", err)
		return err
	}

	// Log para frames-chave
	if frame.IsKeyFrame() {
		log.Printf("[VIDEO_HANDLER] I-Frame received - Seq: %d, Size: %d bytes, Res: %dx%d",
			frame.Header.FrameSequence, len(frame.Data),
			frame.Header.VideoWidth, frame.Header.VideoHeight)
	}

	return nil
}

// StopVideoStream para um stream de vídeo
func (vfh *VideoFrameHandler) StopVideoStream(deviceID, streamID string) error {
	stream, exists := vfh.ActiveStreams[streamID]
	if !exists {
		return fmt.Errorf("stream %s not found", streamID)
	}

	// Parar conversor
	if stream.Converter != nil {
		if err := stream.Converter.Stop(); err != nil {
			log.Printf("[VIDEO_HANDLER] Error stopping converter: %v", err)
		}
	}

	// Log estatísticas finais
	duration := time.Since(stream.StartTime)
	fps := 0.0
	if duration.Seconds() > 0 {
		fps = float64(stream.FrameCount) / duration.Seconds()
	}

	log.Printf("[VIDEO_HANDLER] Stream stopped - Device: %s, Stream: %s\n"+
		"  Frames: %d (KeyFrames: %d), Bytes: %d, Duration: %v, FPS: %.2f",
		deviceID, streamID, stream.FrameCount, stream.KeyFrameCount,
		stream.BytesReceived, duration, fps)

	delete(vfh.ActiveStreams, streamID)
	return nil
}

// GetStreamStatus obtém status de um stream
func (vfh *VideoFrameHandler) GetStreamStatus(streamID string) (map[string]interface{}, error) {
	stream, exists := vfh.ActiveStreams[streamID]
	if !exists {
		return nil, fmt.Errorf("stream %s not found", streamID)
	}

	duration := time.Since(stream.StartTime)
	fps := 0.0
	if duration.Seconds() > 0 {
		fps = float64(stream.FrameCount) / duration.Seconds()
	}

	return map[string]interface{}{
		"device_id":      stream.DeviceID,
		"stream_id":      stream.StreamID,
		"codec":          stream.GetVideoCodecString(),
		"resolution":     fmt.Sprintf("%dx%d", stream.VideoWidth, stream.VideoHeight),
		"frame_rate":     stream.FrameRate,
		"status":         stream.Status,
		"total_frames":   stream.FrameCount,
		"key_frames":     stream.KeyFrameCount,
		"bytes_received": stream.BytesReceived,
		"duration":       duration.String(),
		"fps":            fmt.Sprintf("%.2f", fps),
		"last_frame":     time.Since(stream.LastFrameTime).String(),
	}, nil
}

// GetAllStreams retorna informações de todos os streams
func (vfh *VideoFrameHandler) GetAllStreams() map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{})

	for streamID := range vfh.ActiveStreams {
		if status, err := vfh.GetStreamStatus(streamID); err == nil {
			result[streamID] = status
		}
	}

	return result
}

// CleanupStaleStreams remove streams inativos há muito tempo
func (vfh *VideoFrameHandler) CleanupStaleStreams(maxInactivity time.Duration) {
	now := time.Now()
	for streamID, stream := range vfh.ActiveStreams {
		if now.Sub(stream.LastFrameTime) > maxInactivity {
			log.Printf("[VIDEO_HANDLER] Cleaning up stale stream: %s (inactive for %v)",
				streamID, now.Sub(stream.LastFrameTime))
			_ = vfh.StopVideoStream(stream.DeviceID, streamID)
		}
	}
}

// ============================================================================
// Helper Methods
// ============================================================================

// GetVideoCodecString retorna codec legível
func (avs *ActiveVideoStream) GetVideoCodecString() string {
	switch avs.CodecType {
	case protocol.VideoCodecH264:
		return "H.264"
	case protocol.VideoCodecH265:
		return "H.265"
	case protocol.VideoCodecMJPG:
		return "MJPEG"
	case protocol.VideoCodecMPEG:
		return "MPEG"
	default:
		return fmt.Sprintf("Unknown (0x%02x)", avs.CodecType)
	}
}

// ============================================================================
// Audio Frame Handler
// ============================================================================

// AudioFrameHandler coordena recebimento de frames de áudio
type AudioFrameHandler struct {
	StreamManager *StreamManager
	ActiveStreams map[string]*ActiveAudioStream
	MaxBufferSize int
	BufferTimeout time.Duration
}

// ActiveAudioStream rastreia um stream de áudio ativo
type ActiveAudioStream struct {
	DeviceID      string
	StreamID      string
	CodecType     uint8
	SampleRate    uint16
	SampleBits    uint8
	ChannelCount  uint8
	StartTime     time.Time
	LastFrameTime time.Time
	FrameCount    int64
	BytesReceived int64
	Converter     *StreamConverter
	Status        string
}

// NewAudioFrameHandler cria um novo handler de áudio
func NewAudioFrameHandler(streamManager *StreamManager) *AudioFrameHandler {
	return &AudioFrameHandler{
		StreamManager: streamManager,
		ActiveStreams: make(map[string]*ActiveAudioStream),
		MaxBufferSize: 5 * 1024 * 1024, // 5MB
		BufferTimeout: 30 * time.Second,
	}
}

// HandleAudioStreamStart inicia um stream de áudio
func (afh *AudioFrameHandler) HandleAudioStreamStart(deviceID string, parser *protocol.JT1078Parser, channelID uint8) error {
	streamID := fmt.Sprintf("audio_ch%d", channelID)

	// Parar stream anterior se existir
	if _, exists := afh.ActiveStreams[streamID]; exists {
		if err := afh.StopAudioStream(deviceID, streamID); err != nil {
			log.Printf("[AUDIO_HANDLER] Warning stopping existing stream: %v", err)
		}
	}

	// Enviar comando para iniciar stream
	cmd, err := parser.EncodeAudioCommand(channelID)
	if err != nil {
		return fmt.Errorf("failed to encode audio command: %v", err)
	}
	_ = cmd // Command would be sent via session

	// Criar conversor
	converter, err := afh.StreamManager.CreateConverter(deviceID, streamID, protocol.FrameTypeAudio, "aac")
	if err != nil {
		return fmt.Errorf("failed to create converter: %v", err)
	}

	// Registrar stream ativo
	stream := &ActiveAudioStream{
		DeviceID:      deviceID,
		StreamID:      streamID,
		CodecType:     protocol.AudioCodecAAC,
		StartTime:     time.Now(),
		LastFrameTime: time.Now(),
		FrameCount:    0,
		BytesReceived: 0,
		Converter:     converter,
		Status:        "active",
	}

	afh.ActiveStreams[streamID] = stream

	log.Printf("[AUDIO_HANDLER] Stream started - Device: %s, Stream: %s, Channel: %d",
		deviceID, streamID, channelID)

	return nil
}

// HandleAudioFrame processa um frame de áudio
func (afh *AudioFrameHandler) HandleAudioFrame(deviceID string, frame *protocol.AudioFrame) error {
	streamID := fmt.Sprintf("audio_ch%d", frame.Header.StreamID)

	// Validações
	if len(frame.Data) > afh.MaxBufferSize {
		return fmt.Errorf("audio frame exceeds max buffer: %d > %d", len(frame.Data), afh.MaxBufferSize)
	}

	// Obter stream ativo
	stream, exists := afh.ActiveStreams[streamID]
	if !exists {
		return fmt.Errorf("audio stream %s not found", streamID)
	}

	// Atualizar informações
	stream.CodecType = frame.Header.CodecType
	stream.SampleRate = frame.Header.SampleRate
	stream.SampleBits = frame.Header.SampleBits
	stream.ChannelCount = frame.Header.ChannelCount
	stream.FrameCount++
	stream.BytesReceived += int64(len(frame.Data))
	stream.LastFrameTime = time.Now()

	// Adicionar ao conversor
	if err := stream.Converter.AddAudioFrame(frame); err != nil {
		log.Printf("[AUDIO_HANDLER] ERROR adding frame: %v", err)
		return err
	}

	return nil
}

// StopAudioStream para um stream de áudio
func (afh *AudioFrameHandler) StopAudioStream(deviceID, streamID string) error {
	stream, exists := afh.ActiveStreams[streamID]
	if !exists {
		return fmt.Errorf("audio stream %s not found", streamID)
	}

	// Parar conversor
	if stream.Converter != nil {
		if err := stream.Converter.Stop(); err != nil {
			log.Printf("[AUDIO_HANDLER] Error stopping converter: %v", err)
		}
	}

	// Log estatísticas
	duration := time.Since(stream.StartTime)
	log.Printf("[AUDIO_HANDLER] Stream stopped - Device: %s, Stream: %s, Frames: %d, Bytes: %d, Duration: %v",
		deviceID, streamID, stream.FrameCount, stream.BytesReceived, duration)

	delete(afh.ActiveStreams, streamID)
	return nil
}

// GetStreamStatus obtém status de um stream de áudio
func (afh *AudioFrameHandler) GetStreamStatus(streamID string) (map[string]interface{}, error) {
	stream, exists := afh.ActiveStreams[streamID]
	if !exists {
		return nil, fmt.Errorf("audio stream %s not found", streamID)
	}

	return map[string]interface{}{
		"device_id":      stream.DeviceID,
		"stream_id":      stream.StreamID,
		"codec":          stream.GetAudioCodecString(),
		"sample_rate":    stream.SampleRate,
		"sample_bits":    stream.SampleBits,
		"channels":       stream.ChannelCount,
		"status":         stream.Status,
		"frames":         stream.FrameCount,
		"bytes_received": stream.BytesReceived,
		"duration":       time.Since(stream.StartTime).String(),
	}, nil
}

// GetAudioCodecString retorna codec legível
func (aas *ActiveAudioStream) GetAudioCodecString() string {
	switch aas.CodecType {
	case protocol.AudioCodecPCM:
		return "PCM"
	case protocol.AudioCodecAMR:
		return "AMR"
	case protocol.AudioCodecAAC:
		return "AAC"
	case protocol.AudioCodecG726:
		return "G.726"
	case protocol.AudioCodecG729:
		return "G.729"
	case protocol.AudioCodecOpus:
		return "Opus"
	default:
		return fmt.Sprintf("Unknown (0x%02x)", aas.CodecType)
	}
}
