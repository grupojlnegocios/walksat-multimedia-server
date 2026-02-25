# Guia de Migração - Estrutura de Pacotes JT808

## Visão Geral

A estrutura do projeto foi refatorada para alinhar-se com a especificação JT808/JT1078/JT1077 do fabricante. As mudanças principais incluem:

1. **`protocol/types.go`**: Define tipos estruturados de pacotes
2. **`protocol/parser.go`**: Implementa parsing base de pacotes
3. **`protocol/jt808.go`**: Parser JT808 refatorado com compatibilidade retroativa

## Arquivos Criados/Modificados

### 1. `PROTOCOL_SPECIFICATION.md`
- Documentação completa do protocolo JT808/JT1078
- Estrutura de pacotes
- Tratativa de mensagens
- Exemplos práticos

### 2. `internal/protocol/types.go` (NOVO)
Define constantes e tipos para o protocolo:

```go
// Constantes de Message IDs
const (
    MsgLogin           = 0x0001
    MsgLogout          = 0x0002
    MsgHeartbeat       = 0x0003
    MsgLocationReport  = 0x0200
    MsgMultimediaEvent = 0x0800
    MsgMultimediaData  = 0x0801
)

// Estruturas de Pacote
type PacketFrame struct {
    Flag      byte
    Header    *PacketHeader
    Body      []byte
    Checksum  byte
    Timestamp time.Time
}

type PacketHeader struct {
    MsgID       uint16
    Properties  uint16
    DeviceID    string
    SequenceNum uint16
}
```

### 3. `internal/protocol/parser.go` (NOVO)
Implementa funcionalidades base:

```go
// Funções Principais
DecodeBCD(data []byte) (string, error)
EncodeBCD(str string) ([]byte, error)
Escape(data []byte) []byte
Unescape(data []byte) ([]byte, error)
CalculateChecksum(data []byte) byte

// Tipos
type BaseParser struct
type PacketParser interface
```

### 4. `internal/protocol/jt808.go` (REFATORADO)
Mantém compatibilidade com código existente:

```go
type JT808Parser struct {
    *BaseParser  // Herança da funcionalidade base
}

// Métodos compatíveis
func (p *JT808Parser) Push(data []byte) []*JT808Message

// Novos métodos para build de respostas
func BuildResponse(msgID uint16, deviceID string, seqNum uint16, body []byte) ([]byte, error)
func BuildGeneralResponse(deviceID string, seqNum uint16, ...) ([]byte, error)
func BuildCameraCommandImmediate(...) ([]byte, error)
```

## Guia de Uso

### Uso Existente (Compatível)

Código antigo continua funcionando:

```go
parser := protocol.NewJT808()
messages := parser.Push(data)

for _, msg := range messages {
    fmt.Printf("MsgID: 0x%04X, DeviceID: %s\n", msg.MessageID, msg.DeviceID)
}
```

### Uso Novo (Recomendado)

Aproveitar a nova estrutura:

```go
// Parser retorna PacketFrames internamente
parser := protocol.NewJT808()
messages := parser.Push(data)

// Acessar como antes (compatível)
for _, msg := range messages {
    fmt.Printf("MessageID: 0x%04X (%s)\n", 
        msg.MessageID, 
        protocol.GetMessageTypeName(msg.MessageID))
}
```

### Construir Respostas

**Antes:**
```go
response := protocol.BuildResponse(0x8001, deviceID, seqNum, body)
conn.Write(response)
```

**Depois:**
```go
response, err := protocol.BuildResponse(0x8001, deviceID, seqNum, body)
if err != nil {
    log.Fatal(err)
}
conn.Write(response)
```

### Funções de Comando

**Antes:**
```go
cmd := protocol.BuildCameraCommandImmediate(
    deviceID, 1, 5, 0, 0, 4, 5)
conn.Write(cmd)
```

**Depois:**
```go
cmd, err := protocol.BuildCameraCommandImmediate(
    deviceID, seqNum, 1, 5, 0, 0, 4, 5)
if err != nil {
    log.Fatal(err)
}
conn.Write(cmd)
```

## Migração de Componentes

### `internal/stream/jt808_session.go`

Usar novas estruturas:

