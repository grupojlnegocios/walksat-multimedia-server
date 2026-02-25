# Tratativa de Frames de Vídeo e Streaming

## Visão Geral

Este documento detalha como o sistema JT808-Broker processa frames de vídeo e áudio do protocolo JT1078, com conversão em tempo real e streaming.

## Arquitetura de Streaming

```
Dispositivo (Câmera)
        ↓
TCP Port 6207
        ↓
JT808Session (JT1078Parser)
        ↓
VideoFrame / AudioFrame
        ↓
VideoFrameHandler / AudioFrameHandler
        ↓
StreamConverter
        ↓
FrameBuffer → Flush → Arquivo/Stream
        ↓
FFmpeg (Conversão)
        ↓
RTSP/HTTP/HLS Stream
```

## Parte 1: Estruturas de Frames JT1078

### Video Frame Header (30 bytes)

```
Offset  Size  Campo             Tipo      Descrição
0       1B    FrameType         UINT8     I/P/B/Audio frame type
1       1B    CodecType         UINT8     H264/H265/MJPG/MPEG
2       1B    FrameRate         UINT8     FPS (ex: 25, 30)
3-4     2B    VideoWidth        UINT16    Resolução horizontal
5-6     2B    VideoHeight       UINT16    Resolução vertical
7-10    4B    Timestamp         UINT32    Timestamp 90kHz (RTP)
11-16   6B    SystemTime        BCD       YYMMDDHHMMSS
17-18   2B    FrameSequence     UINT16    Seq. do frame
19      1B    StreamID          UINT8     ID do stream
20      1B    Reserved          UINT8     Reservado
21-24   4B    DataSize          UINT32    Tamanho dos dados
25-32   8B    PTS               UINT64    Presentation TimeStamp
```

**Exemplo Prático**:
```
Raw Header:
00 01 18 03 E0 07 68 00 00 27 10 26 01 15 14 30 15 00 01 00 00 00 AB CD EF 00 00 00 00 00

Decodificado:
- FrameType: 0x00 (I-frame)
- CodecType: 0x01 (H.265)
- FrameRate: 24 FPS
- VideoWidth: 0x03E0 = 992 pixels
- VideoHeight: 0x0768 = 1896 pixels
- Timestamp: 0x000027XX (10.5ms @ 90kHz)
- SystemTime: 26-01-15-14-30-15 = 26/01/2024 14:30:15
- FrameSequence: 0x0001
- StreamID: 0x00 (Canal 0)
- DataSize: 0x0000ABCD = 43981 bytes
- PTS: 0x00000000CD = 205ms
```

### Audio Frame Header (25 bytes)

```
Offset  Size  Campo             Tipo      Descrição
0       1B    CodecType         UINT8     PCM/AMR/AAC/G726/etc
1-2     2B    SampleRate        UINT16    Hz (ex: 8000, 16000)
3       1B    SampleBits        UINT8     8/16/32 bits
4       1B    ChannelCount      UINT8     Mono=1, Estéreo=2
5-6     2B    FrameSequence     UINT16    Seq. do frame
7-10    4B    Timestamp         UINT32    Timestamp 90kHz
11-16   6B    SystemTime        BCD       YYMMDDHHMMSS
17      1B    StreamID          UINT8     ID do stream
18      1B    Reserved          UINT8     Reservado
19-22   4B    DataSize          UINT32    Tamanho dos dados
23-24   8B    PTS               UINT64    Presentation TimeStamp
```

## Parte 2: Parsing de Frames

### Fluxo de Recebimento

```
TCP Packet (JT808 0x1001 para vídeo, 0x1002 para áudio)
        ↓
BaseParser.Push() - Extrai PacketFrame
        ↓
JT1078Parser.ParseVideoFrame() / ParseAudioFrame()
        ↓
VideoFrame struct com header + dados
        ↓
VideoFrameHandler.HandleVideoFrame()
        ↓
StreamConverter.AddVideoFrame()
```

### Implementação em JT1078Parser

