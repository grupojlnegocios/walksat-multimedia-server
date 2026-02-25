package http

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"jt808-broker/internal/protocol"
	"jt808-broker/internal/stream"
)

type API struct {
	router *stream.Router
}

func NewAPI(router *stream.Router) *API {
	return &API{router: router}
}

func (api *API) servePlayer(w http.ResponseWriter, r *http.Request) {
	// Only serve root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Get filename from query parameter if provided
	filename := r.URL.Query().Get("file")

	// Serve the HTML player page
	playerHTML := `<!DOCTYPE html>
<html lang="pt-BR">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>JT808 Video Stream Player</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            background: #1a1a1a;
            color: #fff;
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
        }
        
        .container {
            max-width: 1400px;
            margin: 0 auto;
            padding: 20px;
        }
        
        header {
            text-align: center;
            margin-bottom: 30px;
            padding-bottom: 20px;
            border-bottom: 2px solid #333;
        }
        
        h1 {
            font-size: 28px;
            margin-bottom: 10px;
        }
        
        .info {
            color: #aaa;
            font-size: 14px;
        }
        
        .main-content {
            display: grid;
            grid-template-columns: 1fr 300px;
            gap: 20px;
            margin-bottom: 30px;
        }
        
        .video-section {
            background: #222;
            border-radius: 8px;
            overflow: hidden;
            border: 1px solid #333;
        }
        
        .video-container {
            background: #000;
            position: relative;
            padding-bottom: 56.25%;
            height: 0;
            overflow: hidden;
        }
        
        video {
            position: absolute;
            top: 0;
            left: 0;
            width: 100%;
            height: 100%;
        }
        
        .video-controls {
            padding: 15px;
            background: #1a1a1a;
            border-top: 1px solid #333;
        }
        
        .control-group {
            margin-bottom: 10px;
        }
        
        .control-group label {
            display: block;
            font-size: 12px;
            color: #aaa;
            margin-bottom: 5px;
        }
        
        button, select, input[type="text"] {
            width: 100%;
            padding: 8px 12px;
            border: 1px solid #444;
            background: #333;
            color: #fff;
            border-radius: 4px;
            cursor: pointer;
            font-size: 13px;
            transition: all 0.3s ease;
        }
        
        button:hover {
            background: #444;
            border-color: #555;
        }
        
        button.primary {
            background: #4CAF50;
            color: white;
            border-color: #45a049;
        }
        
        button.primary:hover {
            background: #45a049;
        }
        
        button.secondary {
            background: #2196F3;
            border-color: #0b7dda;
        }
        
        button.secondary:hover {
            background: #0b7dda;
        }
        
        .sidebar {
            background: #222;
            border-radius: 8px;
            padding: 20px;
            border: 1px solid #333;
            height: fit-content;
        }
        
        .sidebar h2 {
            font-size: 16px;
            margin-bottom: 15px;
            border-bottom: 1px solid #333;
            padding-bottom: 10px;
        }
        
        .streams-list {
            max-height: 500px;
            overflow-y: auto;
            display: flex;
            flex-direction: column;
            gap: 8px;
        }
        
        .stream-item {
            background: #333;
            padding: 10px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 12px;
            border: 1px solid #444;
            transition: all 0.2s;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        
        .stream-item:hover {
            background: #444;
            border-color: #4CAF50;
        }
        
        .stream-item.active {
            background: #4CAF50;
            color: white;
            border-color: #45a049;
        }
        
        .device-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
            gap: 20px;
            margin-top: 30px;
        }
        
        .device-card {
            background: #222;
            border-radius: 8px;
            overflow: hidden;
            border: 1px solid #333;
        }
        
        .device-video {
            background: #000;
            position: relative;
            padding-bottom: 56.25%;
            height: 0;
            overflow: hidden;
        }
        
        .device-video video {
            position: absolute;
            top: 0;
            left: 0;
            width: 100%;
            height: 100%;
        }
        
        .device-info {
            padding: 15px;
            border-top: 1px solid #333;
        }
        
        .device-id {
            font-size: 14px;
            font-weight: bold;
            color: #4CAF50;
            margin-bottom: 8px;
        }
        
        .device-status {
            font-size: 11px;
            color: #aaa;
        }
        
        .loading, .error {
            display: none;
            text-align: center;
            padding: 20px;
            color: #aaa;
        }
        
        .loading.show, .error.show {
            display: block;
        }
        
        .spinner {
            display: inline-block;
            width: 20px;
            height: 20px;
            border: 3px solid #333;
            border-top-color: #4CAF50;
            border-radius: 50%;
            animation: spin 0.8s linear infinite;
            margin-right: 10px;
        }
        
        @keyframes spin {
            to { transform: rotate(360deg); }
        }
        
        .no-streams {
            text-align: center;
            padding: 40px 20px;
            color: #aaa;
        }
        
        .refresh-btn {
            background: #666 !important;
            margin-top: 10px !important;
        }
        
        .refresh-btn:hover {
            background: #777 !important;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>🎥 Video Stream Player</h1>
            <p class="info">Reproduza streams H.264 disponíveis no sistema</p>
        </header>

        <div class="main-content" id="mainContent">
            <div class="video-section">
                <div class="video-container">
                    <video id="mainVideo" controls playsinline></video>
                    <div class="loading show" id="mainLoading">
                        <div class="spinner"></div>
                        <span>Carregando stream...</span>
                    </div>
                </div>
                <div class="video-controls">
                    <div class="control-group">
                        <label>Stream Selecionado</label>
                        <input type="text" id="currentFile" placeholder="Nenhum stream selecionado" readonly>
                    </div>
                    <div class="control-group">
                        <button class="secondary" onclick="downloadCurrentStream()">⬇ Baixar Stream</button>
                    </div>
                    <div class="control-group">
                        <button class="primary" onclick="toggleFullscreen()">⛶ Tela Cheia</button>
                    </div>
                </div>
            </div>

            <div class="sidebar">
                <h2>Streams Disponíveis</h2>
                <div class="streams-list" id="streamsList">
                    <div class="loading show">
                        <div class="spinner"></div>
                        <span>Carregando...</span>
                    </div>
                </div>
                <button class="refresh-btn" onclick="loadStreams()">🔄 Atualizar</button>
            </div>
        </div>

        <div id="devicesSection" style="display: none;">
            <h2 style="margin-bottom: 20px;">📱 Dispositivos Conectados</h2>
            <div class="device-grid" id="devicesGrid">
            </div>
        </div>
    </div>

    <script>
        const mainVideo = document.getElementById('mainVideo');
        const mainLoading = document.getElementById('mainLoading');
        const streamsList = document.getElementById('streamsList');
        const currentFileInput = document.getElementById('currentFile');
        
        let currentFile = '` + filename + `';

        async function loadStreams() {
            try {
                const response = await fetch('/streams');
                const data = await response.json();

                if (!data.streams || data.streams.length === 0) {
                    streamsList.innerHTML = '<div class="no-streams">📡 Nenhum stream disponível</div>';
                    return;
                }

                streamsList.innerHTML = data.streams.map(stream => ` + "`" + `
                    <div class="stream-item ` + "`" + ` + (currentFile === stream.filename ? 'active' : '') + ` + "`" + `" 
                         onclick="selectStream('` + "`" + `${stream.filename}` + "`" + `', '` + "`" + `${stream.url}` + "`" + `')"
                         title="` + "`" + `${stream.filename}` + "`" + `">
                        📹 ` + "`" + `${stream.filename}` + "`" + `
                    </div>
                ` + "`" + `).join('');

                // Auto-load initial file if provided
                if (currentFile) {
                    const initialStream = data.streams.find(s => s.filename === currentFile);
                    if (initialStream) {
                        selectStream(currentFile, initialStream.url);
                    }
                }

            } catch (error) {
                streamsList.innerHTML = '<div class="error show">❌ Erro ao carregar streams</div>';
                console.error('Error:', error);
            }
        }

        function selectStream(filename, url) {
            currentFile = filename;
            currentFileInput.value = filename;

            // Update active state
            document.querySelectorAll('.stream-item').forEach(item => {
                item.classList.remove('active');
            });
            event.target.closest('.stream-item').classList.add('active');

            // Load video
            loadVideo(url);
        }

        function loadVideo(url) {
            mainLoading.classList.add('show');
            
            mainVideo.src = url;
            mainVideo.load();
            
            mainVideo.addEventListener('canplay', () => {
                mainLoading.classList.remove('show');
                mainVideo.play().catch(e => console.error('Play error:', e));
            }, { once: true });
            
            mainVideo.addEventListener('error', () => {
                mainLoading.classList.remove('show');
                console.error('Video error:', mainVideo.error);
            }, { once: true });
        }

        function downloadCurrentStream() {
            if (!currentFile) {
                alert('Selecione um stream primeiro');
                return;
            }
            const link = document.createElement('a');
            link.href = '/streams/' + currentFile;
            link.download = currentFile;
            link.click();
        }

        function toggleFullscreen() {
            if (mainVideo.requestFullscreen) {
                mainVideo.requestFullscreen();
            } else if (mainVideo.webkitRequestFullscreen) {
                mainVideo.webkitRequestFullscreen();
            }
        }

        // Load streams on startup
        loadStreams();
        
        // Refresh streams every 10 seconds
        setInterval(loadStreams, 10000);
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(playerHTML))
}

func (api *API) Start(addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/devices", api.listDevices)
	mux.HandleFunc("/device/", api.deviceCommand)
	mux.HandleFunc("/camera/capture", api.cameraCapture)
	mux.HandleFunc("/multimedia", api.listMultimedia)
	mux.HandleFunc("/multimedia/stream/", api.serveMultimedia)
	mux.HandleFunc("/live", api.listLiveStreams)
	mux.HandleFunc("/live/", api.serveLiveStream)
	mux.HandleFunc("/streams", api.listStreams)
	mux.HandleFunc("/streams/", api.serveStream)
	mux.HandleFunc("/", api.servePlayer)

	log.Printf("[HTTP_API] Starting API server on %s\n", addr)
	return http.ListenAndServe(addr, mux)
}

func (api *API) listDevices(w http.ResponseWriter, r *http.Request) {
	devices := api.router.DeviceRegistry.GetAll()

	result := make([]map[string]interface{}, 0)
	for deviceID, session := range devices {
		result = append(result, map[string]interface{}{
			"device_id": deviceID,
			"address":   session.Conn.RemoteAddr().String(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":   len(result),
		"devices": result,
	})
}

func (api *API) deviceCommand(w http.ResponseWriter, r *http.Request) {
	// Extract device ID from URL
	deviceID := r.URL.Path[len("/device/"):]
	if deviceID == "" {
		http.Error(w, "Device ID required", http.StatusBadRequest)
		return
	}

	_, ok := api.router.DeviceRegistry.Get(deviceID)
	if !ok {
		http.Error(w, "Device not connected", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"device_id": deviceID,
		"status":    "connected",
	})
}

func (api *API) cameraCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	// Parse parameters
	deviceID := r.URL.Query().Get("device")
	if deviceID == "" {
		http.Error(w, "device parameter required", http.StatusBadRequest)
		return
	}

	channel, _ := strconv.Atoi(r.URL.Query().Get("channel"))
	if channel == 0 {
		channel = 1
	}

	shots, _ := strconv.Atoi(r.URL.Query().Get("shots"))
	if shots == 0 {
		shots = 1
	}

	interval, _ := strconv.Atoi(r.URL.Query().Get("interval"))
	resolution, _ := strconv.Atoi(r.URL.Query().Get("resolution"))
	if resolution == 0 {
		resolution = 4 // 1024x768
	}

	quality, _ := strconv.Atoi(r.URL.Query().Get("quality"))
	if quality == 0 {
		quality = 5
	}

	// Check if device is connected
	_, ok := api.router.DeviceRegistry.Get(deviceID)
	if !ok {
		http.Error(w, "Device not connected", http.StatusNotFound)
		return
	}

	// Determine if this is a live stream request (shots=65535) or snapshot/video capture
	isLiveStream := shots == 65535

	// Note: Device connects anonymously without registration (0x0100)
	// Send commands directly without checking registration state

	var cmd []byte
	var cmdType string
	var err error

	if isLiveStream {
		// Live streaming: send 0x9101 (Real-time video request)
		log.Printf("[HTTP_API] Building live stream request (0x9101) for device %s\n", deviceID)

		// Use public IP for internet-accessible streaming
		publicIP := "201.6.109.131" // Public IP for devices to connect via internet
		mediaPort := api.router.MediaPort

		log.Printf("[HTTP_API] Media server (public): %s:%d\n", publicIP, mediaPort)

		// BuildRealTimeVideoRequest(deviceID, seqNum, serverIP, tcpPort, udpPort, channelID, dataType, streamType)
		// dataType: 0=audio+video, 1=video, 2=bidirectional, 3=audio monitoring, 4=center broadcast
		// streamType: 0=main stream, 1=sub stream
		cmd, err = protocol.BuildRealTimeVideoRequest(
			deviceID,
			1,             // sequence number
			publicIP,      // server IP (public internet IP)
			mediaPort,     // TCP port
			0,             // UDP port (0 = TCP only)
			byte(channel), // channel ID
			1,             // data type: 1 = video only
			0,             // stream type: 0 = main stream
		)
		cmdType = "live_stream_0x9101"
	} else {
		// Snapshot/video capture: send 0x8801 (Camera immediate command)
		log.Printf("[HTTP_API] Building camera capture request (0x8801) for device %s\n", deviceID)

		cmd, err = protocol.BuildCameraCommandImmediate(
			deviceID,
			1, // sequence number
			byte(channel),
			uint16(shots),
			uint16(interval),
			0, // Upload immediately
			byte(resolution),
			byte(quality),
		)
		cmdType = "camera_capture_0x8801"
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Error building command: %v", err), http.StatusInternalServerError)
		return
	}

	// Send command
	err = api.router.DeviceRegistry.SendCommand(deviceID, cmd)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send command: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"device_id":   deviceID,
		"command":     cmdType,
		"channel":     channel,
		"shots":       shots,
		"interval":    interval,
		"resolution":  resolution,
		"quality":     quality,
		"live_stream": isLiveStream,
	})
}

func (api *API) listMultimedia(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device")

	files := make([]map[string]interface{}, 0)

	// If device specified, list files for that device
	if deviceID != "" {
		deviceDir := filepath.Join(api.router.StreamRoot, deviceID, "multimedia")
		entries, err := os.ReadDir(deviceDir)
		if err != nil {
			log.Printf("[HTTP_API] Error reading multimedia directory: %v\n", err)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"device_id": deviceID,
				"count":     0,
				"files":     []map[string]interface{}{},
			})
			return
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				info, _ := entry.Info()
				files = append(files, map[string]interface{}{
					"filename": entry.Name(),
					"size":     info.Size(),
					"modified": info.ModTime().Unix(),
					"url":      fmt.Sprintf("/multimedia/stream/%s/%s", deviceID, entry.Name()),
				})
			}
		}
	} else {
		// List all devices with multimedia
		entries, err := os.ReadDir(api.router.StreamRoot)
		if err != nil {
			http.Error(w, "Failed to read stream root", http.StatusInternalServerError)
			return
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			multimediaDir := filepath.Join(api.router.StreamRoot, entry.Name(), "multimedia")
			fileEntries, err := os.ReadDir(multimediaDir)
			if err != nil {
				continue
			}

			for _, fileEntry := range fileEntries {
				if !fileEntry.IsDir() {
					info, _ := fileEntry.Info()
					files = append(files, map[string]interface{}{
						"device_id": entry.Name(),
						"filename":  fileEntry.Name(),
						"size":      info.Size(),
						"modified":  info.ModTime().Unix(),
						"url":       fmt.Sprintf("/multimedia/stream/%s/%s", entry.Name(), fileEntry.Name()),
					})
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"device_id": deviceID,
		"count":     len(files),
		"files":     files,
	})
}

func (api *API) serveMultimedia(w http.ResponseWriter, r *http.Request) {
	// Extract device ID and filename from URL
	path := strings.TrimPrefix(r.URL.Path, "/multimedia/stream/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) != 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	deviceID := parts[0]
	filename := parts[1]

	// Prevent path traversal
	if strings.Contains(filename, "..") || strings.HasPrefix(filename, "/") {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(api.router.StreamRoot, deviceID, "multimedia", filename)

	// Verify the file exists and is in the right directory
	realPath, err := filepath.Abs(filePath)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	expectedDir, _ := filepath.Abs(filepath.Join(api.router.StreamRoot, deviceID, "multimedia"))
	if !strings.HasPrefix(realPath, expectedDir) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil || fileInfo.IsDir() {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Determine MIME type based on extension
	ext := strings.ToLower(filepath.Ext(filename))
	contentType := "application/octet-stream"
	switch ext {
	case ".mp4", ".h264", ".h265":
		contentType = "video/mp4"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".wav":
		contentType = "audio/wav"
	case ".mp3":
		contentType = "audio/mpeg"
	case ".avi":
		contentType = "video/x-msvideo"
	case ".flv":
		contentType = "video/x-flv"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	http.ServeFile(w, r, filePath)
}

func (api *API) listLiveStreams(w http.ResponseWriter, r *http.Request) {
	streams := make([]map[string]interface{}, 0)
	entries, err := os.ReadDir(api.router.StreamRoot)
	if err != nil {
		log.Printf("[HTTP_API] Error reading stream root for live streams: %v\n", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"count":   0,
			"streams": streams,
			"error":   err.Error(),
		})
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		deviceID := entry.Name()
		hlsPath := filepath.Join(api.router.StreamRoot, deviceID)
		if !isValidHLSStreamDir(hlsPath) {
			continue
		}

		streams = append(streams, map[string]interface{}{
			"device_id":  deviceID,
			"hls_url":    fmt.Sprintf("/live/%s/index.m3u8", deviceID),
			"player_url": fmt.Sprintf("/player?stream=/live/%s/index.m3u8", deviceID),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":   len(streams),
		"streams": streams,
	})
}

func isValidHLSStreamDir(dir string) bool {
	manifest := filepath.Join(dir, "index.m3u8")
	info, err := os.Stat(manifest)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".ts" && ext != ".m4s" {
			continue
		}
		if segmentInfo, err := entry.Info(); err == nil && segmentInfo.Size() > 0 {
			return true
		}
	}

	return false
}

func (api *API) serveLiveStream(w http.ResponseWriter, r *http.Request) {
	// Extract device ID and resource from URL
	// /live/DEVICEID/index.m3u8 or /live/DEVICEID/file.ts
	path := strings.TrimPrefix(r.URL.Path, "/live/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) != 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	deviceID := parts[0]
	resource := parts[1]

	// Prevent path traversal
	if strings.Contains(resource, "..") || strings.HasPrefix(resource, "/") {
		http.Error(w, "Invalid resource", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(api.router.StreamRoot, deviceID, resource)

	// Verify the file is within the device directory
	realPath, err := filepath.Abs(filePath)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	expectedDir, _ := filepath.Abs(filepath.Join(api.router.StreamRoot, deviceID))
	if !strings.HasPrefix(realPath, expectedDir) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Determine content type based on extension
	ext := strings.ToLower(filepath.Ext(resource))
	contentType := "application/octet-stream"
	switch ext {
	case ".m3u8":
		contentType = "application/vnd.apple.mpegurl"
	case ".ts":
		contentType = "video/mp2t"
	case ".mp4":
		contentType = "video/mp4"
	}

	// Add CORS headers for HLS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Content-Type", contentType)

	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil || fileInfo.IsDir() {
		http.Error(w, "Resource not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	http.ServeFile(w, r, filePath)
}

// getLocalIP returns the local IP address that can reach the device
func getLocalIP(conn net.Conn) string {
	// Try to get local address from the connection
	if conn != nil {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			localAddr := tcpConn.LocalAddr().(*net.TCPAddr)
			ip := localAddr.IP.String()
			// Filter out loopback
			if ip != "127.0.0.1" && ip != "::1" {
				return ip
			}
		}
	}

	// Fallback: get first non-loopback interface
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("[HTTP_API] Error getting interfaces: %v\n", err)
		return "192.168.0.73" // Hardcoded fallback
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return "192.168.0.73" // Final fallback
}

// listStreams returns all available H.264 streams from the streams directory
func (api *API) listStreams(w http.ResponseWriter, r *http.Request) {
	streamsDir := filepath.Join(api.router.StreamRoot, "..")

	files := make([]map[string]interface{}, 0)

	// Read all .h264 files from streams directory
	entries, err := os.ReadDir(streamsDir)
	if err != nil {
		log.Printf("[HTTP_API] Error reading streams directory: %v\n", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"count":   0,
			"streams": []map[string]interface{}{},
			"error":   err.Error(),
		})
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".h264") {
			continue
		}

		info, _ := entry.Info()
		streamURL := fmt.Sprintf("/streams/%s", name)

		files = append(files, map[string]interface{}{
			"filename":    name,
			"size":        info.Size(),
			"modified":    info.ModTime().Unix(),
			"url":         streamURL,
			"stream_url":  fmt.Sprintf("/streams/%s?stream=1", name),
			"player_html": fmt.Sprintf("/?file=%s", name),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":   len(files),
		"streams": files,
	})
}

// serveStream serves H.264 video files from the streams directory
// Supports both download and streaming playback
func (api *API) serveStream(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/streams/")

	// Prevent path traversal
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	// Construct the full path
	streamsDir := filepath.Join(api.router.StreamRoot, "..")
	filePath := filepath.Join(streamsDir, filename)

	// Verify the file exists and is in the right directory
	realPath, err := filepath.Abs(filePath)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	expectedDir, _ := filepath.Abs(streamsDir)
	if !strings.HasPrefix(realPath, expectedDir) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil || fileInfo.IsDir() {
		log.Printf("[HTTP_API] Stream file not found: %s\n", filePath)
		http.Error(w, "Stream not found", http.StatusNotFound)
		return
	}

	// Determine content type based on extension
	ext := strings.ToLower(filepath.Ext(filename))
	contentType := "application/octet-stream"
	switch ext {
	case ".h264":
		contentType = "video/h264"
	case ".h265":
		contentType = "video/h265"
	case ".mp4":
		contentType = "video/mp4"
	case ".ts":
		contentType = "video/mp2t"
	}

	// Set headers for streaming
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Pragma", "no-cache")

	// Support range requests for seeking
	http.ServeFile(w, r, filePath)

	log.Printf("[HTTP_API] Served stream: %s (size: %d bytes)\n", filename, fileInfo.Size())
}
