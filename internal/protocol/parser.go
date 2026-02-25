package protocol

import (
	"encoding/binary"
	"fmt"
	"log"
	"time"
)

// ============================================================================
// Funções Auxiliares de Conversão
// ============================================================================

// DecodeBCD converte dados em formato BCD para string
// Cada byte contém 2 dígitos decimais (nibbles alto e baixo)
func DecodeBCD(data []byte) (string, error) {
	if len(data) == 0 {
		return "", ErrInvalidBCD
	}

	result := ""
	for _, b := range data {
		high := (b >> 4) & 0x0F
		low := b & 0x0F

		// Validar dígitos BCD válidos (0-9)
		if high > 9 || low > 9 {
			return "", ErrInvalidBCD
		}

		result += fmt.Sprintf("%d%d", high, low)
	}

	log.Printf("[PROTOCOL] BCD decoded: % X → %s\n", data, result)
	return result, nil
}

// EncodeBCD converte string para formato BCD
// Retorna erro se houver caracteres não-numéricos
func EncodeBCD(str string) ([]byte, error) {
	if len(str)%2 != 0 {
		str = "0" + str // Pad com 0 à esquerda se necessário
	}

	result := make([]byte, len(str)/2)
	for i := 0; i < len(str); i += 2 {
		high := str[i] - '0'
		low := str[i+1] - '0'

		if high > 9 || low > 9 {
			return nil, ErrInvalidBCD
		}

		result[i/2] = (high << 4) | low
	}

	log.Printf("[PROTOCOL] BCD encoded: %s → % X\n", str, result)
	return result, nil
}

// ============================================================================
// Funções de Escape/Unescape
// ============================================================================

// Escape adiciona sequências de escape para bytes especiais
// 0x7E → 0x7D 0x02
// 0x7D → 0x7D 0x01
func Escape(data []byte) []byte {
	result := make([]byte, 0, len(data)*2) // Pre-allocate potential max size

	for _, b := range data {
		switch b {
		case 0x7E:
			result = append(result, 0x7D, 0x02)
		case 0x7D:
			result = append(result, 0x7D, 0x01)
		default:
			result = append(result, b)
		}
	}

	return result
}

// Unescape remove sequências de escape
// 0x7D 0x02 → 0x7E
// 0x7D 0x01 → 0x7D
func Unescape(data []byte) ([]byte, error) {
	result := make([]byte, 0, len(data))

	for i := 0; i < len(data); i++ {
		if data[i] == 0x7D && i+1 < len(data) {
			next := data[i+1]
			if next == 0x01 {
				result = append(result, 0x7D)
				i++ // Skip next byte
			} else if next == 0x02 {
				result = append(result, 0x7E)
				i++ // Skip next byte
			} else {
				return nil, fmt.Errorf("invalid escape sequence: 0x7D 0x%02X", next)
			}
		} else {
			result = append(result, data[i])
		}
	}

	return result, nil
}

// ============================================================================
// Funções de Checksum
// ============================================================================

// CalculateChecksum calcula o XOR de todos os bytes
// Usado para validação de integridade de frame
func CalculateChecksum(data []byte) byte {
	checksum := byte(0)
	for _, b := range data {
		checksum ^= b
	}
	return checksum
}

// ValidateChecksum verifica se o checksum está correto
func ValidateChecksum(data []byte, checksum byte) bool {
	calculated := CalculateChecksum(data)
	return calculated == checksum
}

// ============================================================================
// Parser Base JT808
// ============================================================================

// BaseParser implementa funcionalidades comuns de parsing
type BaseParser struct {
	buffer   []byte
	deviceID string
}

// NewBaseParser creates a new BaseParser instance
func NewBaseParser() *BaseParser {
	return &BaseParser{
		buffer:   make([]byte, 0, 4096), // Pre-allocate 4KB buffer
		deviceID: "",
	}
}

