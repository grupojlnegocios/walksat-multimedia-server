# Especificação de Protocolo JT808/JT1078/JT1077

## 1. Estrutura de Pacote Base (JT808)

### Formato de Quadro (Frame)

```
┌──────────────────────────────────────────────────────────────────┐
│  FLAG  │ HEADER │ BODY │ CHECK │ FLAG │
├──────────────────────────────────────────────────────────────────┤
│  0x7E  │ 13 B   │ Var  │ 1 B   │ 0x7E │
└──────────────────────────────────────────────────────────────────┘
```

### 1.1 Delimitadores
- **FLAG (Start/End)**: `0x7E` - Define início e fim de quadro
- **Check (Checksum)**: 1 byte - XOR de todos os bytes no header e body

### 1.2 Escape (Enquadramento)
Quando encontrado `0x7E` ou `0x7D` dentro do header/body:
- `0x7E` → `0x7D 0x02`
- `0x7D` → `0x7D 0x01`

---

## 2. Estrutura de Header (13 bytes)

### 2.1 Layout em Bytes

```
Offset │ Bytes │ Nome              │ Tipo         │ Descrição
───────┼───────┼───────────────────┼──────────────┼────────────────────
0      │ 2     │ MsgID             │ uint16 (BE)  │ Identificador mensagem
2      │ 2     │ Properties        │ uint16 (BE)  │ Propriedades/Flags
4      │ 6     │ DeviceID          │ BCD (string) │ ID do dispositivo
10     │ 2     │ SequenceNum       │ uint16 (BE)  │ Número de sequência
12     │ 1     │ [TOTAL]           │ N/A          │ Comprimento do Body
```

### 2.2 Message ID (0x0000 - 0xFFFF)

#### Categorias JT808:
- **0x0001-0x0003**: Login, Logout, Heartbeat
- **0x0100-0x01FF**: Controle de Posicionamento
- **0x0200-0x02FF**: Relatórios de Posicionamento
- **0x0400-0x04FF**: Informações do Dispositivo
- **0x0500-0x05FF**: Parâmetros
- **0x0700-0x07FF**: Eventos/Alarmes
- **0x0800-0x0FFF**: Multimedia (JT808 ext.)

#### Categorias JT1078 (Estendido):
- **0x8800-0x8FFF**: Respostas do Platform → Device
- **0x9000-0x9FFF**: Comandos do Platform → Device

### 2.3 Properties (Word 16-bits, Big Endian)

```
Bit │ Campo
────┼─────────────────────────────────
15  │ Reserved
14  │ Response Required (1 = SIM)
13  │ Body Encryption (0=None, 1=Encrypted)
12  │ Reserved
11-10 │ Reserved
9-0 │ Body Length (0-1023 bytes, se ≤1023)
    │ Ou continuation flag (bit 9) + offset se >1023
```

### 2.4 Device ID (BCD Encoded - 6 bytes)
- Encoding: Binary Coded Decimal
- Formato: 12 dígitos hexadecimais (ex: "000000000001")
- Armazenado como 6 bytes BCD

### 2.5 Sequence Number (uint16, Big Endian)
- Incrementado a cada mensagem
- Usado para rastrear e ackowledges

---

## 3. Estrutura de Body (Variável)

### 3.1 Categorias de Body

#### A. Mensagens Simples (Login - 0x0001)
```
Offset │ Bytes │ Campo              │ Tipo
───────┼───────┼────────────────────┼─────────
0      │ 2     │ Province Code      │ uint16
2      │ 2     │ City Code          │ uint16
4      │ 2     │ Company ID         │ uint16
6      │ 5     │ Terminal Type      │ ASCII (5)
11     │ 20    │ Terminal ID        │ ASCII (20)
31     │ 4     │ License Plate Color│ ASCII (4)
35     │ 12    │ License Plate      │ ASCII (12)
```

#### B. Mensagens de Localização (0x0200)
```
Offset │ Bytes │ Campo              │ Tipo
───────┼───────┼────────────────────┼─────────
0      │ 4     │ Latitude           │ uint32 (1/1e6°)
4      │ 4     │ Longitude          │ uint32 (1/1e6°)
8      │ 2     │ Elevation          │ uint16 (m)
10     │ 2     │ Speed              │ uint16 (km/h)
12     │ 2     │ Direction          │ uint16 (°)
14     │ 6     │ Timestamp          │ BCD (YYMMDDHHMMSS)
20     │ Var   │ Extended Info      │ Opcional
```

#### C. Mensagens Multimedia (0x0800/0x0801)
- **0x0800**: Notificação de evento multimedia
- **0x0801**: Dados multimedia em pacotes

---

## 4. Enquadramento JT1078 (Protocolo de Vídeo)

### 4.1 Estrutura de Quadro

```
┌─────────────────────────────────────────────────────┐
│ Tipo Pacote │ Flags │ Timestamp │ Dados │ Checksum │
├─────────────────────────────────────────────────────┤
│ 1 byte      │ 1 B   │ 4 bytes   │ Var   │ 1 byte   │
└─────────────────────────────────────────────────────┘
```

### 4.2 Campos JT1078

