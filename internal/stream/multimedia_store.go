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

// MultimediaStore manages multimedia file storage and retrieval
type MultimediaStore struct {
	rootDir       string
	activeUploads map[string]*MultimediaUpload
	uploadsMutex  sync.RWMutex
}

// MultimediaUpload tracks an ongoing multimedia upload
type MultimediaUpload struct {
	MultimediaID uint32
	DeviceID     string
	MediaType    byte
	MediaFormat  byte
	EventCode    byte
	ChannelID    byte
	StartTime    time.Time
	Packets      map[uint16][]byte
	TotalSize    int
	FilePath     string
}

func NewMultimediaStore(rootDir string) *MultimediaStore {
	return &MultimediaStore{
		rootDir:       rootDir,
		activeUploads: make(map[string]*MultimediaUpload),
	}
}

// StartUpload initiates a new multimedia upload
func (m *MultimediaStore) StartUpload(deviceID string, multimediaID uint32, mediaType, mediaFormat, eventCode, channelID byte) *MultimediaUpload {
	m.uploadsMutex.Lock()
	defer m.uploadsMutex.Unlock()

	key := fmt.Sprintf("%s_%d", deviceID, multimediaID)

	upload := &MultimediaUpload{
		MultimediaID: multimediaID,
		DeviceID:     deviceID,
		MediaType:    mediaType,
		MediaFormat:  mediaFormat,
		EventCode:    eventCode,
		ChannelID:    channelID,
		StartTime:    time.Now(),
		Packets:      make(map[uint16][]byte),
		TotalSize:    0,
	}

	// Create file path
	deviceDir := filepath.Join(m.rootDir, deviceID, "multimedia")
	os.MkdirAll(deviceDir, 0755)

	ext := protocol.GetFileExtension(mediaType, mediaFormat)
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_ch%d_id%d%s", timestamp, channelID, multimediaID, ext)
	upload.FilePath = filepath.Join(deviceDir, filename)

	m.activeUploads[key] = upload

	log.Printf("[MULTIMEDIA_STORE] Started upload - Device: %s, ID: %d, Type: %s, File: %s\n",
		deviceID, multimediaID, protocol.GetMediaTypeName(mediaType), upload.FilePath)

	return upload
}

// AddDataPacket adds a data packet to an ongoing upload
func (m *MultimediaStore) AddDataPacket(deviceID string, multimediaID uint32, packetID uint16, data []byte) error {
	m.uploadsMutex.Lock()
	defer m.uploadsMutex.Unlock()

	key := fmt.Sprintf("%s_%d", deviceID, multimediaID)
	upload, ok := m.activeUploads[key]
	if !ok {
		return fmt.Errorf("no active upload found for device %s, multimedia ID %d", deviceID, multimediaID)
	}

	upload.Packets[packetID] = data
	upload.TotalSize += len(data)

	log.Printf("[MULTIMEDIA_STORE] Added packet %d (%d bytes) to upload %s_%d (total: %d bytes, packets: %d)\n",
		packetID, len(data), deviceID, multimediaID, upload.TotalSize, len(upload.Packets))

	return nil
}

// CompleteUpload finalizes and saves the multimedia file
func (m *MultimediaStore) CompleteUpload(deviceID string, multimediaID uint32, expectedPackets uint16) (string, error) {
	m.uploadsMutex.Lock()
	defer m.uploadsMutex.Unlock()

	key := fmt.Sprintf("%s_%d", deviceID, multimediaID)
	upload, ok := m.activeUploads[key]
	if !ok {
		return "", fmt.Errorf("no active upload found for device %s, multimedia ID %d", deviceID, multimediaID)
	}

	// Check if all packets are received
	missingPackets := []uint16{}
	if expectedPackets > 0 {
		for i := uint16(0); i < expectedPackets; i++ {
			if _, ok := upload.Packets[i]; !ok {
				missingPackets = append(missingPackets, i)
			}
		}
	}

	if len(missingPackets) > 0 {
		log.Printf("[MULTIMEDIA_STORE] Upload incomplete - missing %d packets: %v\n", len(missingPackets), missingPackets)
		return "", fmt.Errorf("missing %d packets", len(missingPackets))
	}

	// Write file
	file, err := os.Create(upload.FilePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %v", err)
	}
	defer file.Close()

	// Write packets in order
	totalWritten := 0
	for i := uint16(0); i < uint16(len(upload.Packets)); i++ {
		if data, ok := upload.Packets[i]; ok {
			n, err := file.Write(data)
			if err != nil {
				return "", fmt.Errorf("failed to write packet %d: %v", i, err)
			}
			totalWritten += n
		}
	}

	duration := time.Since(upload.StartTime)
	log.Printf("[MULTIMEDIA_STORE] Upload completed - Device: %s, ID: %d, File: %s, Size: %d bytes, Duration: %v\n",
		deviceID, multimediaID, upload.FilePath, totalWritten, duration)

	// Clean up
	delete(m.activeUploads, key)

	return upload.FilePath, nil
}

// GetUpload retrieves an active upload
func (m *MultimediaStore) GetUpload(deviceID string, multimediaID uint32) (*MultimediaUpload, bool) {
	m.uploadsMutex.RLock()
	defer m.uploadsMutex.RUnlock()

	key := fmt.Sprintf("%s_%d", deviceID, multimediaID)
	upload, ok := m.activeUploads[key]
	return upload, ok
}

// GetMissingPackets returns list of missing packet IDs
func (m *MultimediaStore) GetMissingPackets(deviceID string, multimediaID uint32, expectedTotal uint16) []uint16 {
	m.uploadsMutex.RLock()
	defer m.uploadsMutex.RUnlock()

	key := fmt.Sprintf("%s_%d", deviceID, multimediaID)
	upload, ok := m.activeUploads[key]
	if !ok {
		return nil
	}

	missing := []uint16{}
	for i := uint16(0); i < expectedTotal; i++ {
		if _, ok := upload.Packets[i]; !ok {
			missing = append(missing, i)
		}
	}

	return missing
}

// CleanupStaleUploads removes uploads that haven't been updated in a while
func (m *MultimediaStore) CleanupStaleUploads(maxAge time.Duration) {
	m.uploadsMutex.Lock()
	defer m.uploadsMutex.Unlock()

	now := time.Now()
	for key, upload := range m.activeUploads {
		if now.Sub(upload.StartTime) > maxAge {
			log.Printf("[MULTIMEDIA_STORE] Cleaning up stale upload: %s (age: %v)\n", key, now.Sub(upload.StartTime))
			delete(m.activeUploads, key)
		}
	}
}
