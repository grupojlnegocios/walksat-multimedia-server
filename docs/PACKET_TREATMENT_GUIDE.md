# Guia de Tratativa de Pacotes Conforme Especificação do Fabricante

## Sumário Executivo

Este documento detalha como o projeto JT808-Broker trata cada tipo de pacote conforme a especificação oficial JT/T 808-2013 do fabricante, com suporte estendido para JT/T 1078-2014 (vídeo) e JT/T 1077-2020 (áudio).

## Parte 1: Recebimento e Parsing

### 1.1 Fluxo de Recebimento

```
Dispositivo → TCP Port 6207 → tcp/listener.go
                                    ↓
                          TCP Connection Handler
                                    ↓
                          stream/jt808_session.go
                                    ↓
                          protocol/jt808.go Parser
                                    ↓
                          stream/jt808_session.go Handler
```

### 1.2 Processamento de Bytes Brutos

#### Entrada: Stream TCP contínuo
```
7E 00 01 00 1C 00 00 00 00 00 01 00 02 [Body 28 bytes] CS 7E
7E 00 03 00 00 00 00 00 00 00 01 00 03 CS 7E
7D 02 00 01 7D 01 00 1C ... [próximo pacote]
```

#### Etapa 1: Acumular Buffer
```go
// em JT808Session.Run()
data := make([]byte, 1024)
n, _ := conn.Read(data)
parser.Push(data[:n])  // Acumular em buffer interno
```

#### Etapa 2: Encontrar Delimitadores
```
Buffer: [7E 00 01 ... CS 7E] [Parcial do próximo 7E ...]
        └─ Quadro Completo ─┘
```

**Função**: `findNextDelimiter()` em `parser.go`
- Procura por 0x7E
- Ignora escapes (0x7D não é delimitador)
- Retorna offset se encontrado

#### Etapa 3: Remover Escape
**Regra de Escape** (conforme fabricante):
- `0x7E` → `0x7D 0x02`
- `0x7D` → `0x7D 0x01`

**Implementação**: `Unescape()` em `parser.go`
```go
// Input (com escape): [7D 02 00 01 7D 01 00]
// Output (decodificado): [7E 00 01 7D 00]

Exemplo concreto:
Input bytes:   7D 02 00 01 7D 01 00
Output bytes:  7E 00 01 7D 00
```

**Casos de Erro**:
- Sequência 0x7D incompleta no final do buffer → Manter no buffer, aguardar próximo read
- 0x7D seguido de byte inválido → Erro, descartar quadro

#### Etapa 4: Validar Checksum
**Cálculo de Checksum** (XOR - conforme fabricante):
```
Checksum = byte[0] XOR byte[1] XOR ... XOR byte[n-1]
```

**Implementação**: `CalculateChecksum()` em `parser.go`
```go
// Input: Header (13 bytes) + Body (N bytes)
// Output: 1 byte (0x00 a 0xFF)

Exemplo:
Header:  00 01 00 1C 00 00 00 00 00 01 00 02
Body:    [28 bytes de dados]
Result:  XOR de todos = 0xAB (exemplo)
```

**Validação**: Compare checksum extraído com calculado
- Se diferente: Erro ErrInvalidChecksum, descartar quadro

### 1.3 Parsing de Header

**Localização no Quadro**:
```
[7E] [Header 13 bytes] [Body variable] [CS] [7E]
      │
      └─ Sempre 13 bytes
```

**Estrutura de Header** (conforme especificação):
```
Offset  Size  Campo              Tipo
0-1     2B    Message ID         uint16 (Big-Endian)
2-3     2B    Properties         uint16 (Big-Endian)
4-9     6B    Device ID          BCD (3 bytes → 6 dígitos)
10-11   2B    Sequence Number    uint16 (Big-Endian)
12      1B    Body Length        uint8 (se Properties & 0x2000 == 0)
        OR    High Byte of Len   (se Properties & 0x2000 == 1)
```

**Implementação**: `parseHeader()` em `parser.go`

#### Device ID - Conversão BCD
**BCD (Binary Coded Decimal)**:
- Cada nibble (4 bits) = 1 dígito decimal
- 1 byte = 2 dígitos

```go
// Input bytes: [00 00 00 00 00 01]
// Conversão:
//   00 → "00"
//   00 → "00"
//   00 → "00"
//   00 → "00"
//   00 → "00"
//   01 → "01"
// Output: "000000000001"

// Implementação DecodeBCD():
func DecodeBCD(data []byte) string {
    result := ""
    for _, b := range data {
        high := (b >> 4) & 0x0F
        low := b & 0x0F
        if high > 9 || low > 9 {
            return "", ErrInvalidBCD  // Byte com valor > 9 em nibble
        }
        result += fmt.Sprintf("%d%d", high, low)
    }
    return result, nil
}
```

