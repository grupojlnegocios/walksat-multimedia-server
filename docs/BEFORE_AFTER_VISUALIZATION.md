# Antes vs Depois - Visualização da Solução

## O Problema (Antes)

```
╔═════════════════════════════════════════════════════════════════════╗
║                 FLUXO ERRADO - Descartando Dados                   ║
╚═════════════════════════════════════════════════════════════════════╝

TCP Layer
─────────
[692 bytes]  [1704 bytes]  [2006 bytes]  [2216 bytes]  [3000 bytes] ...

         ↓                   ↓               ↓              ↓
    
Reader.Read()  (cada read() obtém um chunk diferente)
         ↓
    
Parser.Push()  (tenta parsear cada chunk individualmente)
    
Chunk 1 (692 bytes):
┌─────────────────────────────────────────────────────────────┐
│ 30 31 63 64 81 62 02 8F 01 19 93 49 36 43 01 11 00 ...   │
│ ↑  ↑  ↑  ↑                                                   │
│ Sync Word        DataLen = 61441                           │
│                                                              │
│ Header diz: preciso de 61441 bytes                          │
│ Tenho: 692 bytes                                            │
│ Status: INCOMPLETO ❌                                        │
│                                                              │
│ ✓ ParsedFrames = 0                                           │
│ ✓ Buffer = DESCARTADO (!!!)  ← PROBLEMA AQUI                │
└─────────────────────────────────────────────────────────────┘
         ↓
    
Chunk 2 (1704 bytes):
┌─────────────────────────────────────────────────────────────┐
│ [novo chunk, começa do zero]                                │
│ Nem vê header anterior                                      │
│                                                              │
│ [1704 bytes]                                                │
│ Status: INCOMPLETO ❌                                        │
│                                                              │
│ ✓ ParsedFrames = 0                                           │
│ ✓ Buffer = DESCARTADO (!!!)  ← NOVAMENTE DESCARTA            │
└─────────────────────────────────────────────────────────────┘

... REPETIÇÃO INFINITA ...

Nunca acumula 61441 bytes
Nunca completa um frame
Parser nunca consegue extrair dados H.264 válidos


RESULTADO FINAL:
❌ SIM parsing fails (offset errado, BCD inválido)
❌ Channel = 147 (lixo)
❌ DataLen = 0
❌ FFmpeg recebe nada
❌ MediaMTX não funciona
```

---

## A Solução (Depois)

```
╔═════════════════════════════════════════════════════════════════════╗
║              FLUXO CORRETO - Buffer Persistente                     ║
╚═════════════════════════════════════════════════════════════════════╝

TCP Layer
─────────
[692 bytes]  [1704 bytes]  [2006 bytes]  [2216 bytes]  [3000 bytes] ...

         ↓                   ↓               ↓              ↓
    
Reader.Read()  (cada read() obtém um chunk diferente)
         ↓
    
connBuffer.Append()  (ACUMULA SEMPRE, NUNCA DESCARTA)
    
    ┌──────────────────────────────────────────────────────────────────┐
    │ PERSISTENT BUFFER IN MEMORY - Parte do JT1078StreamBuffer         │
    └──────────────────────────────────────────────────────────────────┘

Chunk 1 (692 bytes):
┌──────────────────────────────────────────────────────────────────┐
│ connBuffer.Append([692 bytes])                                    │
│                                                                   │
│ Buffer State:                                                     │
│ ┌────────────────────────────────────────────────────────────┐   │
│ │ 30 31 63 64 81 62 02 8F 01 19 93 49 36 43 01 11 00 ...   │   │
│ │ └─── 692 bytes em buffer                                  │   │
│ └────────────────────────────────────────────────────────────┘   │
│                                                                   │
│ ExtractFrames():                                                  │
│   Header diz: DataLen = 61441                                    │
│   Tenho: 692 bytes                                               │
│   Preciso: 25 + 61441 = 61466 bytes                              │
│   Status: 692 < 61466 = INCOMPLETO ✓                             │
│   Retorna: []  (nenhum frame completo)                           │
│   Buffer: MANTÉM [692 bytes] em memória ← NÃO DESCARTA!         │
└──────────────────────────────────────────────────────────────────┘

Chunk 2 (1704 bytes):
┌──────────────────────────────────────────────────────────────────┐
│ connBuffer.Append([1704 bytes])                                   │
│                                                                   │
│ Buffer State:                                                     │
│ ┌────────────────────────────────────────────────────────────┐   │
│ │ [692 bytes anterior] + [1704 bytes novo] = 2396 bytes     │   │
│ └────────────────────────────────────────────────────────────┘   │
│                                                                   │
│ ExtractFrames():                                                  │
│   2396 < 61466 = INCOMPLETO ✓                                    │
│   Retorna: []  (nenhum frame completo)                           │
│   Buffer: MANTÉM [2396 bytes] em memória                         │
└──────────────────────────────────────────────────────────────────┘

Chunk 3 (2006 bytes):
┌──────────────────────────────────────────────────────────────────┐
│ connBuffer.Append([2006 bytes])                                   │
│                                                                   │
│ Buffer: [2396 + 2006] = 4402 bytes (continuando acumular...)     │
│                                                                   │
│ ExtractFrames(): 4402 < 61466 = INCOMPLETO ✓                     │
│ Buffer: MANTÉM [4402 bytes]                                      │
└──────────────────────────────────────────────────────────────────┘

... ACUMULA CONTINUAMENTE ...

Chunk N ([último]):
┌──────────────────────────────────────────────────────────────────┐
│ connBuffer.Append([último chunk])                                 │
│                                                                   │
│ Buffer: [4402 + ... + último] = 61466 bytes TOTAIS               │
│                                                                   │
│ ExtractFrames():                                                  │
│   61466 >= 61466 = COMPLETO! ✓✓✓                                 │
│   Retorna: [frameData de 61466 bytes]  ← PRIMEIRO FRAME!          │
│   Buffer: REMOVE frame, mantém resto                             │
│                                                                   │
│   ✓✓✓ SUCESSO! Conseguiu 1 frame completo                        │
└──────────────────────────────────────────────────────────────────┘
         ↓
    
processJT1078Frame(61466 bytes):
    
    ✓ Parser consegue ler header corretamente
    ✓ Extrai Device ID BCD corretamente
    ✓ Extrai Channel ID corretamente
    ✓ Detecta H.264 data (61441 bytes)
    ✓ Procura SPS/PPS/IDR
    ✓ Envia para FFmpeg
    ✓ MediaMTX consegue transmitir


RESULTADO FINAL:
✓ SIM parsing funciona (header correto)
✓ Channel = 0x01 (correto)
✓ DataLen = 61441 (correto)
✓ FFmpeg recebe SPS/PPS/IDR válidos
✓ MediaMTX transmite stream válido
✓ Video player consegue decodificar
```

