# Integração Completa - Próximos Passos

## Arquivo: `internal/tcp/media_listener.go` - Método processJT1078Frame

Atualmente:
```go
func (ms *MediaServer) processJT1078Frame(frameData []byte, parser *protocol.JT1078Parser, conn net.Conn) {
	if len(frameData) < 25 {
		log.Printf("[MEDIA_CONN] Frame too small: %d bytes\n", len(frameData))
		return
	}

	// Log frame header info
	log.Printf("[MEDIA_CONN] Frame header: % X\n", frameData[:min(len(frameData), 32)])

	// TODO: Parse frame header with fragmentatio support
	// TODO: Assemble fragments if needed
	// TODO: Extract video/audio data
	// TODO: Send to FFmpeg worker
}
```

### Passo 1: Implementar Parsing Completo de Header

```go
func (ms *MediaServer) processJT1078Frame(frameData []byte, parser *protocol.JT1078Parser, conn net.Conn) {
	if len(frameData) < 25 {
		log.Printf("[MEDIA_CONN] Frame too small: %d bytes\n", len(frameData))
		return
	}

	// ========================================================================
	// PARSE HEADER COM SUPORTE A FRAGMENTAÇÃO
	// ========================================================================
	
	pos := 0
	
	// Sync Word (4 bytes)
	if frameData[pos] != 0x30 || frameData[pos+1] != 0x31 ||
		frameData[pos+2] != 0x63 || frameData[pos+3] != 0x64 {
		log.Printf("[MEDIA_CONN] Invalid sync word\n")
		return
	}
	pos += 4
	
	// Device ID (6 bytes BCD)
	deviceID := decodeBCDDeviceID(frameData[pos : pos+6])
	pos += 6
	log.Printf("[MEDIA_CONN] Device ID: %s\n", deviceID)
	
	// Channel ID (1 byte)
	channelID := frameData[pos]
	pos += 1
	log.Printf("[MEDIA_CONN] Channel: %d\n", channelID)
	
	// Flags (1 byte)
	flags := frameData[pos]
	pos += 1
	hasInterval := (flags & 0x01) != 0
	hasFrameSeq := (flags & 0x02) != 0
	hasFragmentIndex := (flags & 0x04) != 0
	hasFragmentCount := (flags & 0x08) != 0
	
	log.Printf("[MEDIA_CONN] Flags: Interval=%v, FrameSeq=%v, FragIdx=%v, FragCnt=%v\n",
		hasInterval, hasFrameSeq, hasFragmentIndex, hasFragmentCount)
	
	// Optional fields
	var interval uint16
	if hasInterval {
		interval = binary.BigEndian.Uint16(frameData[pos : pos+2])
		pos += 2
		log.Printf("[MEDIA_CONN] Interval: %d\n", interval)
	}
	
	var frameSeq uint32
	if hasFrameSeq {
		frameSeq = binary.BigEndian.Uint32(frameData[pos : pos+4])
		pos += 4
		log.Printf("[MEDIA_CONN] Frame Sequence: 0x%08X\n", frameSeq)
	}
	
	var fragIndex uint16
	if hasFragmentIndex {
		fragIndex = binary.BigEndian.Uint16(frameData[pos : pos+2])
		pos += 2
		log.Printf("[MEDIA_CONN] Fragment Index: %d\n", fragIndex)
	}
	
	var fragCount uint16
	if hasFragmentCount {
		fragCount = binary.BigEndian.Uint16(frameData[pos : pos+2])
		pos += 2
		log.Printf("[MEDIA_CONN] Fragment Count: %d\n", fragCount)
	}
	
	// Mandatory fields
	if len(frameData) < pos+12 {
		log.Printf("[MEDIA_CONN] Frame too small for data length and timestamp\n")
		return
	}
	
	dataLen := binary.BigEndian.Uint32(frameData[pos : pos+4])
	pos += 4
	log.Printf("[MEDIA_CONN] Data Length: %d bytes\n", dataLen)
	
	timestamp := binary.BigEndian.Uint64(frameData[pos : pos+8])
	pos += 8
	log.Printf("[MEDIA_CONN] Timestamp: 0x%016X\n", timestamp)
	
	headerSize := pos
	log.Printf("[MEDIA_CONN] Header Size: %d bytes\n", headerSize)
	
	// ========================================================================
	// EXTRACT VIDEO/AUDIO DATA
	// ========================================================================
	
	if len(frameData) < headerSize+int(dataLen) {
		log.Printf("[MEDIA_CONN] Insufficient data: header=%d, dataLen=%d, total=%d\n",
			headerSize, dataLen, len(frameData))
		return
	}
	
	h264Data := frameData[headerSize : headerSize+int(dataLen)]
	log.Printf("[MEDIA_CONN] H.264 Data extracted: %d bytes\n", len(h264Data))
	
	// ========================================================================
	// ANALYZE H.264 NAL UNITS
	// ========================================================================
	
	nalUnits := extractNALUnits(h264Data)
	log.Printf("[MEDIA_CONN] Found %d NAL units\n", len(nalUnits))
	
	for i, nal := range nalUnits {
		nalType := nal.Data[0] & 0x1F
		nalTypeName := getNALTypeName(nalType)
		log.Printf("[MEDIA_CONN]   NAL %d: Type=%d (%s), Size=%d\n",
			i, nalType, nalTypeName, len(nal.Data))
	}
	
	// ========================================================================
	// HANDLE FRAGMENTATION IF PRESENT
	// ========================================================================
	
	if hasFragmentIndex && hasFragmentCount {
		if fragIndex < fragCount {
			log.Printf("[MEDIA_CONN] Fragment %d/%d, queuing for reassembly...\n",
				fragIndex, fragCount)
			// TODO: Implement fragment reassembly
			// Store in buffer indexed by frameSeq
			// Only process when all fragments received
			return
		}
	}
	
	// ========================================================================
	// SEND TO FFMPEG WORKER (quando implementado)
	// ========================================================================
	
	// ms.sendToFFmpeg(deviceID, channelID, h264Data, nalUnits)
	
	log.Printf("[MEDIA_CONN] ✓ Frame processed successfully\n")
}

// Helper functions

func decodeBCDDeviceID(bcdBytes []byte) string {
	result := ""
	for _, b := range bcdBytes {
		high := (b >> 4) & 0x0F
		low := b & 0x0F
		result += fmt.Sprintf("%d%d", high, low)
	}
	return result
}

func extractNALUnits(h264Data []byte) []struct {
	Data []byte
} {
	var nals []struct {
		Data []byte
	}
	
	pos := 0
	for pos < len(h264Data) {
		// Look for start code (0x00 0x00 0x01 or 0x00 0x00 0x00 0x01)
		if pos+3 <= len(h264Data) &&
			h264Data[pos] == 0x00 && h264Data[pos+1] == 0x00 && h264Data[pos+2] == 0x01 {
			// 3-byte start code
			start := pos
			pos += 3
			
			// Find next start code
			for pos+3 <= len(h264Data) {
				if h264Data[pos] == 0x00 && h264Data[pos+1] == 0x00 &&
					h264Data[pos+2] == 0x01 {
					break
				}
				pos++
			}
			
			nalData := h264Data[start:pos]
			nals = append(nals, struct {
				Data []byte
			}{nalData})
		} else if pos+4 <= len(h264Data) &&
			h264Data[pos] == 0x00 && h264Data[pos+1] == 0x00 &&
			h264Data[pos+2] == 0x00 && h264Data[pos+3] == 0x01 {
			// 4-byte start code
			start := pos
			pos += 4
			
			for pos+4 <= len(h264Data) {
				if h264Data[pos] == 0x00 && h264Data[pos+1] == 0x00 &&
					h264Data[pos+2] == 0x00 && h264Data[pos+3] == 0x01 {
					break
				}
				pos++
			}
			
			nalData := h264Data[start:pos]
			nals = append(nals, struct {
				Data []byte
			}{nalData})
		} else {
			pos++
		}
	}
	
	return nals
}

func getNALTypeName(nalType byte) string {
	switch nalType {
	case 1:
		return "Slice (P/B)"
	case 5:
		return "Slice (IDR/Key)"
	case 7:
		return "SPS"
	case 8:
		return "PPS"
	case 6:
		return "SEI"
	default:
		return "Other"
	}
}
```

