# Guia de Integração - Streaming de Vídeo

## Exemplo Completo de Integração

Este guia mostra como integrar o sistema de streaming de vídeo JT1078 com a sessão JT808 existente.

## Passo 1: Modificar JT808Session

```go
package stream

import (
	"fmt"
	"sync"
	"time"

	"jt808-broker/internal/protocol"
	"jt808-broker/internal/tcp"
)

// JT808Session com suporte a streaming de vídeo
type JT808Session struct {
	Conn              tcp.Connection
	DeviceID          string
	Parser            *protocol.JT808Parser
	JT1078Parser      *protocol.JT1078Parser // Novo
	Registry          *Registry
	MultimediaStore   *MultimediaStore
	StreamManager     *StreamManager         // Novo
	VideoFrameHandler *VideoFrameHandler     // Novo
	AudioFrameHandler *AudioFrameHandler     // Novo
	LastHeartbeat     time.Time
	Mu                sync.RWMutex
	Done              chan bool
}

// NewJT808Session cria nova sessão
func NewJT808Session(conn tcp.Connection, registry *Registry, store *MultimediaStore) *JT808Session {
	outputDir := "/var/jt808-broker/streams" // Configurável

	return &JT808Session{
		Conn:            conn,
		Parser:          protocol.NewJT808Parser(),
		Registry:        registry,
		MultimediaStore: store,
		StreamManager:   NewStreamManager(outputDir),
		Done:            make(chan bool),
		LastHeartbeat:   time.Now(),
	}
}

// Run executa loop de sessão
func (s *JT808Session) Run() {
	defer close(s.Done)

	// Goroutine para cleanup de streams inativos
	go s.cleanupStaleStreams()

	buffer := make([]byte, 65536)
	for {
		select {
		case <-s.Done:
			return
		default:
			// Ler dados TCP
			n, err := s.Conn.Read(buffer)
			if err != nil {
				log.Printf("[SESSION] Connection error: %v", err)
				s.cleanup()
				return
			}

			if n == 0 {
				continue
			}

			// Processar dados
			s.processData(buffer[:n])
		}
	}
}

// processData processa dados recebidos
func (s *JT808Session) processData(data []byte) {
	// Enviar para JT808 parser
	frames, err := s.Parser.Push(data)
	if err != nil {
		log.Printf("[SESSION] Parser error: %v", err)
		return
	}

	for _, frame := range frames {
		// Registrar dispositivo no primeiro login
		if frame.Header.MsgID == protocol.MsgLogin && s.DeviceID == "" {
			s.DeviceID = frame.Header.DeviceID
			s.Registry.Register(s.DeviceID, s)

			// Inicializar parsers de vídeo
			s.JT1078Parser = protocol.NewJT1078Parser()
			s.JT1078Parser.SetDeviceID(s.DeviceID)
			s.VideoFrameHandler = NewVideoFrameHandler(s.StreamManager)
			s.AudioFrameHandler = NewAudioFrameHandler(s.StreamManager)

			log.Printf("[SESSION] Device registered: %s", s.DeviceID)
		}

		// Processar mensagem
		s.handleMessage(frame)
	}
}

// handleMessage processa uma mensagem
func (s *JT808Session) handleMessage(frame *protocol.PacketFrame) {
	// Validar device registrado
	if s.DeviceID == "" && frame.Header.MsgID != protocol.MsgLogin {
		log.Printf("[SESSION] Ignoring message from unregistered device")
		return
	}

	// Dispatch por tipo de mensagem
	switch frame.Header.MsgID {
	case protocol.MsgLogin:
		s.handleLogin(frame)

	case protocol.MsgLogout:
		s.handleLogout(frame)

	case protocol.MsgHeartbeat:
		s.handleHeartbeat(frame)

	case protocol.MsgLocationReport:
		s.handleLocationReport(frame)

	case protocol.MsgMultimediaEvent:
		s.handleMultimediaEvent(frame)

	case protocol.MsgMultimediaData:
		s.handleMultimediaData(frame)

	case protocol.MsgVideoData: // 0x1001 - Novo
		s.handleVideoData(frame)

	case protocol.MsgAudioData: // 0x1002 - Novo
		s.handleAudioData(frame)

	case protocol.MsgGeneralResponse:
		s.handleGeneralResponse(frame)

	case protocol.MsgMultimediaResponse:
		s.handleMultimediaResponse(frame)

	default:
		log.Printf("[SESSION] Unknown message type: 0x%04x", frame.Header.MsgID)
	}
}

// ============================================================================
// Handlers de Vídeo/Áudio (Novo)
// ============================================================================

// handleVideoData processa frame de vídeo
func (s *JT808Session) handleVideoData(frame *protocol.PacketFrame) {
	// Parsear frame de vídeo
	videoFrame, err := s.JT1078Parser.ParseVideoFrame(frame)
	if err != nil {
		log.Printf("[SESSION] Error parsing video frame: %v", err)
		return
	}

	// Processar com handler
	if err := s.VideoFrameHandler.HandleVideoFrame(s.DeviceID, videoFrame); err != nil {
		log.Printf("[SESSION] Error handling video frame: %v", err)
		return
	}

	// Log para I-frames
	if videoFrame.IsKeyFrame() {
		log.Printf("[VIDEO] I-Frame received\n"+
			"  Codec: %s, Resolution: %dx%d, FPS: %d\n"+
			"  Sequence: %d, Size: %d bytes\n"+
			"  Timestamp: %d (90kHz), PTS: %dms",
			videoFrame.GetVideoCodecString(),
			videoFrame.Header.VideoWidth,
			videoFrame.Header.VideoHeight,
			videoFrame.Header.FrameRate,
			videoFrame.Header.FrameSequence,
			len(videoFrame.Data),
			videoFrame.Header.Timestamp,
			videoFrame.Header.PTS/90)
	}
}

// handleAudioData processa frame de áudio
func (s *JT808Session) handleAudioData(frame *protocol.PacketFrame) {
	// Parsear frame de áudio
	audioFrame, err := s.JT1078Parser.ParseAudioFrame(frame)
	if err != nil {
		log.Printf("[SESSION] Error parsing audio frame: %v", err)
		return
	}

	// Processar com handler
	if err := s.AudioFrameHandler.HandleAudioFrame(s.DeviceID, audioFrame); err != nil {
		log.Printf("[SESSION] Error handling audio frame: %v", err)
		return
	}

	log.Printf("[AUDIO] Frame: %s, %dHz, %d-bit, %d channels, Size: %d bytes",
		audioFrame.GetAudioCodecString(),
		audioFrame.Header.SampleRate,
		audioFrame.Header.SampleBits,
		audioFrame.Header.ChannelCount,
		len(audioFrame.Data))
}

// handleLogin processa login
func (s *JT808Session) handleLogin(frame *protocol.PacketFrame) {
	log.Printf("[SESSION] Device login: %s", frame.Header.DeviceID)

	// Enviar resposta
	response, err := protocol.BuildResponse(
		frame.Header.SequenceNum,
		frame.Header.MessageID,
		1, // Success
		frame.Header.DeviceID,
	)
	if err != nil {
		log.Printf("[SESSION] Error building response: %v", err)
		return
	}

	if _, err := s.Conn.Write(response); err != nil {
		log.Printf("[SESSION] Error sending response: %v", err)
	}
}

// handleLogout processa logout
func (s *JT808Session) handleLogout(frame *protocol.PacketFrame) {
	log.Printf("[SESSION] Device logout: %s", s.DeviceID)

	// Parar todos os streams
	s.StreamManager.StopAll()

	// Enviar resposta
	response, err := protocol.BuildResponse(
		frame.Header.SequenceNum,
		frame.Header.MessageID,
		1,
		frame.Header.DeviceID,
	)
	if err != nil {
		log.Printf("[SESSION] Error building response: %v", err)
		return
	}

	if _, err := s.Conn.Write(response); err != nil {
		log.Printf("[SESSION] Error sending response: %v", err)
	}

	// Fechar sessão
	s.cleanup()
}

// handleHeartbeat processa heartbeat
func (s *JT808Session) handleHeartbeat(frame *protocol.PacketFrame) {
	s.LastHeartbeat = time.Now()

	// Enviar resposta
	response, err := protocol.BuildResponse(
		frame.Header.SequenceNum,
		frame.Header.MessageID,
		1,
		frame.Header.DeviceID,
	)
	if err != nil {
		return
	}

	s.Conn.Write(response)
}

// handleLocationReport processa relatório de localização
func (s *JT808Session) handleLocationReport(frame *protocol.PacketFrame) {
	// Processar localização (código existente)
	log.Printf("[SESSION] Location report from %s", s.DeviceID)
}

// handleMultimediaEvent processa evento multimedia
func (s *JT808Session) handleMultimediaEvent(frame *protocol.PacketFrame) {
	// Processar evento multimedia (código existente)
	log.Printf("[SESSION] Multimedia event from %s", s.DeviceID)
}

// handleMultimediaData processa dados multimedia
func (s *JT808Session) handleMultimediaData(frame *protocol.PacketFrame) {
	// Processar dados multimedia (código existente)
	log.Printf("[SESSION] Multimedia data from %s", s.DeviceID)
}

// handleGeneralResponse processa resposta geral
func (s *JT808Session) handleGeneralResponse(frame *protocol.PacketFrame) {
	log.Printf("[SESSION] General response from %s", s.DeviceID)
}

// handleMultimediaResponse processa resposta multimedia
func (s *JT808Session) handleMultimediaResponse(frame *protocol.PacketFrame) {
	log.Printf("[SESSION] Multimedia response from %s", s.DeviceID)
}

// cleanupStaleStreams remove streams inativos
func (s *JT808Session) cleanupStaleStreams() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if s.VideoFrameHandler != nil {
			s.VideoFrameHandler.CleanupStaleStreams(120 * time.Second)
		}
	}
}

// cleanup libera recursos
func (s *JT808Session) cleanup() {
	if s.StreamManager != nil {
		s.StreamManager.StopAll()
	}

	if s.Registry != nil {
		s.Registry.Unregister(s.DeviceID)
	}

	s.Conn.Close()
	s.Done <- true
}
```