#### Validação de Properties
```
Bit 0-13: Body Length (13 bits)       → Tamanho do body
Bit 14: Response Required (1 = sim)   → Necessário responder
Bit 15: Encryption (1 = sim)          → Body encriptado (RSA)

Exemplo: 0x001C = 0000 0000 0001 1100 (binário)
- Bits 0-13: 0x001C = 28 → Body tem 28 bytes
- Bit 14: 0 → Não requer resposta
- Bit 15: 0 → Sem criptografia
```

### 1.4 Extração de Body

**Tamanho do Body**:
```go
// De Properties:
bodyLength := header.Properties & 0x3FFF  // Mascara bits 0-13

// Se Properties & 0x2000 (bit 13 = 1):
//   Body Length usa 2 bytes no header + 13 bits de Properties
//   bodyLength = (header.Properties & 0x1FFF) | (bodyFirstByte << 13)
```

**Validação**:
- Verificar se há bodyLength bytes disponível no buffer
- Se não: Aguardar próximo read() do TCP

## Parte 2: Tipos de Mensagem e Tratativa

### 2.1 Classificação de Message IDs

**Servidor ← Dispositivo** (Range: 0x0001 - 0x0FFF):
```
0x0001: Login
0x0002: Logout
0x0003: Heartbeat
0x0004: Report
0x0200: Location Report
0x0800: Multimedia Event Inform
0x0801: Multimedia Data Transfer
0x0900: Alarm Report
0x0901-0x0FFF: [Outros tipos]
```

**Servidor → Dispositivo** (Range: 0x8000 - 0x8FFF):
```
0x8001: General Response
0x8100: Location Services Response
0x8300: Text Message
0x8400: Event Setting
0x8500: Camera Commands
0x8600: Stored Media Search
0x8601: Stored Media Upload Confirm
0x8602: Stored Media Retrieval
0x8603: Drive Recorder Play Back Control
0x8800: Multimedia Data Server Response
0x9000-0x9FFF: [Tipos customizados]
```

### 2.2 Tratativa por Tipo - Login (0x0001)

**Body Structure** (conforme especificação):
```
Offset  Size  Campo                    Tipo
0-3     4B    Device GNSS Latitude     INT32 (degrees × 10^6)
4-7     4B    Device GNSS Longitude    INT32 (degrees × 10^6)
8-9     2B    Device GNSS Height       UINT16 (meters)
10-11   2B    Telephone/SIM            BCD (6 dígitos)
12-16   5B    IMEI                     BCD (15 dígitos)
17-18   2B    Firmware Version         String (2 chars)
19-22   4B    Hardware Version         String (4 chars)
...     ...   ...                      ...
```

**Implementação** em `jt808_session.go`:
```go
case protocol.MsgLogin:
    // 1. Decodificar body em LoginMessage
    loginMsg, err := parseLoginMessage(frame.Body)
    
    // 2. Validar Device ID (BCD)
    deviceID := protocol.DecodeBCD(frame.Header.DeviceID)
    
    // 3. Registrar no Registry
    registry.Register(deviceID, session)
    
    // 4. Responder com 0x8001 (General Response)
    response, err := protocol.BuildResponse(
        frame.Header.SequenceNum,
        frame.Header.MessageID,
        1,  // Result: 1 = Success
        deviceID,
    )
    
    // 5. Enviar para dispositivo
    conn.Write(response)
```

**Resposta Esperada**: Message 0x8001 (General Response)
```
Body:
- Original Message ID: 0x0001 (2 bytes)
- Result: 0x01 = Success (1 byte)
```

### 2.3 Tratativa por Tipo - Heartbeat (0x0003)

**Body**: Vazio (0 bytes)

**Implementação**:
```go
case protocol.MsgHeartbeat:
    // 1. Extrair Device ID
    deviceID := protocol.DecodeBCD(frame.Header.DeviceID)
    
    // 2. Registrar heartbeat no Registry
    session.LastHeartbeat = time.Now()
    
    // 3. Responder com 0x8001
    response, err := protocol.BuildResponse(
        frame.Header.SequenceNum,
        0x0003,
        1,  // Success
        deviceID,
    )
    conn.Write(response)
```

**Timeout de Heartbeat**: Se não receber em 60s → Fechar conexão

### 2.4 Tratativa por Tipo - Location Report (0x0200)

**Body Structure**:
```
Offset  Size  Campo                       Tipo
0-3     4B    Latitude                    INT32 (deg × 10^6)
4-7     4B    Longitude                   INT32 (deg × 10^6)
8-9     2B    Altitude                    UINT16 (meters)
10-11   2B    Speed                       UINT16 (km/h × 10)
12-13   2B    Direction                   UINT16 (degrees)
14      1B    Timestamp (BCD: YYMMDDHHMMSS) 6B
20-21   2B    Status                      UINT16 (bit flags)
22+     VAR   Extra Info (optional)       Variable
```