---

## Diferença no Código

### ANTES (Errado)

```go
func (s *JT808Session) Run() {
    reader := bufio.NewReader(s.Conn)
    
    for {
        buf := make([]byte, 4096)
        n, err := reader.Read(buf)
        
        // PROBLEMA: Cada Read cria novo slice
        // Dados anteriores incompletos são descartados
        messages := s.Parser.Push(buf[:n])  ← Só tem `n` bytes
        
        // Se frame precisava de 61441 bytes e n=692:
        // - Parser retorna []
        // - Próximo Read vem com 1704 bytes diferentes
        // - Parse tenta do zero novamente
        // - Nunca acumula
    }
}
```

### DEPOIS (Correto)

```go
func (ms *MediaServer) handleMediaConnection(conn net.Conn) {
    // Criar BUFFER PERSISTENTE para a conexão
    connBuffer := stream.NewJT1078StreamBuffer()  ← NOVO!
    reader := bufio.NewReader(conn)
    
    for {
        buf := make([]byte, 65536)
        n, err := reader.Read(buf)
        
        // SOLUÇÃO: Acumular ao invés de descartar
        connBuffer.Append(buf[:n])  ← ACUMULA TUDO!
        
        // Extrair apenas frames COMPLETOS
        frames, _ := connBuffer.ExtractFrames()
        
        // Se dataLen=61441 e n=692: frames = [] (aguarda mais)
        // Na próxima read com 1704 bytes: connBuffer tem 2396 (aguarda mais)
        // Quando tiver 61466: frames = [completo] ✓
        
        for _, frameData := range frames {
            processJT1078Frame(frameData, parser, conn)
        }
    }
}
```

---

## Impacto nos Logs

### ANTES

```
[MEDIA_CONN] Received 692 bytes
[MEDIA_CONN] Extracted 0 complete frames
[MEDIA_CONN] Failed to decode SIM: invalid BCD encoding  ← Sintoma!

[MEDIA_CONN] Received 1704 bytes
[MEDIA_CONN] Extracted 0 complete frames
[MEDIA_CONN] Channel is 147, expected 0-16  ← Sintoma!

[MEDIA_CONN] Received 2006 bytes
[MEDIA_CONN] Extracted 0 complete frames
[MEDIA_CONN] DataLen = 0, invalid  ← Sintoma!

... NUNCA PROGRIDE ...
```

### DEPOIS

```
[MEDIA_CONN] Received 692 bytes from 192.168.x.x (total: 692, buffer: 692)
[MEDIA_CONN] Extracted 0 complete frames

[MEDIA_CONN] Received 1704 bytes from 192.168.x.x (total: 2396, buffer: 2396)
[MEDIA_CONN] Extracted 0 complete frames

[MEDIA_CONN] Received 2006 bytes from 192.168.x.x (total: 4402, buffer: 4402)
[MEDIA_CONN] Extracted 0 complete frames

... (mais reads acumulando) ...

[MEDIA_CONN] Received 12000 bytes from 192.168.x.x (total: 61466, buffer: 61466)
[MEDIA_CONN] Extracted 1 complete frames  ← ✓ FINALMENTE!
[MEDIA_CONN] Processing frame 0: 61466 bytes
[MEDIA_CONN] Frame header: 30 31 63 64 [header correto]

✓ Parser consegue ler header
✓ H.264 data extracted corretamente
✓ Enviando para FFmpeg...
```

---

## Verificação Prática

Para confirmar que está funcionando:

```bash
# Terminal 1: Rodar servidor
./server

# Seu cliente JT1078 conecta e envia frames

# Verificar logs com grep (em outro terminal):
tail -f /tmp/broker.log | grep -E "buffer:|Extracted|Processing"

# Você DEVE VER:
# - "Extracted 1 complete frames" (não 0!)
# - "Processing frame 0: 61466 bytes"
# - "Buffer Stats: Received=XXXXX, Frames=N, Dropped=0"
```

Se ver:
- ✓ Frames > 0: Solução funcionando!
- ✓ Dropped = 0: Sem perda de dados!
- ✓ Resyncs ≈ 0: Buffer limpo e ordenado!

---

## Conclusão

**ANTES**: Cada read descartava buffer → nunca completava frame → tudo falhava

**DEPOIS**: Buffer persistente acumula → completa frame → tudo funciona

O código agora segue princípio de **streaming orientado a buffer**, não **orientado a read()**.

Isso é padrão industrial para processamento de protocolos em streaming TCP.