// Push adiciona novos dados ao buffer e retorna frames completos
func (p *BaseParser) Push(data []byte) ([]*PacketFrame, error) {
	p.buffer = append(p.buffer, data...)
	var frames []*PacketFrame
	var err error

	// Processar múltiplos frames no buffer
	for len(p.buffer) > 0 {
		frame, consumed, parseErr := p.parseNextFrame()

		if parseErr != nil {
			if parseErr == ErrIncompleteFrame {
				// Frame incompleto, aguardar mais dados
				break
			}
			// Erro de parsing, remover dados até o próximo delimitador
			log.Printf("[PROTOCOL] Parse error: %v, skipping to next delimiter\n", parseErr)
			if idx := findNextDelimiter(p.buffer, 1); idx >= 0 {
				p.buffer = p.buffer[idx:]
			} else {
				p.buffer = p.buffer[0:0] // Limpar buffer
				err = parseErr
				break
			}
			continue
		}

		if frame != nil {
			frames = append(frames, frame)
			p.buffer = p.buffer[consumed:]

			// Atualizar device ID se encontrado
			if p.deviceID == "" && frame.Header != nil {
				p.deviceID = frame.Header.DeviceID
			}
		} else {
			break
		}
	}

	return frames, err
}

// parseNextFrame tenta extrair um frame completo do buffer
// Retorna: (frame, bytes_consumed, error)
func (p *BaseParser) parseNextFrame() (*PacketFrame, int, error) {
	if len(p.buffer) < 2 {
		return nil, 0, ErrIncompleteFrame
	}

	// Procurar pelo delimitador de início
	if p.buffer[0] != FrameDelimiter {
		return nil, 0, fmt.Errorf("frame does not start with 0x7E")
	}

	// Procurar pelo delimitador de fim
	endIdx := findNextDelimiter(p.buffer, 1)
	if endIdx < 0 {
		return nil, 0, ErrIncompleteFrame
	}

	// Extrair dados entre delimitadores (sem incluir os delimitadores)
	frameData := p.buffer[1:endIdx]
	log.Printf("[PROTOCOL] Frame data extracted: %d bytes\n", len(frameData))

	// Fazer unescape dos dados
	unescaped, err := Unescape(frameData)
	if err != nil {
		return nil, endIdx + 1, fmt.Errorf("failed to unescape frame: %w", err)
	}

	// Frame deve ter no mínimo: 12 bytes (header) + 1 byte (checksum)
	if len(unescaped) < 13 {
		return nil, endIdx + 1, ErrInvalidFrame
	}

	// Separar checksum (último byte)
	checksum := unescaped[len(unescaped)-1]
	dataWithHeader := unescaped[:len(unescaped)-1]

	// Validar checksum
	if !ValidateChecksum(dataWithHeader, checksum) {
		calculatedChecksum := CalculateChecksum(dataWithHeader)
		log.Printf("[PROTOCOL] Checksum mismatch: got 0x%02X, calculated 0x%02X\n", checksum, calculatedChecksum)
		return nil, endIdx + 1, ErrInvalidChecksum
	}

	// Parsear header (primeiros 12 bytes)
	headerData := dataWithHeader[:12]
	log.Printf("[PROTOCOL] Header bytes (hex): % X\n", headerData)

	header, err := p.parseHeader(headerData)
	if err != nil {
		return nil, endIdx + 1, err
	}

	// Extrair body
	var body []byte
	if len(dataWithHeader) > 12 {
		body = dataWithHeader[12:]
	}

	// Validar comprimento do body
	bodyLen := header.GetBodyLength()
	if uint16(len(body)) != bodyLen {
		log.Printf("[PROTOCOL] Body length mismatch: expected %d, got %d\n", bodyLen, len(body))
		return nil, endIdx + 1, fmt.Errorf("body length mismatch")
	}

	frame := &PacketFrame{
		Flag:      FrameDelimiter,
		Header:    header,
		Body:      body,
		Checksum:  checksum,
		Timestamp: time.Now(),
		Raw:       frameData,
	}

	log.Printf("[PROTOCOL] Frame parsed successfully: MsgID=0x%04X, DeviceID=%s, SeqNum=%d, BodyLen=%d\n",
		header.MsgID, header.DeviceID, header.SequenceNum, bodyLen)

	return frame, endIdx + 1, nil
}