- **Tipo Pacote (1 byte)**:
  - Bit 7-6: Tipo (01 = Áudio, 10 = Vídeo)
  - Bit 5: Marca de Finalização
  - Bit 4-0: Channel ID

- **Flags (1 byte)**:
  - Keyframe, CodecType (H264/H265), etc.

- **Timestamp (4 bytes, Big Endian)**:
  - Milissegundos desde o início do stream

- **Dados**: Carga útil (áudio/vídeo codec)

- **Checksum**: Validação de integridade

---

## 5. Tratativa de Pacotes em Go

### 5.1 Estrutura Recomendada - `internal/protocol/types.go`

```go
package protocol

import "time"

// PacketFrame representa um quadro completo
type PacketFrame struct {
    Flag      byte            // 0x7E
    Header    *PacketHeader   // 13 bytes
    Body      []byte          // 0-1023+ bytes
    Checksum  byte
    Timestamp time.Time       // Recebimento
}

// PacketHeader contém informações do quadro
type PacketHeader struct {
    MsgID          uint16
    Properties     uint16
    DeviceID       string  // BCD decodificado
    SequenceNum    uint16
    BodyLength     uint16
}

// Message Types
const (
    MsgLogin           uint16 = 0x0001
    MsgLogout          uint16 = 0x0002
    MsgHeartbeat       uint16 = 0x0003
    MsgLocation        uint16 = 0x0200
    MsgEventReport     uint16 = 0x0301
    MsgMultimediaEvent uint16 = 0x0800
    MsgMultimediaData  uint16 = 0x0801
)

// Response Types
const (
    MsgResponse           uint16 = 0x8001
    MsgMultimediaResponse uint16 = 0x8800
)

// PacketParser interface para diferentes protocolos
type PacketParser interface {
    Parse(data []byte) ([]*PacketFrame, error)
    Encode(frame *PacketFrame) []byte
}
```

### 5.2 Parser Principal - `internal/protocol/parser.go`

Funções essenciais:
- `DecodeBCD(data []byte) string`
- `EncodeBCD(str string) []byte`
- `CalculateChecksum(data []byte) byte`
- `ParseFrame(data []byte) (*PacketFrame, int, error)`
- `EncodeFrame(frame *PacketFrame) []byte`
- `Unescape(data []byte) []byte` (remover sequências 0x7D)
- `Escape(data []byte) []byte` (adicionar sequências 0x7D)

### 5.3 Session Handler - `internal/stream/packet_handler.go`

```go
type PacketHandler struct {
    buffer      []byte
    parser      protocol.PacketParser
    deviceID    string
    
    onFrame     func(*protocol.PacketFrame)
    onError     func(error)
    onDisconnect func()
}

func (h *PacketHandler) ProcessData(data []byte)
func (h *PacketHandler) SendPacket(frame *protocol.PacketFrame) error
```

---

## 6. Fluxo de Tratativa

### 6.1 Recebimento de Dados

```
TCP Data (raw bytes)
    ↓
Buffer + Escape Removal
    ↓
Find 0x7E Delimiters
    ↓
Extract Header (13 bytes)
    ↓
Validate Checksum
    ↓
Extract Body (n bytes)
    ↓
Create PacketFrame
    ↓
Message Handler
```

### 6.2 Envio de Dados

```
Create PacketFrame
    ↓
Populate Header + Body
    ↓
Calculate Checksum
    ↓
Escape Special Bytes
    ↓
Add Delimiters (0x7E)
    ↓
TCP Send
```

---

## 7. Tipos de Mensagem JT808

### Lado Terminal (Device → Server)
- **0x0001**: Login
- **0x0002**: Logout
- **0x0003**: Heartbeat
- **0x0200**: Location Report
- **0x0301**: Event Report
- **0x0800**: Multimedia Event
- **0x0801**: Multimedia Data

### Lado Platform (Server → Device)
- **0x8001**: General Response
- **0x8104**: Set Speed Limit
- **0x8201**: Control Device
- **0x8301**: Set Parameters
- **0x8800**: Multimedia Response

---

## 8. Exemplo Prático

### Pacote de Login (0x0001)

**Dados Brutos (sem escape):**
```
7E | 00 01 00 1C 00 00 00 00 00 01 00 02 | [Body de 28 bytes] | CS | 7E
```

**Decoded:**
- MsgID: 0x0001 (Login)
- Properties: 0x001C (28 bytes de body)
- DeviceID: "000000000001"
- SeqNum: 0x0002
- Body: 28 bytes de login info
- Checksum: Calcula-se com XOR

---

## 9. Validação

Implementar validações:
1. **Delimitadores**: Começar/terminar com 0x7E
2. **Header**: Exatamente 13 bytes
3. **Checksum**: XOR de header + body
4. **Body Length**: Máximo 1023 bytes por padrão
5. **Device ID**: Validar BCD format

---

## 10. Considerações de Performance

- Use buffering para entrada de rede
- Implemente state machine para parsing incremental
- Reutilize buffers onde possível
- Valide checksum apenas após frame completo

---

## Referências

- JT/T 808-2013: Vehicle Positioning and Information Protocol
- JT/T 1078-2014: Video Transmission Protocol
- JT/T 1077-2020: Audio Transmission Protocol (se aplicável)
