package stream

import (
	"bytes"
	"fmt"
	"log"
	"sync"
	"time"

	"jt808-broker/internal/protocol"
)

// ============================================================================
// Stream Initialization Buffer
// Bufferiza frames até ter SPS+PPS+IDR completo antes de enviar ao FFmpeg
// ============================================================================

// StreamInitState represents the initialization state
type StreamInitState int

const (
	StateWaitingSPS StreamInitState = iota // Waiting for SPS
	StateWaitingPPS                        // Has SPS, waiting for PPS
	StateWaitingIDR                        // Has SPS+PPS, waiting for IDR
	StateReady                             // Has SPS+PPS+IDR, ready to stream
)

// StreamInitBuffer buffers frames until complete initialization sequence
type StreamInitBuffer struct {
	DeviceID string
	Channel  uint8

	// State
	state         StreamInitState
	mutex         sync.RWMutex
	initialized   bool
	readyTime     time.Time
	startTime     time.Time
	bytesBuffered int64

	// NAL detector
	detector *protocol.NALDetector

	// Cached parameter sets
	sps []byte // Cached SPS with start code
	pps []byte // Cached PPS with start code

	// Buffered frames waiting for initialization
	pendingFrames [][]byte

	// Configuration
	maxPendingFrames int           // Max frames to buffer
	maxBufferSize    int64         // Max bytes to buffer
	timeout          time.Duration // Timeout for initialization

	// Callbacks
	onReady      func(sps, pps []byte) // Called when stream is ready
	onFrameReady func(data []byte)     // Called for each ready frame
}

// NewStreamInitBuffer creates a new initialization buffer
func NewStreamInitBuffer(deviceID string, channel uint8) *StreamInitBuffer {
	return &StreamInitBuffer{
		DeviceID:         deviceID,
		Channel:          channel,
		state:            StateWaitingSPS,
		detector:         protocol.NewNALDetector(),
		pendingFrames:    make([][]byte, 0, 100),
		maxPendingFrames: 100,
		maxBufferSize:    50 * 1024 * 1024, // 50MB
		timeout:          30 * time.Second,
		startTime:        time.Now(),
	}
}

// SetOnReady sets callback for when stream is ready
func (sib *StreamInitBuffer) SetOnReady(callback func(sps, pps []byte)) {
	sib.onReady = callback
}

// SetOnFrameReady sets callback for ready frames
func (sib *StreamInitBuffer) SetOnFrameReady(callback func(data []byte)) {
	sib.onFrameReady = callback
}

// AddFrame adds a frame to the buffer
// Returns true if frame was processed and stream is ready
func (sib *StreamInitBuffer) AddFrame(frameData []byte) (bool, error) {
	sib.mutex.Lock()
	defer sib.mutex.Unlock()

	// Check timeout
	if !sib.initialized && time.Since(sib.startTime) > sib.timeout {
		return false, fmt.Errorf("initialization timeout after %v", sib.timeout)
	}

	// If already initialized, pass through directly
	if sib.initialized {
		if sib.onFrameReady != nil {
			// Prepend SPS+PPS to every keyframe
			if sib.isKeyFrame(frameData) {
				frameData = sib.prependSPSPPS(frameData)
			}
			sib.onFrameReady(frameData)
		}
		return true, nil
	}

	// Check buffer limits
	if len(sib.pendingFrames) >= sib.maxPendingFrames {
		return false, fmt.Errorf("pending frame buffer full (%d frames)", sib.maxPendingFrames)
	}
	if sib.bytesBuffered+int64(len(frameData)) > sib.maxBufferSize {
		return false, fmt.Errorf("buffer size exceeded (%d bytes)", sib.bytesBuffered)
	}

	// Analyze frame
	info := sib.detector.AnalyzeStream(frameData)

	log.Printf("[INIT_BUFFER] Frame analysis: SPS=%v PPS=%v IDR=%v State=%s\n",
		info.HasSPS, info.HasPPS, info.HasIDR, sib.getStateName())

	// Update state based on what we found
	if info.HasSPS && sib.sps == nil {
		sib.extractAndCacheSPS(frameData)
		if sib.state == StateWaitingSPS {
			sib.state = StateWaitingPPS
			log.Printf("[INIT_BUFFER] ✓ State: Waiting SPS → Waiting PPS\n")
		}
	}

	if info.HasPPS && sib.pps == nil {
		sib.extractAndCachePPS(frameData)
		if sib.state == StateWaitingPPS {
			sib.state = StateWaitingIDR
			log.Printf("[INIT_BUFFER] ✓ State: Waiting PPS → Waiting IDR\n")
		}
	}

	if info.HasIDR && sib.state == StateWaitingIDR {
		sib.state = StateReady
		log.Printf("[INIT_BUFFER] ✓ State: Waiting IDR → READY\n")
	}

	// Buffer frame
	sib.pendingFrames = append(sib.pendingFrames, frameData)
	sib.bytesBuffered += int64(len(frameData))

	// Check if we're ready to initialize
	if sib.state == StateReady && !sib.initialized {
		return sib.initialize()
	}

	return false, nil
}