**Implementação**:
```go
case protocol.MsgLocationReport:
    // 1. Parsear location
    location := parseLocationReport(frame.Body)
    
    // 2. Armazenar em registry/banco de dados
    session.LastLocation = location
    
    // 3. Logar com formatação
    log.Printf("[LOCATION] Device: %s, Lat: %.6f, Lon: %.6f, Speed: %d km/h",
        deviceID,
        float64(location.Latitude)/1e6,
        float64(location.Longitude)/1e6,
        location.Speed)
    
    // 4. Responder (se Properties.ResponseRequired)
    if frame.Header.Properties&0x4000 != 0 {
        response, _ := protocol.BuildResponse(...)
        conn.Write(response)
    }
```

### 2.5 Tratativa por Tipo - Multimedia Event (0x0800) e Data (0x0801)

**Multimedia Event (0x0800) Body**:
```
Offset  Size  Campo                    Tipo
0       1B    Event Code               UINT8
1       1B    Channel ID               UINT8
2-3     2B    Event ID                 UINT16
4-5     2B    Reserved                 UINT16
6-9     4B    Multimedia Type/Format   UINT16 + UINT16
```

**Implementação**:
```go
case protocol.MsgMultimediaEvent:
    mediaEvent := parseMultimediaEvent(frame.Body)
    
    // Mapear tipo de mídia
    mediaType := mediaEvent.Type  // 0=Image, 1=Audio, 2=Video
    format := mediaEvent.Format   // 0=JPEG, 1=MP3, 2=MP4, 3=H264
    
    log.Printf("[MULTIMEDIA] Event: %s, Format: %s, Channel: %d",
        getEventName(mediaEvent.EventCode),
        getFormatName(format),
        mediaEvent.ChannelID)
    
    // Criar armazenador para dados
    store := multimedia_store.Create(
        deviceID,
        mediaEvent.EventID,
        mediaType,
        format,
    )
    
    // Responder com 0x8800 confirmando receção
    response := protocol.BuildMultimediaResponse(
        frame.Header.SequenceNum,
        0x01,  // Success
    )
    conn.Write(response)
```

**Multimedia Data (0x0801) Body**:
```
Offset  Size  Campo                    Tipo
0-1     2B    Multimedia Type/Format   UINT16
2-3     2B    Multimedia Data ID       UINT16
4-5     2B    Reserved                 UINT16
6-9     4B    Multimedia Data Length   UINT32
10+     VAR   Multimedia Data          BINARY
```

**Implementação**:
```go
case protocol.MsgMultimediaData:
    mediaData := parseMultimediaData(frame.Body)
    
    // Recuperar armazenador
    store := multimedia_store.Get(deviceID, mediaData.DataID)
    
    // Escrever dados em arquivo
    store.Write(mediaData.Data)
    
    // Se completo, processar
    if store.IsComplete() {
        // Converter com FFmpeg se necessário
        ffmpeg.Convert(store.FilePath, outputFormat)
    }
    
    // Responder com 0x8800
```

## Parte 3: Envio de Comandos

### 3.1 Estrutura Geral de Comando

**Message IDs de Comando**: 0x8xxx (Server → Device)

**Fluxo**:
```
API HTTP Request
    ↓
http/api.go Handler
    ↓
registry.Get(deviceID)
    ↓
protocol/jt808.go Build*Command()
    ↓
Encode → Escape → Add Delimiters
    ↓
session.Conn.Write()
    ↓
Dispositivo recebe comando
```

### 3.2 Comando: Camera Capture (0x8500)

**Requisição HTTP**:
```
POST /camera/capture?device=000000000001&channel=1
Content-Type: application/json
{
    "command": "capture",
    "channel": 1,
    "storage": 0,
    "resolution": 1
}
```

**Body do Comando 0x8500**:
```
Offset  Size  Campo                Tipo
0       1B    Command Type         UINT8 (0=Capture, 1=Video, ...)
1       1B    Channel ID           UINT8
2-3     2B    Command ID           UINT16
4       1B    Command Parameters   UINT8 (resolution, etc)
```

**Implementação**:
```go
func (api *API) HandleCameraCapture(w http.ResponseWriter, r *http.Request) {
    deviceID := r.URL.Query().Get("device")
    channel := r.URL.Query().Get("channel")
    
    // Construir comando
    cmd, err := protocol.BuildCameraCommandImmediate(
        deviceID,
        channel,
        1,  // Capture command
        0,  // Storage: Device
    )
    
    // Enviar via Session
    session := registry.Get(deviceID)
    session.Conn.Write(cmd)
    
    w.WriteHeader(http.StatusOK)
}
```

