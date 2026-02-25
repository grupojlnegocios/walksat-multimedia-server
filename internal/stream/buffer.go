package stream

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"sync"
)

// ============================================================================
// JT1078 Stream Buffer - Persistente por Conexão
// ============================================================================

// JT1078StreamBuffer gerencia um buffer persistente para um stream de conexão
// Acumula dados TCP e extrai frames JT1078 completos
type JT1078StreamBuffer struct {
	mu             sync.Mutex
	connBuffer     bytes.Buffer                  // Buffer persistente da conexão
	maxBufferSize  int                           // Tamanho máximo do buffer (padrão 50MB)
	statistics     BufferStats                   // Estatísticas
	fragmentBuffer map[uint32]*FragmentAssembler // Por frame sequence
}

const (
	// JT/T 1078 Table 19 recomenda <= 950 bytes por payload.
	// Mantemos margem para variações de fabricante, mas rejeitamos valores absurdos
	// que causam "buffer lock" aguardando frames impossíveis.
	maxReasonableJT1078Payload = 4096
)

// BufferStats contém estatísticas do buffer
type BufferStats struct {
	BytesReceived  int64
	FramesReceived int
	FramesDropped  int
	ResyncCount    int
	LastResyncPos  int
}

// FragmentAssembler monta fragmentos de um único frame
type FragmentAssembler struct {
	FrameSequence uint32
	FrameIndex    uint16 // Fragment index
	FrameCount    uint16 // Total fragments
	Buffer        bytes.Buffer
	ExpectedSize  int
	ReceivedCount int
	Timestamp     int64
}

// JT1078FrameHeader com suporte a fragmentação
type JT1078FrameHeaderEx struct {
	// Header fixo (primeiro 4 bytes + campos variáveis)
	SyncWord  [4]byte // Always: 0x30 0x31 0x63 0x64
	DeviceID  [6]byte // BCD encoded
	ChannelID uint8

	// Campos adicionais (podem estar presentes)
	HasInterval      bool
	HasFrameSeq      bool
	HasFragmentIndex bool
	HasFragmentCount bool

	// Valores opcionais
	Interval      uint16 // Se HasInterval
	FrameSequence uint32 // Crescente
	FragmentIndex uint16 // Se HasFragmentIndex
	FragmentCount uint16 // Se HasFragmentCount

	// Campos obrigatórios
	DataLength uint32 // Tamanho dos dados do frame
	Timestamp  uint64
	HeaderSize int // Tamanho total do header parseado
}

// NewJT1078StreamBuffer cria um novo buffer de stream
func NewJT1078StreamBuffer() *JT1078StreamBuffer {
	return &JT1078StreamBuffer{
		maxBufferSize:  50 * 1024 * 1024, // 50MB default
		fragmentBuffer: make(map[uint32]*FragmentAssembler),
	}
}

// Append adiciona novos dados ao buffer
func (sb *JT1078StreamBuffer) Append(data []byte) error {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	// Verificar limite de buffer
	if sb.connBuffer.Len()+len(data) > sb.maxBufferSize {
		log.Printf("[STREAM_BUFFER] WARNING: Buffer overflow, current: %d, adding: %d, max: %d\n",
			sb.connBuffer.Len(), len(data), sb.maxBufferSize)
		// Descartar 25% do buffer para liberar espaço
		content := sb.connBuffer.Bytes()
		discard := len(content) / 4
		sb.connBuffer = bytes.Buffer{}
		sb.connBuffer.Write(content[discard:])
		sb.statistics.FramesDropped += discard / 1024 // Aproximação
		log.Printf("[STREAM_BUFFER] Discarded %d bytes, buffer now: %d\n", discard, sb.connBuffer.Len())
	}

	sb.connBuffer.Write(data)
	sb.statistics.BytesReceived += int64(len(data))
	return nil
}