```go
func (p *JT1078Parser) ParseVideoFrame(frame *PacketFrame) (*VideoFrame, error) {
    // 1. Validar tamanho mínimo (30 bytes header + dados)
    if len(frame.Body) < VideoFrameHeaderSize {
        return nil, fmt.Errorf("video frame too small")
    }

    // 2. Ler header binary (big-endian)
    header := &VideoFrameHeader{}
    buf := bytes.NewReader(frame.Body[:VideoFrameHeaderSize])
    binary.Read(buf, binary.BigEndian, &header.FrameType)
    // ... resto dos campos

    // 3. Validar tamanho dos dados
    if header.DataSize > maxFrameSize {
        return nil, fmt.Errorf("frame data exceeds max size")
    }

    // 4. Extrair payload
    frameData := frame.Body[VideoFrameHeaderSize:VideoFrameHeaderSize+header.DataSize]

    // 5. Incrementar sequência global
    p.videoSequence++

    return &VideoFrame{
        Header:    header,
        Data:      frameData,
        DeviceID:  p.deviceID,
        Timestamp: frame.Timestamp,
        Sequence:  p.videoSequence,
    }, nil
}
```

## Parte 3: Tipos de Codecs e Formatos

### Codecs de Vídeo Suportados

| Tipo | ID  | Descrição      | Extensão | FFmpeg Codec |
|------|-----|----------------|----------|--------------|
| H.264 | 0x00 | MPEG-4 AVC    | .h264    | h264         |
| H.265 | 0x01 | HEVC/H.265    | .h265    | hevc         |
| MJPEG | 0x02 | Motion JPEG   | .mjpg    | mjpeg        |
| MPEG  | 0x03 | MPEG-2/4      | .mpeg    | mpeg2video   |

### Codecs de Áudio Suportados

| Tipo | ID  | Descrição      | Extensão | FFmpeg Codec |
|------|-----|----------------|----------|--------------|
| PCM  | 0x00 | PCM Raw        | .pcm     | pcm_s16le    |
| AMR  | 0x01 | Adaptive Multi-Rate | .amr | amrnb        |
| AAC  | 0x02 | Advanced Audio Coding | .aac | aac        |
| G.726| 0x03 | ITU G.726      | .g726    | g726         |
| G.729| 0x04 | ITU G.729      | .g729    | g729         |
| Opus | 0x05 | Opus           | .opus    | opus         |

## Parte 4: Frame Buffer e Conversão

### FrameBuffer

Acumula frames para processamento em lotes:

```go
type FrameBuffer struct {
    VideoFrames   []*VideoFrame
    AudioFrames   []*AudioFrame
    MaxFrames     int           // Flush ao atingir limite
    FlushInterval time.Duration // Flush periódico
    onFlushCallback func(...)   // Callback customizado
}
```

**Estratégia de Flush**:
- Quando atinge `MaxFrames` (ex: 30)
- Quando recebe I-frame em stream de vídeo
- Periodicamente a cada `FlushInterval` (ex: 2 segundos)

**Exemplo de Uso**:
```go
buffer := NewFrameBuffer(30, 2*time.Second)

// Configurar callback
buffer.SetOnFlushCallback(func(vFrames, aFrames) {
    // Processar lote de frames
    encodeWithFFmpeg(vFrames, aFrames)
})

// Adicionar frames
for frame := range incomingFrames {
    buffer.AddVideoFrame(frame)
}
```

### StreamConverter

Coordena conversão de stream completo:

```go
converter := NewStreamConverter(
    "000000000001",      // Device ID
    "video_ch0",         // Stream ID
    FrameTypeVideo,      // Media type
    "/streams/device1",  // Output dir
    "mp4",               // Output format
)

if err := converter.Start(); err != nil {
    log.Fatal(err)
}

// Adicionar frames
converter.AddVideoFrame(frame)

// Obter estatísticas
stats := converter.GetStats()
fmt.Printf("FPS: %.2f, Throughput: %.2f Mbps", stats["fps"], stats["throughput_mbps"])

// Parar
converter.Stop()
```

## Parte 5: Gerenciamento de Streams

### VideoFrameHandler

Coordena múltiplos streams de vídeo:

```go
handler := NewVideoFrameHandler(streamManager)

// Iniciar stream
handler.HandleVideoStreamStart("000000000001", parser, 0)

// Processar frames
for frame := range receivedFrames {
    handler.HandleVideoFrame("000000000001", frame)
}

// Obter status
status, _ := handler.GetStreamStatus("video_ch0")
fmt.Printf("Resolution: %s, FPS: %.2f", status["resolution"], status["fps"])

// Parar stream
handler.StopVideoStream("000000000001", "video_ch0")
```

**Status do Stream**:
- `waiting_keyframe`: Aguardando I-frame (antes do primeiro keyframe)
- `active`: Recebendo frames normalmente
- `buffering`: Frames chegando lentamente

## Parte 6: Integração com Sessão JT808

### Exemplo de Uso Completo

```go
// No jt808_session.go:

type JT808Session struct {
    // ... campos existentes ...
    jt1078Parser      *protocol.JT1078Parser
    videoFrameHandler *stream.VideoFrameHandler
    audioFrameHandler *stream.AudioFrameHandler
    streamManager     *stream.StreamManager
}

func (s *JT808Session) handleMessage(msg *protocol.JT808Message) {
    switch msg.MsgID {
    case protocol.MsgLogin:
        // Inicializar parsers para vídeo
        s.jt1078Parser = protocol.NewJT1078Parser()
        s.jt1078Parser.SetDeviceID(deviceID)
        s.streamManager = stream.NewStreamManager(s.outputDir)
        s.videoFrameHandler = stream.NewVideoFrameHandler(s.streamManager)
        s.audioFrameHandler = stream.NewAudioFrameHandler(s.streamManager)

    case protocol.MsgVideoData: // 0x1001
        // Parsear frame de vídeo
        packetFrame := convertToPacketFrame(msg.Body)
        videoFrame, err := s.jt1078Parser.ParseVideoFrame(packetFrame)
        if err != nil {
            log.Printf("Error parsing video frame: %v", err)
            return
        }

        // Processar frame
        if err := s.videoFrameHandler.HandleVideoFrame(deviceID, videoFrame); err != nil {
            log.Printf("Error handling video frame: %v", err)
        }

        // Log para I-frames
        if videoFrame.IsKeyFrame() {
            log.Printf("[VIDEO] I-Frame: %dx%d, Seq: %d, Size: %d bytes",
                videoFrame.Header.VideoWidth,
                videoFrame.Header.VideoHeight,
                videoFrame.Header.FrameSequence,
                len(videoFrame.Data))
        }

    case protocol.MsgAudioData: // 0x1002
        // Similar para áudio
        packetFrame := convertToPacketFrame(msg.Body)
        audioFrame, err := s.jt1078Parser.ParseAudioFrame(packetFrame)
        if err != nil {
            log.Printf("Error parsing audio frame: %v", err)
            return
        }

        if err := s.audioFrameHandler.HandleAudioFrame(deviceID, audioFrame); err != nil {
            log.Printf("Error handling audio frame: %v", err)
        }

    case protocol.MsgLogout:
        // Limpar streams
        s.streamManager.StopAll()
    }
}

// API HTTP para iniciar stream
func (api *API) HandleStartVideoStream(w http.ResponseWriter, r *http.Request) {
    deviceID := r.URL.Query().Get("device")
    channel := r.URL.Query().Get("channel") // 0-3

    session := registry.Get(deviceID)
    if session == nil || session.videoFrameHandler == nil {
        http.Error(w, "Device or video handler not found", http.StatusNotFound)
        return
    }

    if err := session.videoFrameHandler.HandleVideoStreamStart(deviceID, session.jt1078Parser, 0); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, `{"status": "streaming", "device": "%s", "channel": %s}`, deviceID, channel)
}
```

## Parte 7: Operações com Frames

### Detecção de Tipo de Frame

```go
// I-frame (chave) - contém informação completa
if frame.IsKeyFrame() {
    // Sincronizar stream, reset buffer, etc
    codec := frame.GetVideoCodecString() // "H.264", "H.265"
    log.Printf("Keyframe: %s, Res: %dx%d",
        codec, frame.Header.VideoWidth, frame.Header.VideoHeight)
}

// P-frame - referencia I-frame anterior
// B-frame - referencia frames antes e depois
```

### Cálculo de Bitrate