// initialize completes the initialization and flushes buffered frames
func (sib *StreamInitBuffer) initialize() (bool, error) {
	if sib.sps == nil || sib.pps == nil {
		return false, fmt.Errorf("missing SPS or PPS for initialization")
	}

	log.Printf("[INIT_BUFFER] ========================================\n")
	log.Printf("[INIT_BUFFER] STREAM INITIALIZATION COMPLETE\n")
	log.Printf("[INIT_BUFFER] Device: %s, Channel: %d\n", sib.DeviceID, sib.Channel)
	log.Printf("[INIT_BUFFER] SPS: %d bytes, PPS: %d bytes\n", len(sib.sps), len(sib.pps))
	log.Printf("[INIT_BUFFER] Buffered frames: %d (%d bytes)\n", len(sib.pendingFrames), sib.bytesBuffered)
	log.Printf("[INIT_BUFFER] Init time: %v\n", time.Since(sib.startTime))
	log.Printf("[INIT_BUFFER] ========================================\n")

	sib.initialized = true
	sib.readyTime = time.Now()

	// Call ready callback
	if sib.onReady != nil {
		sib.onReady(sib.sps, sib.pps)
	}

	// Flush all pending frames
	if sib.onFrameReady != nil {
		// First send SPS+PPS
		initSequence := append(sib.sps, sib.pps...)
		sib.onFrameReady(initSequence)
		log.Printf("[INIT_BUFFER] → Sent SPS+PPS initialization sequence (%d bytes)\n", len(initSequence))

		// Then send all buffered frames
		for i, frame := range sib.pendingFrames {
			sib.onFrameReady(frame)
			log.Printf("[INIT_BUFFER] → Flushed buffered frame %d/%d (%d bytes)\n",
				i+1, len(sib.pendingFrames), len(frame))
		}
	}

	// Clear buffer
	sib.pendingFrames = nil
	sib.bytesBuffered = 0

	return true, nil
}

// extractAndCacheSPS extracts and caches SPS from frame data
func (sib *StreamInitBuffer) extractAndCacheSPS(data []byte) {
	units := sib.detector.ExtractNALUnits(data)
	for _, unit := range units {
		if unit.Type == protocol.NALUnitTypeSPS {
			// Store complete NAL unit with start code
			sib.sps = make([]byte, len(unit.StartCode)+len(unit.Data))
			copy(sib.sps, unit.StartCode)
			copy(sib.sps[len(unit.StartCode):], unit.Data)
			log.Printf("[INIT_BUFFER] ✓ Cached SPS: %d bytes (header: %02X)\n",
				len(sib.sps), unit.Data[0])
			break
		}
	}
}

// extractAndCachePPS extracts and caches PPS from frame data
func (sib *StreamInitBuffer) extractAndCachePPS(data []byte) {
	units := sib.detector.ExtractNALUnits(data)
	for _, unit := range units {
		if unit.Type == protocol.NALUnitTypePPS {
			// Store complete NAL unit with start code
			sib.pps = make([]byte, len(unit.StartCode)+len(unit.Data))
			copy(sib.pps, unit.StartCode)
			copy(sib.pps[len(unit.StartCode):], unit.Data)
			log.Printf("[INIT_BUFFER] ✓ Cached PPS: %d bytes (header: %02X)\n",
				len(sib.pps), unit.Data[0])
			break
		}
	}
}