// ExtractFrames extrai todos os frames completos disponíveis
func (sb *JT1078StreamBuffer) ExtractFrames() ([][]byte, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	var frames [][]byte
	content := sb.connBuffer.Bytes()

	originalLen := len(content)
	log.Printf("[STREAM_BUFFER] ExtractFrames: buffer has %d bytes\n", originalLen)

	for len(content) > 0 {
		// Procurar pelo sync word (0x30 0x31 0x63 0x64)
		headerPos := sb.findNextHeader(content)
		if headerPos < 0 {
			// Nenhum header válido encontrado
			if len(content) > 100000 {
				// Se buffer tem muito lixo, descartar e resyncar
				log.Printf("[STREAM_BUFFER] No valid header in %d bytes, resyncing...\n", len(content))
				sb.connBuffer = bytes.Buffer{}
				sb.statistics.ResyncCount++
				break
			}
			// Manter dados, pode ser header incompleto
			log.Printf("[STREAM_BUFFER] No complete header found, keeping %d bytes in buffer\n", len(content))
			break
		}

		if headerPos > 0 {
			// Descartar lixo antes do header
			log.Printf("[STREAM_BUFFER] Skipped %d bytes of garbage before header\n", headerPos)
			sb.statistics.ResyncCount++
			content = content[headerPos:]
		}

		// Parse header com fragmentação
		header, headerSize, err := sb.parseFrameHeader(content)
		if err != nil {
			if err.Error() == "incomplete_header" {
				// Header incompleto, aguardar mais dados
				log.Printf("[STREAM_BUFFER] Header incomplete, keeping %d bytes\n", len(content))
				break
			}
			// Header inválido, skip 1 byte e tentar novamente
			log.Printf("[STREAM_BUFFER] Invalid header: %v, skipping 1 byte\n", err)
			content = content[1:]
			sb.statistics.ResyncCount++
			continue
		}

		// Calcular tamanho total do pacote
		packetSize := headerSize + int(header.DataLength)
		log.Printf("[STREAM_BUFFER] Found frame header: headerSize=%d, dataLen=%d, total=%d\n",
			headerSize, header.DataLength, packetSize)

		if len(content) < packetSize {
			// Heurística de ressincronização:
			// se já existe um novo syncword antes de completar o pacote esperado,
			// o header atual provavelmente está corrompido/fora de alinhamento.
			if nextHeaderPos := sb.findNextHeader(content[1:]); nextHeaderPos >= 0 {
				realPos := nextHeaderPos + 1
				if realPos < len(content) {
					log.Printf("[STREAM_BUFFER] Resync: detected nested header at +%d while waiting for %d bytes, skipping current header\n",
						realPos, packetSize)
					content = content[realPos:]
					sb.statistics.ResyncCount++
					continue
				}
			}

			// Pacote incompleto, aguardar mais dados
			log.Printf("[STREAM_BUFFER] Incomplete frame: need %d bytes, have %d, waiting...\n",
				packetSize, len(content))
			break
		}

		// Extrair frame completo
		log.Printf("[STREAM_BUFFER] ✓ Frame complete! Extracting %d bytes\n", packetSize)
		frameData := make([]byte, packetSize)
		copy(frameData, content[:packetSize])

		// LOG: Mostrar primeiros bytes do frame para debug
		log.Printf("[STREAM_BUFFER] Frame data: header=% X\n", frameData[:min(len(frameData), 40)])

		frames = append(frames, frameData)
		sb.statistics.FramesReceived++

		// Avançar buffer
		content = content[packetSize:]
	}

	// Atualizar buffer se removermos dados
	if len(frames) > 0 {
		consumed := originalLen - len(content)
		log.Printf("[STREAM_BUFFER] Extracted %d frames, consumed %d bytes, %d bytes remaining\n",
			len(frames), consumed, len(content))
		sb.connBuffer = bytes.Buffer{}
		if len(content) > 0 {
			sb.connBuffer.Write(content)
		}
	} else {
		log.Printf("[STREAM_BUFFER] No complete frames extracted, buffer unchanged: %d bytes\n", len(content))
	}

	return frames, nil
}

