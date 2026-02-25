package stream

import (
	"encoding/binary"
	"testing"
)

// TestStreamBufferAccumulation verifica se o buffer acumula dados persistentemente
func TestStreamBufferAccumulation(t *testing.T) {
	buf := NewJT1078StreamBuffer()

	// Simular recebimento gradual de um frame
	// Frame total: 130 bytes (30 header + 100 data)

	// Criar header válido com syncword
	header := make([]byte, 30)
	header[0] = 0x30
	header[1] = 0x31
	header[2] = 0x63
	header[3] = 0x64

	header[4] = 0x21                                // V=2, CC=1
	header[5] = 0x62                                // M=0, PT=98
	binary.BigEndian.PutUint16(header[6:8], 0x027E) // PacketSN
	copy(header[8:14], []byte{0x01, 0x19, 0x93, 0x49, 0x36, 0x43})
	header[14] = 0x01 // Channel
	header[15] = 0x10 // DataType(video P-frame=1), Mark(atomic=0)
	binary.BigEndian.PutUint64(header[16:24], 0x0000000000000001)
	binary.BigEndian.PutUint16(header[24:26], 0)
	binary.BigEndian.PutUint16(header[26:28], 40)
	binary.BigEndian.PutUint16(header[28:30], 100)

	// Dados fictícios
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i % 256)
	}

	completeFrame := append(header, data...)

	// Teste 1: Dividir frame em chunks
	chunk1 := completeFrame[:30]
	chunk2 := completeFrame[30:80]
	chunk3 := completeFrame[80:]

	// Adicionar chunk 1
	if err := buf.Append(chunk1); err != nil {
		t.Fatalf("Failed to append chunk 1: %v", err)
	}

	// Tentar extrair (deve retornar vazio)
	frames1, err := buf.ExtractFrames()
	if err != nil {
		t.Fatalf("Error extracting frames after chunk 1: %v", err)
	}
	if len(frames1) != 0 {
		t.Errorf("Should not extract frames after chunk 1, got %d", len(frames1))
	}

	// Adicionar chunk 2
	if err := buf.Append(chunk2); err != nil {
		t.Fatalf("Failed to append chunk 2: %v", err)
	}

	// Tentar extrair (deve retornar vazio)
	frames2, err := buf.ExtractFrames()
	if err != nil {
		t.Fatalf("Error extracting frames after chunk 2: %v", err)
	}
	if len(frames2) != 0 {
		t.Errorf("Should not extract frames after chunk 2, got %d", len(frames2))
	}

	// Adicionar chunk 3
	if err := buf.Append(chunk3); err != nil {
		t.Fatalf("Failed to append chunk 3: %v", err)
	}

	// Agora deve extrair 1 frame completo
	frames3, err := buf.ExtractFrames()
	if err != nil {
		t.Fatalf("Error extracting frames after chunk 3: %v", err)
	}
	if len(frames3) != 1 {
		t.Errorf("Should extract 1 frame after chunk 3, got %d", len(frames3))
	}

	// Verificar tamanho do frame
	if len(frames3[0]) != len(completeFrame) {
		t.Errorf("Frame size mismatch: expected %d, got %d", len(completeFrame), len(frames3[0]))
	}

	// Verificar conteúdo
	for i, b := range frames3[0] {
		if b != completeFrame[i] {
			t.Errorf("Frame data mismatch at index %d: expected 0x%02x, got 0x%02x",
				i, completeFrame[i], b)
			break
		}
	}

	t.Logf("✓ Buffer correctly accumulated and extracted frame")
}

// TestStreamBufferMultipleFrames verifica extração de múltiplos frames
func TestStreamBufferMultipleFrames(t *testing.T) {
	buf := NewJT1078StreamBuffer()

	// Criar 3 frames pequenos
	frames := make([][]byte, 3)
	for i := 0; i < 3; i++ {
		header := make([]byte, 30)
		header[0] = 0x30
		header[1] = 0x31
		header[2] = 0x63
		header[3] = 0x64

		header[4] = 0x21
		header[5] = 0x62
		binary.BigEndian.PutUint16(header[6:8], uint16(0x0100+i))
		copy(header[8:14], []byte{0x01, 0x19, 0x93, 0x49, 0x36, 0x43})
		header[14] = 0x01
		header[15] = 0x10
		binary.BigEndian.PutUint64(header[16:24], 1)
		binary.BigEndian.PutUint16(header[24:26], 0)
		binary.BigEndian.PutUint16(header[26:28], 40)
		binary.BigEndian.PutUint16(header[28:30], 50)

		data := make([]byte, 50)
		for j := range data {
			data[j] = byte((i*50 + j) % 256)
		}

		frames[i] = append(header, data...)
	}

	// Adicionar todos os dados de uma vez
	allData := append(append(frames[0], frames[1]...), frames[2]...)
	if err := buf.Append(allData); err != nil {
		t.Fatalf("Failed to append data: %v", err)
	}

	// Extrair
	extracted, err := buf.ExtractFrames()
	if err != nil {
		t.Fatalf("Error extracting frames: %v", err)
	}

	if len(extracted) != 3 {
		t.Errorf("Expected 3 frames, got %d", len(extracted))
	}

	for i := 0; i < len(extracted) && i < 3; i++ {
		if len(extracted[i]) != len(frames[i]) {
			t.Errorf("Frame %d size mismatch: expected %d, got %d",
				i, len(frames[i]), len(extracted[i]))
		}
	}

	t.Logf("✓ Buffer correctly extracted %d frames", len(extracted))
}

// TestStreamBufferResynchonization verifica resincronização com lixo
func TestStreamBufferResynchronization(t *testing.T) {
	buf := NewJT1078StreamBuffer()

	// Adicionar lixo
	garbage := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	if err := buf.Append(garbage); err != nil {
		t.Fatalf("Failed to append garbage: %v", err)
	}

	// Criar frame válido
	header := make([]byte, 30)
	header[0] = 0x30
	header[1] = 0x31
	header[2] = 0x63
	header[3] = 0x64

	header[4] = 0x21
	header[5] = 0x62
	binary.BigEndian.PutUint16(header[6:8], 0x027E)
	copy(header[8:14], []byte{0x01, 0x19, 0x93, 0x49, 0x36, 0x43})
	header[14] = 0x01
	header[15] = 0x10
	binary.BigEndian.PutUint64(header[16:24], 1)
	binary.BigEndian.PutUint16(header[24:26], 0)
	binary.BigEndian.PutUint16(header[26:28], 40)
	binary.BigEndian.PutUint16(header[28:30], 50)

	data := make([]byte, 50)
	frame := append(header, data...)

	// Adicionar lixo + frame
	junkAndFrame := append([]byte{0xAA, 0xBB, 0xCC}, frame...)
	if err := buf.Append(junkAndFrame); err != nil {
		t.Fatalf("Failed to append junk and frame: %v", err)
	}

	// Extrair (deve pular lixo e encontrar frame)
	extracted, err := buf.ExtractFrames()
	if err != nil {
		t.Fatalf("Error extracting frames: %v", err)
	}

	if len(extracted) != 1 {
		t.Errorf("Expected 1 frame after resynchronization, got %d", len(extracted))
	}

	stats := buf.GetStatistics()
	if stats.ResyncCount == 0 {
		t.Errorf("Expected at least 1 resync, got %d", stats.ResyncCount)
	}

	t.Logf("✓ Buffer correctly resynchronized and extracted frame (resyncs: %d)", stats.ResyncCount)
}