// prependSPSPPS prepends SPS+PPS to frame data
func (sib *StreamInitBuffer) prependSPSPPS(data []byte) []byte {
	if sib.sps == nil || sib.pps == nil {
		return data
	}

	// Check if frame already starts with SPS
	if protocol.ContainsSPSPPS(data) {
		return data
	}

	result := make([]byte, 0, len(sib.sps)+len(sib.pps)+len(data))
	result = append(result, sib.sps...)
	result = append(result, sib.pps...)
	result = append(result, data...)

	return result
}

// isKeyFrame checks if frame contains IDR
func (sib *StreamInitBuffer) isKeyFrame(data []byte) bool {
	units := sib.detector.ExtractNALUnits(data)
	for _, unit := range units {
		if unit.Type == protocol.NALUnitTypeCodedSliceIDR {
			return true
		}
	}
	return false
}

// IsInitialized returns whether stream is initialized
func (sib *StreamInitBuffer) IsInitialized() bool {
	sib.mutex.RLock()
	defer sib.mutex.RUnlock()
	return sib.initialized
}

// GetState returns current state
func (sib *StreamInitBuffer) GetState() StreamInitState {
	sib.mutex.RLock()
	defer sib.mutex.RUnlock()
	return sib.state
}

// GetSPS returns cached SPS
func (sib *StreamInitBuffer) GetSPS() []byte {
	sib.mutex.RLock()
	defer sib.mutex.RUnlock()
	if sib.sps == nil {
		return nil
	}
	result := make([]byte, len(sib.sps))
	copy(result, sib.sps)
	return result
}

// GetPPS returns cached PPS
func (sib *StreamInitBuffer) GetPPS() []byte {
	sib.mutex.RLock()
	defer sib.mutex.RUnlock()
	if sib.pps == nil {
		return nil
	}
	result := make([]byte, len(sib.pps))
	copy(result, sib.pps)
	return result
}

// GetStats returns buffer statistics
func (sib *StreamInitBuffer) GetStats() map[string]interface{} {
	sib.mutex.RLock()
	defer sib.mutex.RUnlock()

	return map[string]interface{}{
		"device_id":        sib.DeviceID,
		"channel":          sib.Channel,
		"state":            sib.getStateName(),
		"initialized":      sib.initialized,
		"has_sps":          sib.sps != nil,
		"has_pps":          sib.pps != nil,
		"pending_frames":   len(sib.pendingFrames),
		"bytes_buffered":   sib.bytesBuffered,
		"time_since_start": time.Since(sib.startTime).String(),
	}
}

// getStateName returns human-readable state name
func (sib *StreamInitBuffer) getStateName() string {
	switch sib.state {
	case StateWaitingSPS:
		return "Waiting for SPS"
	case StateWaitingPPS:
		return "Waiting for PPS"
	case StateWaitingIDR:
		return "Waiting for IDR"
	case StateReady:
		return "Ready"
	default:
		return "Unknown"
	}
}

// Reset resets the buffer state
func (sib *StreamInitBuffer) Reset() {
	sib.mutex.Lock()
	defer sib.mutex.Unlock()

	sib.state = StateWaitingSPS
	sib.initialized = false
	sib.sps = nil
	sib.pps = nil
	sib.pendingFrames = make([][]byte, 0, 100)
	sib.bytesBuffered = 0
	sib.detector.Reset()
	sib.startTime = time.Now()

	log.Printf("[INIT_BUFFER] Buffer reset\n")
}

// BuildInitializationSequence builds SPS+PPS+IDR sequence from buffer
func (sib *StreamInitBuffer) BuildInitializationSequence() ([]byte, error) {
	sib.mutex.RLock()
	defer sib.mutex.RUnlock()

	if !sib.initialized {
		return nil, fmt.Errorf("stream not initialized")
	}

	var buf bytes.Buffer

	// Write SPS
	if sib.sps != nil {
		buf.Write(sib.sps)
	}

	// Write PPS
	if sib.pps != nil {
		buf.Write(sib.pps)
	}

	// Find and write first IDR from buffered frames
	for _, frame := range sib.pendingFrames {
		if sib.isKeyFrame(frame) {
			buf.Write(frame)
			break
		}
	}

	if buf.Len() == 0 {
		return nil, fmt.Errorf("no initialization data available")
	}

	return buf.Bytes(), nil
}