### 3.3 Comando: Stored Media Search (0x8600)

**Requisição**:
```
GET /media/search?device=000000000001&type=image&start=2024-01-01&end=2024-01-02
```

**Body do Comando 0x8600**:
```
Offset  Size  Campo                Tipo
0-5     6B    Start Time           BCD (YYMMDDHHMMSS)
6-11    6B    End Time             BCD (YYMMDDHHMMSS)
12      1B    Media Type           UINT8 (0=Image, 1=Audio, 2=Video)
13      1B    Channel Mask         UINT8 (bit mask de channels)
14-15   2B    Event Type           UINT16 (bit mask)
16-17   2B    Reserved             UINT16
```

**Implementação**:
```go
func (api *API) HandleMediaSearch(w http.ResponseWriter, r *http.Request) {
    startTime, _ := time.Parse("2006-01-02", r.URL.Query().Get("start"))
    endTime, _ := time.Parse("2006-01-02", r.URL.Query().Get("end"))
    
    cmd, err := protocol.BuildStoredMediaSearch(
        deviceID,
        startTime,
        endTime,
        0,  // Media type: Image
        0xFF,  // All channels
    )
    
    session := registry.Get(deviceID)
    session.Conn.Write(cmd)
}
```

## Parte 4: Validações Obrigatórias

### 4.1 Validação por Camada

**TCP Layer**:
- ✓ Conexão estabelecida
- ✓ Não há timeout de idle (> 60s sem dados)
- ✓ Buffer não estoura (max 64KB)

**Protocol Layer**:
- ✓ Delimitadores 0x7E presentes
- ✓ Header tem exatamente 13 bytes
- ✓ Checksum válido (XOR)
- ✓ Body length consistente com Properties
- ✓ Device ID válido (BCD)

**Application Layer**:
- ✓ Message ID suportado (0x0001-0x0FFF ou 0x8000-0x8FFF)
- ✓ Campos obrigatórios presentes
- ✓ Valores em intervalos válidos
- ✓ Device registrado no registry

### 4.2 Tratativa de Erro

```go
// Exemplo de validação completa:
func validateFrame(frame *protocol.PacketFrame) error {
    // 1. Device ID válido?
    if len(frame.Header.DeviceID) != 6 {
        return protocol.ErrInvalidDeviceID
    }
    
    // 2. Message ID suportado?
    if !isMessageIDSupported(frame.Header.MessageID) {
        return protocol.ErrUnsupportedMessageID
    }
    
    // 3. Body length consistente?
    expectedLen := frame.Header.Properties & 0x3FFF
    if len(frame.Body) != int(expectedLen) {
        return protocol.ErrBodyLengthMismatch
    }
    
    return nil
}
```

### 4.3 Logging de Erro

**Padrão de Log**:
```
[PROTOCOL] ERROR: Invalid checksum at frame 42
  Expected: 0xAB, Got: 0xCD
  Device: 000000000001
  Message: 0x0200 (Location Report)
  Buffer state: [7E 00 01 ... (truncated)]
```

## Parte 5: Performance e Segurança

### 5.1 Otimizações

**Buffer Management**:
- Preallocar slices de tamanho comum
- Reusar buffers via sync.Pool

**Parsing**:
- Evitar cópias desnecessárias
- Usar ponteiros para estruturas grandes

**Concorrência**:
- Uma goroutine por conexão TCP
- sync.RWMutex para registry

### 5.2 Segurança

**Validação**:
- ✓ Checksum obrigatório
- ✓ Limites de tamanho de body
- ✓ Timeout de idle (60s)
- ✓ Validação de Device ID

**Logs**:
- ✓ Todas as operações críticas registradas
- ✓ Dados sensíveis mascados
- ✓ Erros com stack trace (em debug)

## Resumo de Fluxo Completo

```
1. Dispositivo conecta em TCP 6207
2. Envia: 7E 00 01 [header + login body] CS 7E
3. Servidor recebe e acumula em buffer
4. Parser encontra 0x7E, unescapa, valida checksum
5. Header parseado → Message ID = 0x0001 (Login)
6. Body parseado → LoginMessage struct
7. Device registrado no registry
8. Responde: 0x8001 (General Response)
9. Dispositivo envia: 0x0003 (Heartbeat) a cada 30s
10. Servidor responde com 0x8001 confirmando
11. Dispositivo pode enviar: 0x0200 (Location), 0x0800 (Multimedia), etc
12. Servidor pode enviar: 0x8500 (Camera Cmd), 0x8600 (Media Search), etc
13. Conexão permanece ativa até Logout (0x0002) ou timeout
```

Este é o fluxo completo alinhado à especificação do fabricante JT808/JT1078.