## Passo 2: Modificar API HTTP

```go
package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"jt808-broker/internal/stream"
)

// HandleStartVideoStream inicia streaming de vídeo
func (api *API) HandleStartVideoStream(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device")
	if deviceID == "" {
		http.Error(w, "Missing device parameter", http.StatusBadRequest)
		return
	}

	// Obter sessão
	session, ok := api.Registry.Get(deviceID).(*stream.JT808Session)
	if !ok || session == nil {
		http.Error(w, fmt.Sprintf("Device %s not connected", deviceID), http.StatusNotFound)
		return
	}

	if session.VideoFrameHandler == nil {
		http.Error(w, "Video handler not initialized", http.StatusInternalServerError)
		return
	}

	// Iniciar stream
	channelID := uint8(0)
	if ch := r.URL.Query().Get("channel"); ch != "" {
		var c int
		fmt.Sscanf(ch, "%d", &c)
		channelID = uint8(c)
	}

	if err := session.VideoFrameHandler.HandleVideoStreamStart(deviceID, session.JT1078Parser, channelID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start stream: %v", err), http.StatusInternalServerError)
		return
	}

	// Responder
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "started",
		"device":  deviceID,
		"channel": channelID,
	})
}

// HandleStopVideoStream para streaming de vídeo
func (api *API) HandleStopVideoStream(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device")
	streamID := r.URL.Query().Get("stream")

	session, ok := api.Registry.Get(deviceID).(*stream.JT808Session)
	if !ok || session == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	if err := session.VideoFrameHandler.StopVideoStream(deviceID, streamID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

// HandleGetVideoStreams lista streams ativos
func (api *API) HandleGetVideoStreams(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device")

	session, ok := api.Registry.Get(deviceID).(*stream.JT808Session)
	if !ok || session == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	streams := session.VideoFrameHandler.GetAllStreams()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(streams)
}

// HandleGetStreamStats obtém estatísticas de stream
func (api *API) HandleGetStreamStats(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device")

	session, ok := api.Registry.Get(deviceID).(*stream.JT808Session)
	if !ok || session == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	stats := session.StreamManager.GetStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
```