```go
// Antes
func (s *JT808Session) handleMessage(msg *protocol.JT808Message) {
    switch msg.MessageID {
    case 0x0200:
        // Location report
    }
}

// Depois - Pode usar GetMessageTypeName para melhor logging
func (s *JT808Session) handleMessage(msg *protocol.JT808Message) {
    log.Printf("Received message: %s (0x%04X)\n",
        protocol.GetMessageTypeName(msg.MessageID),
        msg.MessageID)
        
    switch msg.MessageID {
    case 0x0200:
        // Location report
    }
}
```

### Senso de Campos de Mensagens

Para tipos conhecidos, pode-se fazer parsing estruturado:

```go
// Location Report (0x0200)
if msg.MessageID == 0x0200 && len(msg.Body) >= 20 {
    loc := &protocol.LocationReport{
        Latitude:  int32(binary.BigEndian.Uint32(msg.Body[0:4])),
        Longitude: int32(binary.BigEndian.Uint32(msg.Body[4:8])),
        Elevation: binary.BigEndian.Uint16(msg.Body[8:10]),
        Speed:     binary.BigEndian.Uint16(msg.Body[10:12]),
        Direction: binary.BigEndian.Uint16(msg.Body[12:14]),
    }
    
    deviceID, _ := protocol.DecodeBCD(msg.Body[14:20])
    log.Printf("Location: %d.%d, Speed: %d km/h", 
        loc.Latitude/1e6, loc.Longitude/1e6, loc.Speed)
}

// Multimedia Event (0x0800)
if msg.MessageID == 0x0800 && len(msg.Body) >= 9 {
    mmEvent := &protocol.MultimediaEventMessage{
        MultimediaID: binary.BigEndian.Uint32(msg.Body[0:4]),
        MediaType:    msg.Body[4],
        MediaFormat:  msg.Body[5],
        EventCode:    msg.Body[6],
        ChannelID:    msg.Body[7],
    }
    
    log.Printf("Multimedia Event: ID=%d, Type=%s, Format=%d, Event=%s",
        mmEvent.MultimediaID,
        protocol.GetMediaTypeName(mmEvent.MediaType),
        mmEvent.MediaFormat,
        protocol.GetEventCodeName(mmEvent.EventCode))
}
```

## Funções Auxiliares Disponíveis

```go
// Nomes legíveis
GetMessageTypeName(msgID uint16) string
GetMediaTypeName(mediaType byte) string
GetMediaFormatExt(format byte) string
GetEventCodeName(code byte) string

// Validação
ValidateChecksum(data []byte, checksum byte) bool

// Conversão
DecodeBCD(data []byte) (string, error)
EncodeBCD(str string) ([]byte, error)

// Frame Processing
Escape(data []byte) []byte
Unescape(data []byte) ([]byte, error)
CalculateChecksum(data []byte) byte
```

## Exemplo Completo - Tratativa de Pacote

