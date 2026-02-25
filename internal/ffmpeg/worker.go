package ffmpeg

import (
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Worker struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	DeviceID string
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

func NewWorker(root string, conn net.Conn) *Worker {
	id := sanitize(conn.RemoteAddr().String())
	log.Printf("[FFMPEG] Creating worker for device %s\n", id)

	dir := filepath.Join(root, id)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		log.Printf("[FFMPEG] Warning: failed to create directory %s: %v\n", dir, err)
	}
	log.Printf("[FFMPEG] Stream directory: %s\n", dir)

	out := filepath.Join(dir, "index.m3u8")

	cmd := exec.Command(
		"ffmpeg",
		"-fflags", "nobuffer",
		"-i", "pipe:0",
		"-c:v", "copy",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "6",
		"-hls_flags", "delete_segments",
		out,
	)

	log.Printf("[FFMPEG] Starting FFmpeg process for device %s\n", id)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("[FFMPEG] Error creating stdin pipe: %v\n", err)
	}

	err = cmd.Start()
	if err != nil {
		log.Printf("[FFMPEG] Error starting FFmpeg: %v\n", err)
	} else {
		log.Printf("[FFMPEG] FFmpeg process started successfully for device %s (PID: %d)\n", id, cmd.Process.Pid)
	}

	return &Worker{
		cmd:      cmd,
		stdin:    stdin,
		DeviceID: id,
	}
}

func (w *Worker) Write(b []byte) {
	n, err := w.stdin.Write(b)
	if err != nil {
		log.Printf("[FFMPEG] Error writing to FFmpeg stdin for device %s: %v\n", w.DeviceID, err)
	} else {
		log.Printf("[FFMPEG] Wrote %d bytes to FFmpeg for device %s\n", n, w.DeviceID)
	}
}