## Passo 3: Registrar Rotas HTTP

```go
// No main.go ou api setup:
mux.HandleFunc("/api/video/start", api.HandleStartVideoStream)
mux.HandleFunc("/api/video/stop", api.HandleStopVideoStream)
mux.HandleFunc("/api/video/streams", api.HandleGetVideoStreams)
mux.HandleFunc("/api/video/stats", api.HandleGetStreamStats)
```

## Exemplos de Uso

### 1. Iniciar Streaming

```bash
curl "http://localhost:8080/api/video/start?device=000000000001&channel=0"

Response:
{
    "status": "started",
    "device": "000000000001",
    "channel": 0
}
```

### 2. Obter Status

```bash
curl "http://localhost:8080/api/video/streams?device=000000000001"

Response:
{
    "video_ch0": {
        "device_id": "000000000001",
        "stream_id": "video_ch0",
        "codec": "H.264",
        "resolution": "1920x1080",
        "frame_rate": 30,
        "status": "active",
        "total_frames": 450,
        "key_frames": 15,
        "bytes_received": 45000000,
        "duration": "15s",
        "fps": "30.00",
        "last_frame": "150ms"
    }
}
```

### 3. Obter Estatísticas

```bash
curl "http://localhost:8080/api/video/stats?device=000000000001"

Response:
{
    "video_ch0_20240124_143015": {
        "device_id": "000000000001",
        "stream_id": "video_ch0",
        "is_running": true,
        "total_frames": 450,
        "total_bytes": 45000000,
        "duration": "15s",
        "fps": "30.00",
        "throughput_mbps": "24.00"
    }
}
```

### 4. Parar Streaming

```bash
curl "http://localhost:8080/api/video/stop?device=000000000001&stream=video_ch0"

Response:
{
    "status": "stopped"
}
```

## Logs Esperados

```
[SESSION] Device login: 000000000001
[SESSION] Device registered: 000000000001
[VIDEO_HANDLER] Stream started - Device: 000000000001, Stream: video_ch0, Channel: 0
[VIDEO_HANDLER] First keyframe received - Stream ready
[VIDEO] I-Frame received
  Codec: H.264, Resolution: 1920x1080, FPS: 30
  Sequence: 1, Size: 51234 bytes
  Timestamp: 2700000 (90kHz), PTS: 30000ms
[VIDEO_HANDLER] I-Frame received - Seq: 1, Size: 51234 bytes, Res: 1920x1080
[STREAM] Frame rate: 30 FPS, Bytes: 45000000, Duration: 15s
[AUDIO] Frame: AAC, 16000Hz, 16-bit, 2 channels, Size: 1024 bytes
[VIDEO_HANDLER] Stream stopped - Device: 000000000001, Stream: video_ch0
  Frames: 450 (KeyFrames: 15), Bytes: 45000000, Duration: 15s, FPS: 30.00
[SESSION] Device logout: 000000000001
```

