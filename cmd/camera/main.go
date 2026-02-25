package main

import (
	"flag"
	"fmt"
	"net"

	"jt808-broker/internal/protocol"
)

func main() {
	deviceID := flag.String("device", "000000000000", "Device ID (12 hex digits)")
	channel := flag.Int("channel", 1, "Camera channel ID")
	shots := flag.Int("shots", 1, "Number of photos (1-10, or 0xFFFF for video)")
	interval := flag.Int("interval", 0, "Interval between shots in seconds")
	resolution := flag.Int("res", 4, "Resolution (1=320x240, 2=640x480, 3=800x600, 4=1024x768)")
	quality := flag.Int("quality", 5, "Quality (1-10, 1=best)")
	host := flag.String("host", "localhost:6207", "JT808 server address")

	flag.Parse()

	fmt.Printf("Sending camera command to device %s on channel %d\n", *deviceID, *channel)
	fmt.Printf("Photos: %d, Interval: %ds, Resolution: %d, Quality: %d\n", *shots, *interval, *resolution, *quality)

	// Build command
	// BuildCameraCommandImmediate(deviceID, seqNum, channelID, shotCount, shotInterval, saveFlag, resolution, quality)
	cmd, err := protocol.BuildCameraCommandImmediate(
		*deviceID,
		1, // sequence number
		byte(*channel),
		uint16(*shots),
		uint16(*interval),
		0, // Upload immediately
		byte(*resolution),
		byte(*quality),
	)
	if err != nil {
		fmt.Printf("Error building command: %v\n", err)
		return
	}

	// Connect to server
	conn, err := net.Dial("tcp", *host)
	if err != nil {
		fmt.Printf("Error connecting: %v\n", err)
		return
	}
	defer conn.Close()

	// Send command
	n, err := conn.Write(cmd)
	if err != nil {
		fmt.Printf("Error sending command: %v\n", err)
		return
	}

	fmt.Printf("Command sent: %d bytes\n", n)
	fmt.Printf("Waiting for response...\n")

	// Read response
	buf := make([]byte, 1024)
	n, err = conn.Read(buf)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		return
	}

	fmt.Printf("Response received: %d bytes\n", n)
	fmt.Printf("Raw response: % X\n", buf[:n])
}