```go
package main

import (
	"encoding/binary"
	"log"
	"net"
	"jt808-broker/internal/protocol"
)

func handleConnection(conn net.Conn) {
	defer conn.Close()
	
	parser := protocol.NewJT808()
	buffer := make([]byte, 4096)
	
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			break
		}
		
		// Parse incoming data
		messages := parser.Push(buffer[:n])
		
		for _, msg := range messages {
			log.Printf("[%s] From %s: %s\n",
				protocol.GetMessageTypeName(msg.MessageID),
				msg.DeviceID,
				conn.RemoteAddr())
			
			// Handle different message types
			switch msg.MessageID {
			case protocol.MsgLogin:
				handleLogin(conn, msg, parser)
			case protocol.MsgHeartbeat:
				handleHeartbeat(conn, msg, parser)
			case protocol.MsgLocationReport:
				handleLocation(conn, msg, parser)
			case protocol.MsgMultimediaEvent:
				handleMultimediaEvent(conn, msg, parser)
			}
		}
	}
}

func handleLogin(conn net.Conn, msg *protocol.JT808Message, parser *protocol.JT808Parser) {
	// Send general response (0x8001)
	response, err := protocol.BuildGeneralResponse(
		msg.DeviceID,
		msg.SeqNum,
		msg.MessageID,
		0, // Success
	)
	if err != nil {
		log.Printf("Error building response: %v\n", err)
		return
	}
	
	conn.Write(response)
	log.Printf("Login response sent to %s\n", msg.DeviceID)
}

func handleHeartbeat(conn net.Conn, msg *protocol.JT808Message, parser *protocol.JT808Parser) {
	// Send general response
	response, err := protocol.BuildGeneralResponse(
		msg.DeviceID,
		msg.SeqNum,
		msg.MessageID,
		0,
	)
	if err != nil {
		log.Printf("Error building response: %v\n", err)
		return
	}
	
	conn.Write(response)
}

func handleLocation(conn net.Conn, msg *protocol.JT808Message, parser *protocol.JT808Parser) {
	if len(msg.Body) < 20 {
		log.Printf("Invalid location report size: %d\n", len(msg.Body))
		return
	}
	
	lat := int32(binary.BigEndian.Uint32(msg.Body[0:4]))
	lon := int32(binary.BigEndian.Uint32(msg.Body[4:8]))
	speed := binary.BigEndian.Uint16(msg.Body[10:12])
	
	log.Printf("Location: %.6f, %.6f | Speed: %d km/h\n",
		float64(lat)/1e6,
		float64(lon)/1e6,
		speed)
	
	// Send response
	response, _ := protocol.BuildGeneralResponse(msg.DeviceID, msg.SeqNum, msg.MessageID, 0)
	conn.Write(response)
}

func handleMultimediaEvent(conn net.Conn, msg *protocol.JT808Message, parser *protocol.JT808Parser) {
	if len(msg.Body) < 9 {
		return
	}
	
	mmID := binary.BigEndian.Uint32(msg.Body[0:4])
	mediaType := msg.Body[4]
	format := msg.Body[5]
	eventCode := msg.Body[6]
	
	log.Printf("Multimedia Event: ID=%d, Type=%s, Format=%d, Event=%s\n",
		mmID,
		protocol.GetMediaTypeName(mediaType),
		format,
		protocol.GetEventCodeName(eventCode))
}

func main() {
	listener, _ := net.Listen("tcp", ":6207")
	defer listener.Close()
	
	for {
		conn, _ := listener.Accept()
		go handleConnection(conn)
	}
}
```

## Checklist de Migração

- [ ] Adicionar `protocol/types.go` ao projeto
- [ ] Adicionar `protocol/parser.go` ao projeto
- [ ] Atualizar `protocol/jt808.go` conforme especificado
- [ ] Testar compatibilidade com código existente
- [ ] Atualizar `stream/jt808_session.go` para usar novos tipos
- [ ] Adicionar logging com `GetMessageTypeName()`
- [ ] Implementar parsing estruturado de mensagens conhecidas
- [ ] Adicionar testes unitários para novos parsers
- [ ] Atualizar documentação do projeto
- [ ] Validar com dispositivos reais

## Benefícios da Nova Estrutura

1. **Type Safety**: Tipos estruturados em lugar de bytes soltos
2. **Manutenibilidade**: Código mais legível e fácil de debugar
3. **Extensibilidade**: Suporta facilmente novos tipos de mensagem
4. **Compatibilidade**: Funciona com código existente sem breaking changes
5. **Validação**: Checksum e validação integradas
6. **Performance**: Parsing incremental evita processamento duplicado
7. **Logging**: Funções para nomes legíveis de tipos

## Troubleshooting

### Checksum Inválido

```go
// Se houver erro de checksum, verificar:
log.Printf("Checksum received: 0x%02X", receivedChecksum)
log.Printf("Checksum calculated: 0x%02X", protocol.CalculateChecksum(data))

// Pode indicar corrupção de dados ou problema de escape
```

### Frame Incompleto

```go
// Ocorre quando não há dados suficientes
// Solução: aguardar mais dados antes de processar
frames, err := parser.Push(data)
if err == protocol.ErrIncompleteFrame {
    // Aguardar próximo pacote TCP
    continue
}
```

### Device ID Inválido

```go
// Erro de codificação BCD - caracteres não-numéricos
// Verificar se ID está sendo armazenado corretamente no dispositivo
```

## Referências

- Arquivo: `PROTOCOL_SPECIFICATION.md`
- Arquivo: `internal/protocol/types.go`
- Arquivo: `internal/protocol/parser.go`
- Arquivo: `internal/protocol/jt808.go`