// findNextHeader procura pelo sync word 0x30 0x31 0x63 0x64
func (sb *JT1078StreamBuffer) findNextHeader(data []byte) int {
	syncWord := []byte{0x30, 0x31, 0x63, 0x64}

	for i := 0; i <= len(data)-4; i++ {
		if bytes.Equal(data[i:i+4], syncWord) {
			return i
		}
	}
	return -1
}

// parseFrameHeader extrai header JT/T 1078-2016 padrão (30 bytes fixo)
// FORMATO JT/T 1078-2016 RTP-BASED (30 bytes):
// [0-3]   Sync (0x30 0x31 0x63 0x64)
// [4]     V_P_X_CC (RTP version/padding/extension/CSRC count)
// [5]     M_PT (RTP marker/payload type)
// [6-7]   PacketSN (sequence number)
// [8-13]  Device ID (6 bytes BCD)
// [14]    Channel ID
// [15]    DataType_Mark
// [16-23] Timestamp (8 bytes)
// [24-25] LastIFrameInterval
// [26-27] LastFrameInterval
// [28-29] DataBodyLength
// [30+]   Payload
//
// Retorna: (header, headerSize, error)
func (sb *JT1078StreamBuffer) parseFrameHeader(data []byte) (*JT1078FrameHeaderEx, int, error) {
	// Padrão JT/T 1078-2016: 30 bytes de header fixo
	const headerSize = 30
	if len(data) < headerSize {
		return nil, 0, fmt.Errorf("incomplete_header")
	}

	header := &JT1078FrameHeaderEx{}

	// Bytes 0-3: Sync word (obrigatório)
	copy(header.SyncWord[:], data[0:4])
	if header.SyncWord != [4]byte{0x30, 0x31, 0x63, 0x64} {
		return nil, 0, fmt.Errorf("invalid_sync_word")
	}

	// Skip bytes 4-5 (V_P_X_CC, M_PT) - parser JT1078Header cuidará deles
	// Skip byte 6-7 (PacketSN)

	// Bytes 8-13: Device ID (6 bytes BCD)
	copy(header.DeviceID[:], data[8:14])

	// Byte 14: Channel ID
	header.ChannelID = data[14]

	// Skip byte 15 (DataType_Mark) - será tratado em media_listener

	// Bytes 16-23: Timestamp (8 bytes)
	header.Timestamp = binary.BigEndian.Uint64(data[16:24])

	// Skip bytes 24-27 (LastIFrameInterval, LastFrameInterval)

	// Bytes 28-29: DataBodyLength (tamanho da payload)
	header.DataLength = uint32(binary.BigEndian.Uint16(data[28:30]))

	// Validação
	if header.DataLength == 0 || header.DataLength > maxReasonableJT1078Payload {
		log.Printf("[STREAM_BUFFER] Invalid DataLength: %d\n", header.DataLength)
		return nil, 0, fmt.Errorf("invalid_data_length: %d", header.DataLength)
	}

	header.HeaderSize = headerSize
	log.Printf("[STREAM_BUFFER] Parsed 30-byte JT1078 header: DataLen=%d\n", header.DataLength)
	return header, headerSize, nil
}

// GetStatistics retorna estatísticas do buffer
func (sb *JT1078StreamBuffer) GetStatistics() BufferStats {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	stats := sb.statistics
	stats.LastResyncPos = sb.connBuffer.Len()
	return stats
}

// Reset limpa o buffer
func (sb *JT1078StreamBuffer) Reset() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.connBuffer.Reset()
	sb.fragmentBuffer = make(map[uint32]*FragmentAssembler)
	sb.statistics = BufferStats{}
}

// CurrentSize retorna tamanho atual do buffer
func (sb *JT1078StreamBuffer) CurrentSize() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.connBuffer.Len()
}
