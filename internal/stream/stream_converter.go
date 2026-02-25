package stream

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"jt808-broker/internal/protocol"
)

// ============================================================================
// Stream Buffer - Buffer de frames para conversão
// ============================================================================

// FrameBuffer acumula frames de um stream para processamento
type FrameBuffer struct {
	VideoFrames     []*protocol.VideoFrame
	AudioFrames     []*protocol.AudioFrame
	MaxFrames       int
	FlushInterval   time.Duration
	LastFlushTime   time.Time
	mutex           sync.RWMutex
	onFlushCallback func(vFrames []*protocol.VideoFrame, aFrames []*protocol.AudioFrame)
	stopped         bool
}

// NewFrameBuffer cria um novo buffer de frames
func NewFrameBuffer(maxFrames int, flushInterval time.Duration) *FrameBuffer {
	fb := &FrameBuffer{
		VideoFrames:   make([]*protocol.VideoFrame, 0, maxFrames),
		AudioFrames:   make([]*protocol.AudioFrame, 0, maxFrames),
		MaxFrames:     maxFrames,
		FlushInterval: flushInterval,
		LastFlushTime: time.Now(),
	}

	// Goroutine para flush periódico
	go fb.periodicFlush()

	return fb
}

// AddVideoFrame adiciona frame de vídeo ao buffer
func (fb *FrameBuffer) AddVideoFrame(frame *protocol.VideoFrame) error {
	fb.mutex.Lock()
	defer fb.mutex.Unlock()

	if fb.stopped {
		return fmt.Errorf("frame buffer stopped")
	}

	fb.VideoFrames = append(fb.VideoFrames, frame)

	// Flush se atingiu max de frames ou tem I-frame
	if len(fb.VideoFrames) >= fb.MaxFrames || frame.IsKeyFrame() {
		fb.flush()
	}

	return nil
}

// AddAudioFrame adiciona frame de áudio ao buffer
func (fb *FrameBuffer) AddAudioFrame(frame *protocol.AudioFrame) error {
	fb.mutex.Lock()
	defer fb.mutex.Unlock()

	if fb.stopped {
		return fmt.Errorf("frame buffer stopped")
	}

	fb.AudioFrames = append(fb.AudioFrames, frame)

	// Flush se atingiu max de frames
	if len(fb.AudioFrames) >= fb.MaxFrames {
		fb.flush()
	}

	return nil
}

// SetOnFlushCallback define callback quando buffer é despejado
func (fb *FrameBuffer) SetOnFlushCallback(callback func([]*protocol.VideoFrame, []*protocol.AudioFrame)) {
	fb.mutex.Lock()
	defer fb.mutex.Unlock()
	fb.onFlushCallback = callback
}

// flush despeja buffer sem lock (chama com lock já adquirido)
func (fb *FrameBuffer) flush() {
	if len(fb.VideoFrames) == 0 && len(fb.AudioFrames) == 0 {
		return
	}

	if fb.onFlushCallback != nil {
		vFrames := make([]*protocol.VideoFrame, len(fb.VideoFrames))
		copy(vFrames, fb.VideoFrames)

		aFrames := make([]*protocol.AudioFrame, len(fb.AudioFrames))
		copy(aFrames, fb.AudioFrames)

		go fb.onFlushCallback(vFrames, aFrames)
	}

	fb.VideoFrames = make([]*protocol.VideoFrame, 0, fb.MaxFrames)
	fb.AudioFrames = make([]*protocol.AudioFrame, 0, fb.MaxFrames)
	fb.LastFlushTime = time.Now()
}

// Flush despeja o buffer manualmente
func (fb *FrameBuffer) Flush() {
	fb.mutex.Lock()
	defer fb.mutex.Unlock()
	fb.flush()
}

// periodicFlush despeja periodicamente
func (fb *FrameBuffer) periodicFlush() {
	ticker := time.NewTicker(fb.FlushInterval)
	defer ticker.Stop()

	for range ticker.C {
		fb.Flush()
	}
}

// Stop para o buffer
func (fb *FrameBuffer) Stop() {
	fb.mutex.Lock()
	defer fb.mutex.Unlock()
	fb.stopped = true
	fb.flush()
}

