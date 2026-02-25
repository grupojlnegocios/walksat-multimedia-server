package stream

import (
	"log"
	"net"
	"sync"
)

// DeviceRegistry tracks connected devices
type DeviceRegistry struct {
	devices map[string]*JT808Session
	mutex   sync.RWMutex
}

func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{
		devices: make(map[string]*JT808Session),
	}
}

// Register adds a device session
func (r *DeviceRegistry) Register(deviceID string, session *JT808Session) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.devices[deviceID] = session
	log.Printf("[REGISTRY] Device registered: %s from %s\n", deviceID, session.Conn.RemoteAddr())
}

// Unregister removes a device session
func (r *DeviceRegistry) Unregister(deviceID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.devices, deviceID)
	log.Printf("[REGISTRY] Device unregistered: %s\n", deviceID)
}

// Get retrieves a device session
func (r *DeviceRegistry) Get(deviceID string) (*JT808Session, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	session, ok := r.devices[deviceID]
	return session, ok
}

// GetAll returns all connected devices
func (r *DeviceRegistry) GetAll() map[string]*JT808Session {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	result := make(map[string]*JT808Session)
	for k, v := range r.devices {
		result[k] = v
	}
	return result
}

// Count returns number of connected devices
func (r *DeviceRegistry) Count() int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return len(r.devices)
}

// SendCommand sends a command to a specific device
func (r *DeviceRegistry) SendCommand(deviceID string, data []byte) error {
	session, ok := r.Get(deviceID)
	if !ok {
		return net.ErrClosed
	}

	_, err := session.Conn.Write(data)
	if err != nil {
		log.Printf("[REGISTRY] Error sending command to device %s: %v\n", deviceID, err)
		return err
	}

	log.Printf("[REGISTRY] Command sent to device %s: %d bytes\n", deviceID, len(data))
	log.Printf("[REGISTRY] Command hex: % X\n", data)
	return nil
}