// parseHeader extrai e parseia os 12 bytes de header JT808
func (p *BaseParser) parseHeader(data []byte) (*PacketHeader, error) {
	if len(data) != 12 {
		return nil, ErrInvalidHeader
	}

	// Extrair campos
	msgID := binary.BigEndian.Uint16(data[0:2])
	properties := binary.BigEndian.Uint16(data[2:4])

	// Decodificar Device ID (6 bytes BCD)
	deviceID, err := DecodeBCD(data[4:10])
	if err != nil {
		return nil, fmt.Errorf("failed to decode device ID: %w", err)
	}

	seqNum := binary.BigEndian.Uint16(data[10:12])

	// Body length está embutido em properties (bits 0-9)
	bodyLen := properties & PropBodyLengthMask

	header := &PacketHeader{
		MsgID:       msgID,
		Properties:  properties,
		DeviceID:    deviceID,
		SequenceNum: seqNum,
	}

	log.Printf("[PROTOCOL] Header parsed: MsgID=0x%04X (%s), Props=0x%04X, DeviceID=%s, SeqNum=%d, BodyLen=%d\n",
		msgID, GetMessageTypeName(msgID), properties, deviceID, seqNum, bodyLen)

	return header, nil
}

// SetDeviceID define o ID do dispositivo
func (p *BaseParser) SetDeviceID(deviceID string) {
	p.deviceID = deviceID
	log.Printf("[PROTOCOL] Device ID set: %s\n", deviceID)
}

// GetDeviceID retorna o ID do dispositivo
func (p *BaseParser) GetDeviceID() string {
	return p.deviceID
}

// ============================================================================
// Funções Utilitárias
// ============================================================================

// findNextDelimiter procura pelo próximo delimitador (0x7E) a partir do offset
// Retorna o índice ou -1 se não encontrado
func findNextDelimiter(data []byte, startOffset int) int {
	for i := startOffset; i < len(data); i++ {
		if data[i] == FrameDelimiter {
			return i
		}
	}
	return -1
}

// EncodeFrame converte um PacketFrame em bytes para transmissão
func (p *BaseParser) Encode(frame *PacketFrame) ([]byte, error) {
	if frame == nil || frame.Header == nil {
		return nil, fmt.Errorf("invalid frame")
	}

	// Construir header (13 bytes)
	headerBuf := make([]byte, 12)

	// MsgID
	binary.BigEndian.PutUint16(headerBuf[0:2], frame.Header.MsgID)

	// Properties (contains body length in bits 0-9)
	bodyLen := uint16(len(frame.Body))
	if bodyLen > 1023 {
		return nil, ErrBodyTooLarge
	}
	properties := (frame.Header.Properties & 0xFC00) | bodyLen
	binary.BigEndian.PutUint16(headerBuf[2:4], properties)

	// Device ID (BCD encoded, 6 bytes)
	deviceIDBCD, err := EncodeBCD(frame.Header.DeviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to encode device ID: %w", err)
	}
	if len(deviceIDBCD) != 6 {
		return nil, fmt.Errorf("device ID must be 12 hex digits")
	}
	copy(headerBuf[4:10], deviceIDBCD)

	// Sequence Number
	binary.BigEndian.PutUint16(headerBuf[10:12], frame.Header.SequenceNum)

	// Combinar header e body
	frameData := append(headerBuf, frame.Body...)

	// Calcular checksum
	checksum := CalculateChecksum(frameData)
	frameData = append(frameData, checksum)

	// Fazer escape dos dados
	escaped := Escape(frameData)

	// Adicionar delimitadores
	result := append([]byte{FrameDelimiter}, escaped...)
	result = append(result, FrameDelimiter)

	log.Printf("[PROTOCOL] Frame encoded: %d bytes (raw), %d bytes (escaped)\n", len(frameData), len(result))

	return result, nil
}