// ============================================================================
// Stream Converter - Converte frames para arquivo/stream
// ============================================================================

// StreamConverter converte streams de vídeo/áudio
type StreamConverter struct {
	DeviceID     string
	StreamID     string
	MediaType    uint8 // 0=video, 1=audio, 2=mux
	VideoCodec   uint8
	AudioCodec   uint8
	OutputDir    string
	OutputFormat string // "ts", "mp4", "mkv", "flv"
	SessionStart time.Time
	SessionEnd   time.Time
	FrameBuffer  *FrameBuffer
	OutputFile   *os.File
	TotalFrames  int64
	TotalBytes   int64
	IsRunning    bool
	mutex        sync.RWMutex
}

// NewStreamConverter cria um novo conversor de stream
func NewStreamConverter(deviceID, streamID string, mediaType uint8, outputDir, outputFormat string) *StreamConverter {
	return &StreamConverter{
		DeviceID:     deviceID,
		StreamID:     streamID,
		MediaType:    mediaType,
		OutputDir:    outputDir,
		OutputFormat: outputFormat,
		SessionStart: time.Now(),
		FrameBuffer:  NewFrameBuffer(30, 2*time.Second),
		TotalFrames:  0,
		TotalBytes:   0,
		IsRunning:    false,
	}
}

// Start inicia a conversão
func (sc *StreamConverter) Start() error {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	if sc.IsRunning {
		return fmt.Errorf("converter already running")
	}

	// Criar diretório
	if err := os.MkdirAll(sc.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// Criar arquivo de saída
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s_%s.%s", sc.DeviceID, sc.StreamID, timestamp, sc.OutputFormat)
	filepath := filepath.Join(sc.OutputDir, filename)

	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}

	sc.OutputFile = file
	sc.IsRunning = true

	// Setup callback
	sc.FrameBuffer.SetOnFlushCallback(func(vFrames []*protocol.VideoFrame, aFrames []*protocol.AudioFrame) {
		sc.processFrames(vFrames, aFrames)
	})

	log.Printf("[STREAM] Converter started - Device: %s, Stream: %s, Output: %s",
		sc.DeviceID, sc.StreamID, filepath)

	return nil
}

// AddVideoFrame adiciona frame de vídeo
func (sc *StreamConverter) AddVideoFrame(frame *protocol.VideoFrame) error {
	if !sc.IsRunning {
		return fmt.Errorf("converter not running")
	}

	sc.TotalFrames++
	sc.TotalBytes += int64(len(frame.Data))

	return sc.FrameBuffer.AddVideoFrame(frame)
}

// AddAudioFrame adiciona frame de áudio
func (sc *StreamConverter) AddAudioFrame(frame *protocol.AudioFrame) error {
	if !sc.IsRunning {
		return fmt.Errorf("converter not running")
	}

	sc.TotalFrames++
	sc.TotalBytes += int64(len(frame.Data))

	return sc.FrameBuffer.AddAudioFrame(frame)
}

// processFrames processa lotes de frames
func (sc *StreamConverter) processFrames(vFrames []*protocol.VideoFrame, aFrames []*protocol.AudioFrame) {
	if len(vFrames) == 0 && len(aFrames) == 0 {
		return
	}

	// Escrever frames no arquivo (formato simplificado)
	// Em produção, usar FFmpeg para conversão real
	for _, vf := range vFrames {
		header := make([]byte, 4)
		header[0] = byte(len(vf.Data) >> 24)
		header[1] = byte(len(vf.Data) >> 16)
		header[2] = byte(len(vf.Data) >> 8)
		header[3] = byte(len(vf.Data))

		if _, err := sc.OutputFile.Write(header); err != nil {
			log.Printf("[STREAM] ERROR writing header: %v", err)
		}
		if _, err := sc.OutputFile.Write(vf.Data); err != nil {
			log.Printf("[STREAM] ERROR writing frame data: %v", err)
		}
	}

	for _, af := range aFrames {
		if _, err := sc.OutputFile.Write(af.Data); err != nil {
			log.Printf("[STREAM] ERROR writing audio data: %v", err)
		}
	}
}