### Passo 2: Adicionar Imports Necessários

No início de `internal/tcp/media_listener.go`:

```go
import (
	"bufio"
	"encoding/binary"  // ← Add if missing
	"fmt"             // ← Add if missing
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"jt808-broker/internal/protocol"
	"jt808-broker/internal/stream"
)
```

### Passo 3: Integração com FFmpeg Worker (Próximo)

Quando tiver SPS + PPS + IDR:

```go
// Em processJT1078Frame, após analisar NALs:

var sps []byte
var pps []byte
var keyFrameFound bool

for _, nal := range nalUnits {
	nalType := nal.Data[0] & 0x1F
	switch nalType {
	case 7: // SPS
		sps = nal.Data
		log.Printf("[MEDIA_CONN] SPS found: %d bytes\n", len(sps))
	case 8: // PPS
		pps = nal.Data
		log.Printf("[MEDIA_CONN] PPS found: %d bytes\n", len(pps))
	case 5: // IDR (Key frame)
		keyFrameFound = true
		log.Printf("[MEDIA_CONN] ✓ KEY FRAME FOUND!\n")
	}
}

// Se temos SPS + PPS + IDR, stream está pronto
if sps != nil && pps != nil && keyFrameFound {
	log.Printf("[MEDIA_CONN] ✓✓✓ Stream é VÁLIDO e pronto para transcode\n")
	// ms.ffmpegWorker.SendH264Data(h264Data)
	// Vai iniciar pipeline: H.264 → FFmpeg → RTSP/HLS/MP4
}
```

## Arquivos Criados/Modificados

1. ✓ **Criado**: `internal/stream/buffer.go` (JT1078StreamBuffer)
2. ✓ **Criado**: `internal/stream/buffer_test.go` (Testes unitários)
3. ✓ **Modificado**: `internal/tcp/media_listener.go` (Integração do buffer)
4. ✓ **Criado**: `docs/STREAM_BUFFER_SOLUTION.md` (Documentação técnica)
5. ✓ **Criado**: `docs/BUFFER_VERIFICATION.md` (Verificação e diagnóstico)
6. ✓ **Criado**: `docs/BEFORE_AFTER_VISUALIZATION.md` (Comparação visual)
7. ✓ **Este arquivo**: `docs/INTEGRATION_NEXT_STEPS.md`

## Status Geral

✓ **Compilação**: OK (go build sem erros)
✓ **Buffer**: Implementado e testável
✓ **Frame Extraction**: Funcional
✓ **Fragment Detection**: Código presente
✓ **Next**: Parsing completo e FFmpeg integration

## Verificação Rápida

```bash
# Compilar
go build -o server ./cmd/server/main.go

# Rodar
./server

# Em outro terminal, conectar com JT1078 client
# Você deve ver:
# [MEDIA_CONN] Extracted 1 complete frames
# [MEDIA_CONN] Processing frame 0: 61466 bytes
# [MEDIA_CONN] Frame header: 30 31 63 64 ...
```

Se vir isso, a solução está funcionando! 🎯