```go
func calculateBitrate(frames []*protocol.VideoFrame, duration time.Duration) float64 {
    totalBytes := 0
    for _, f := range frames {
        totalBytes += len(f.Data)
    }
    // bits/second / 1e6 = Mbps
    return float64(totalBytes*8) / duration.Seconds() / 1e6
}
```

### Sincronização de Áudio-Vídeo

```go
type AVSync struct {
    VideoTimestamp uint32
    AudioTimestamp uint32
    PTSDiff        time.Duration // Deve ser < 100ms
}

// Verificar sincronização
if abs(videoFrame.Header.PTS - audioFrame.Header.PTS) > 100000 { // 100ms @ 90kHz
    log.Printf("WARNING: A/V Sync problem - Video: %d, Audio: %d",
        videoFrame.Header.PTS, audioFrame.Header.PTS)
}
```

## Parte 8: Tratativa de Erros

### Frames Incompletos

```
Cenário: Frame com header válido mas dados incompletos

TCP Packet 1: Header (30B) + Dados (1000B)
TCP Packet 2: Restante dos dados (2000B) + Próximo frame

Solução:
1. Acumular dados no IncompleteFrame
2. Quando totalizado, marcar como completo
3. Processar quando tem todos os dados
```

### Descontinuidades de Sequência

```go
func checkSequenceGap(lastSeq, newSeq uint16) int {
    if newSeq <= lastSeq {
        return int(newSeq) + (65536 - int(lastSeq)) // Wrapping
    }
    return int(newSeq) - int(lastSeq)
}

gap := checkSequenceGap(lastFrameSeq, newFrame.Header.FrameSequence)
if gap > 1 {
    log.Printf("WARNING: Frame gap detected - Lost %d frames", gap-1)
    // Solicitar retransmissão ou iniciar novo stream
}
```

### Codec/Resolução Muda

```
Se durante stream:
- Codec muda de H.264 para H.265
- Resolução muda de 1080p para 720p
- Frame rate muda

Ações:
1. Log warning
2. Criar novo arquivo
3. Re-inicializar converter
4. Notificar clientes RTSP
```

## Parte 9: Limpeza de Recursos

### Cleanup Automático

```go
// No StreamManager:
func (sm *StreamManager) cleanupStale() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        sm.mutex.Lock()
        for key, converter := range sm.converters {
            // Se não recebeu frame em 60s
            if time.Since(converter.LastFrameTime) > 60*time.Second {
                converter.Stop()
                delete(sm.converters, key)
            }
        }
        sm.mutex.Unlock()
    }
}
```

## Parte 10: Monitoramento e Logs

### Padrão de Logging

```
[VIDEO_HANDLER] Stream started - Device: 000000000001, Stream: video_ch0, Channel: 0
[VIDEO_HANDLER] I-Frame received - Seq: 42, Size: 51234 bytes, Res: 1920x1080
[VIDEO_HANDLER] Codec change detected - H.264 → H.265
[VIDEO_HANDLER] WARNING: Frame gap detected - Lost 3 frames
[VIDEO_HANDLER] Stream stopped - Device: 000000000001, Stream: video_ch0
  Frames: 1250 (KeyFrames: 50), Bytes: 125632640, Duration: 1m12s, FPS: 30.12

[STREAM] Converter started - Device: 000000000001, Stream: video_ch0, Output: /streams/000000000001/streams/video_ch0_20240124_143015.mp4
[STREAM] Converter stopped - Frames: 1250, Bytes: 125632640, Duration: 1m12s
```

## Performance e Otimizações

### Buffer Sizing

- `MaxFrames = 30`: Buffer ~1-2 segundos a 30 FPS
- `FlushInterval = 2s`: Garante latência máxima
- Ideal para RTSP: 1000ms latência

### Throughput Estimado

| Codec | Resolução | FPS | Bitrate (Mbps) |
|-------|-----------|-----|----------------|
| H.264 | 1080p     | 30  | 4-6            |
| H.264 | 720p      | 30  | 2-4            |
| H.265 | 1080p     | 30  | 2-3            |
| MJPEG | 1080p     | 30  | 8-15           |

### CPU Usage

- H.264 parsing: ~1-2% CPU (100M stream)
- MJPEG parsing: ~0.5% CPU (simples)
- FFmpeg conversão: ~10-30% CPU (transcoding)