// Stop para a conversão
func (sc *StreamConverter) Stop() error {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	if !sc.IsRunning {
		return fmt.Errorf("converter not running")
	}

	sc.FrameBuffer.Stop()

	if sc.OutputFile != nil {
		sc.OutputFile.Close()
	}

	sc.SessionEnd = time.Now()
	sc.IsRunning = false

	duration := sc.SessionEnd.Sub(sc.SessionStart)
	log.Printf("[STREAM] Converter stopped - Device: %s, Stream: %s, Frames: %d, Bytes: %d, Duration: %v",
		sc.DeviceID, sc.StreamID, sc.TotalFrames, sc.TotalBytes, duration)

	return nil
}

// GetStats retorna estatísticas do stream
func (sc *StreamConverter) GetStats() map[string]interface{} {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()

	duration := time.Since(sc.SessionStart)
	if !sc.SessionEnd.IsZero() {
		duration = sc.SessionEnd.Sub(sc.SessionStart)
	}

	fps := 0.0
	if duration.Seconds() > 0 {
		fps = float64(sc.TotalFrames) / duration.Seconds()
	}

	return map[string]interface{}{
		"device_id":       sc.DeviceID,
		"stream_id":       sc.StreamID,
		"is_running":      sc.IsRunning,
		"total_frames":    sc.TotalFrames,
		"total_bytes":     sc.TotalBytes,
		"duration":        duration.String(),
		"fps":             fmt.Sprintf("%.2f", fps),
		"throughput_mbps": fmt.Sprintf("%.2f", float64(sc.TotalBytes)*8/1e6/duration.Seconds()),
	}
}

// ============================================================================
// Multi-Stream Manager
// ============================================================================

// StreamManager gerencia múltiplos streams
type StreamManager struct {
	converters map[string]*StreamConverter
	mutex      sync.RWMutex
	outputDir  string
}

// NewStreamManager cria um novo gerenciador
func NewStreamManager(outputDir string) *StreamManager {
	return &StreamManager{
		converters: make(map[string]*StreamConverter),
		outputDir:  outputDir,
	}
}

// CreateConverter cria um novo conversor
func (sm *StreamManager) CreateConverter(deviceID, streamID string, mediaType uint8, format string) (*StreamConverter, error) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	key := fmt.Sprintf("%s_%s", deviceID, streamID)
	if _, exists := sm.converters[key]; exists {
		return nil, fmt.Errorf("converter already exists for %s", key)
	}

	deviceDir := filepath.Join(sm.outputDir, deviceID, "streams")
	sc := NewStreamConverter(deviceID, streamID, mediaType, deviceDir, format)

	if err := sc.Start(); err != nil {
		return nil, err
	}

	sm.converters[key] = sc
	return sc, nil
}

// GetConverter obtém um conversor
func (sm *StreamManager) GetConverter(deviceID, streamID string) (*StreamConverter, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	key := fmt.Sprintf("%s_%s", deviceID, streamID)
	sc, exists := sm.converters[key]
	return sc, exists
}

// StopConverter para um conversor
func (sm *StreamManager) StopConverter(deviceID, streamID string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	key := fmt.Sprintf("%s_%s", deviceID, streamID)
	sc, exists := sm.converters[key]
	if !exists {
		return fmt.Errorf("converter not found: %s", key)
	}

	err := sc.Stop()
	delete(sm.converters, key)
	return err
}

// GetAllConverters retorna todos os conversores
func (sm *StreamManager) GetAllConverters() map[string]*StreamConverter {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	result := make(map[string]*StreamConverter)
	for k, v := range sm.converters {
		result[k] = v
	}
	return result
}

// StopAll para todos os conversores
func (sm *StreamManager) StopAll() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for _, sc := range sm.converters {
		_ = sc.Stop()
	}
	sm.converters = make(map[string]*StreamConverter)
}

// GetStats retorna estatísticas de todos os streams
func (sm *StreamManager) GetStats() map[string]map[string]interface{} {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	result := make(map[string]map[string]interface{})
	for key, sc := range sm.converters {
		result[key] = sc.GetStats()
	}
	return result
}
