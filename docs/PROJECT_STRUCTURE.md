# Estrutura de Projeto Alinhada - JT808-Broker

## Visão Geral da Arquitetura

O projeto foi estruturado para seguir padrões profissionais de desenvolvimento em Go, alinhado com a especificação de protocolo JT808/JT1078 do fabricante.

```
jt808-broker/
├── docs/                           # Documentação
│   ├── PROTOCOL_SPECIFICATION.md   # Especificação completa do protocolo
│   ├── MIGRATION_GUIDE.md          # Guia de migração
│   ├── API.md                      # API HTTP
│   ├── MULTIMEDIA.md               # Implementação multimedia
│   └── TROUBLESHOOTING.md          # Resolução de problemas
│
├── cmd/                            # Executáveis
│   ├── server/main.go              # Servidor principal (TCP + HTTP)
│   └── camera/main.go              # Cliente camera (teste)
│
├── internal/                       # Código interno (não exportável)
│   ├── protocol/                   # Protocolo JT808/JT1078/JT1077
│   │   ├── types.go                # Tipos estruturados de pacotes
│   │   ├── parser.go               # Parser base (escape, BCD, checksum)
│   │   ├── jt808.go                # Parser JT808 específico
│   │   ├── jt1078.go               # Parser JT1078 (vídeo)
│   │   ├── framing.go              # Enquadramento de pacotes
│   │   └── multimedia.go           # Tipos multimedia
│   │
│   ├── stream/                     # Gerenciamento de stream/sessão
│   │   ├── jt808_session.go        # Sessão JT808 individual
│   │   ├── session.go              # Interface base de sessão
│   │   ├── registry.go             # Registro de dispositivos conectados
│   │   ├── router.go               # Roteador de mensagens
│   │   ├── multimedia_store.go     # Armazenamento de arquivos multimedia
│   │   └── packet_handler.go       # Handler de pacotes (novo)
│   │
│   ├── tcp/                        # Camada TCP
│   │   ├── listener.go             # Listener TCP
│   │   └── connection.go           # Conexão TCP individual
│   │
│   ├── http/                       # API HTTP
│   │   ├── api.go                  # Handler de API
│   │   └── player.html             # Player web
│   │
│   ├── config/                     # Configuração
│   │   └── config.go               # Carregamento de config
│   │
│   └── ffmpeg/                     # Processamento multimedia
│       └── worker.go               # Worker FFmpeg
│
├── streams/                        # Dados de streams (multimedia salvo)
│   └── {DeviceID}/
│       └── multimedia/
│           └── {arquivos}.{ext}
│
├── go.mod                          # Dependências Go
├── go.sum                          # Hash de dependências
├── main.go / jt808-broker          # Entry point principal
├── stream_command.py               # Utilitário Python
└── README.md                       # Documentação principal
```

## Camadas da Arquitetura

### 1. Camada de Protocolo (`internal/protocol/`)

**Responsabilidade**: Parsing e construção de pacotes JT808/JT1078

**Arquivos**:
- `types.go`: Tipos estruturados, constantes, interfaces
- `parser.go`: Parser base com escape/unescape, BCD, checksum
- `jt808.go`: Implementação específica JT808 com compatibilidade retroativa
- `jt1078.go`: Parser para protocolo de vídeo JT1078
- `framing.go`: Funcionalidades de enquadramento
- `multimedia.go`: Tipos específicos para multimedia

**Funções Principais**:
```go
type PacketFrame struct       // Quadro completo
type PacketHeader struct      // Header estruturado
type PacketParser interface   // Interface de parser

DecodeBCD()                   // Conversão BCD
EncodeBCD()                   // Conversão reversa
Escape()                      // Escape de caracteres especiais
Unescape()                    // Remoção de escape
CalculateChecksum()           // Validação XOR
```

**Mensagens Suportadas**:
- 0x0001: Login
- 0x0002: Logout
- 0x0003: Heartbeat
- 0x0200: Location Report
- 0x0800: Multimedia Event
- 0x0801: Multimedia Data
- 0x8001: General Response
- 0x8800: Multimedia Response

### 2. Camada de Stream (`internal/stream/`)

**Responsabilidade**: Gerenciamento de sessões e roteamento de mensagens

**Arquivos**:
- `jt808_session.go`: Sessão individual de dispositivo
- `session.go`: Interface base de sessão
- `registry.go`: Registro de dispositivos conectados
- `router.go`: Roteador de mensagens
- `multimedia_store.go`: Armazenamento de arquivos
- `packet_handler.go`: Handler de pacotes (novo)

**Fluxo**:
```
Conexão TCP
    ↓
JT808Session.Run()
    ↓
Parser.Push(data)
    ↓
handleMessage(msg)
    ↓
Registry (armazenar estado)
    ↓
MultimediaStore (salvar arquivos)
```

### 3. Camada TCP (`internal/tcp/`)

**Responsabilidade**: Gerenciamento de conexões TCP

**Arquivos**:
- `listener.go`: Listener TCP na porta 6207
- `connection.go`: Conexão individual com timeout

### 4. Camada HTTP (`internal/http/`)

**Responsabilidade**: API HTTP para gerenciamento remoto

**Endpoints**:
- `GET /devices` - Lista dispositivos conectados
- `GET /device/{id}` - Status do dispositivo
- `POST /camera/capture` - Comando de câmera
- `GET /stream/{id}` - Stream de vídeo

### 5. Camada de Configuração (`internal/config/`)

**Responsabilidade**: Carregamento de configurações

### 6. Processamento Multimedia (`internal/ffmpeg/`)

**Responsabilidade**: Conversão de mídia com FFmpeg

## Fluxo de Dados - Recebimento de Mensagem

```
┌─────────────────────────────────────────────────────────────────────┐
│ Dispositivo TCP → Porta 6207                                        │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ tcp/listener.go: NewListener() - Aceita conexão                     │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ tcp/connection.go: Connection.Handle()                              │
│   - TCP Read buffer                                                 │
│   - Passa para JT808Session                                         │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ stream/jt808_session.go: JT808Session.Run()                         │
│   - Cria novo parser                                                │
│   - Lê dados TCP em loop                                            │
│   - Passa para Parser.Push()                                        │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ protocol/jt808.go: JT808Parser.Push()                               │
│   - Acumula bytes em buffer                                         │
│   - Encontra delimitadores 0x7E                                     │
│   - Remove escape (0x7D sequências)                                 │
│   - Valida checksum XOR                                             │
│   - Parseia header (13 bytes)                                       │
│   - Extrai body e retorna PacketFrame                               │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ JT808Session.handleMessage(msg)                                     │
│   - Switch por MessageID                                            │
│   - Processa tipo específico                                        │
│   - Extrai dados do Body                                            │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ Ações Específicas:                                                  │
│ - Login (0x0001): Registry.Register()                              │
│ - Heartbeat (0x0003): Enviar resposta (0x8001)                     │
│ - Location (0x0200): Logs / Banco de dados                         │
│ - Multimedia (0x0800/0x0801): MultimediaStore.Process()            │
└─────────────────────────────────────────────────────────────────────┘
```

## Fluxo de Dados - Envio de Comando

```
┌─────────────────────────────────────────────────────────────────────┐
│ HTTP API: POST /camera/capture?device=XXX&channel=1                │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ http/api.go: HandleCameraCapture()                                  │
│   - Valida parâmetros                                               │
│   - Extrai device ID                                                │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ stream/registry.go: Registry.Get(deviceID)                          │
│   - Encontra sessão do dispositivo                                  │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ protocol/jt808.go: BuildCameraCommandImmediate()                    │
│   - Cria body com parâmetros                                        │
│   - Chama BuildResponse()                                           │
│   - Escapa sequências especiais                                     │
│   - Adiciona delimitadores 0x7E                                     │
│   - Retorna bytes para envio                                        │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ JT808Session: session.Conn.Write(encoded)                           │
│   - Envia para dispositivo via TCP                                  │
└─────────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────────┐
│ Dispositivo TCP ← Comando recebido                                  │
└─────────────────────────────────────────────────────────────────────┘
```

## Estrutura de Pacote de Exemplo

### Pacote de Login (0x0001)

```
Raw Bytes (sem escape):
┌──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬─ ... ─┬──┐
│7E│00│01│00│1C│00│00│00│00│00│01│00│02│1C│Body... │CS│7E
└──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴─ ... ─┴──┘
 │  └──┬──┘  └──┬──┘  └─────┬──────┘  └──┬──┘  └──┬──┘  │
 │     │       │         │            │        │      │
Flag   │       │         │            │        │      Flag
     MsgID   Props     DeviceID    SeqNum   BodyLen  Checksum
   0x0001  0x001C   000000000001  0x0002    28      XOR(...)

Decodificado:
- Flag: 0x7E (Delimitador)
- MessageID: 0x0001 (Login)
- Properties: 0x001C (Response Required=0, Encryption=0, BodyLen=28)
- DeviceID: "000000000001" (BCD decodificado)
- SequenceNum: 0x0002
- Body: 28 bytes (Login data)
- Checksum: XOR de todos os bytes
- Flag: 0x7E (Delimitador)
```

## Estado do Dispositivo

```go
type DeviceRegistry struct {
    devices map[string]*JT808Session  // ID → Sessão
    mu      sync.RWMutex              // Thread-safe
}

// Informações Mantidas:
- Conexão ativa
- Último heartbeat
- Localização atual
- Arquivos multimedia em progresso
- Sequência de mensagens
- Informações de dispositivo
```

## Validação e Tratamento de Erro

### Validações por Camada

**Camada TCP**:
- Conexão ativa
- Timeout de idle
- Buffer overflow

**Camada de Protocolo**:
- Delimitadores presentes (0x7E)
- Header válido (13 bytes)
- Checksum válido (XOR)
- Body length consistente
- Device ID válido (BCD)

**Camada de Aplicação**:
- Message ID suportado
- Campos obrigatórios presentes
- Valores em intervalos válidos

### Estratégia de Recuperação

```
Erro de Parsing
    ↓
Limpar buffer até próximo 0x7E
    ↓
Registrar erro
    ↓
Continuar processamento
    ↓
(Não fechar conexão a menos que seja erro crítico)
```

## Performance e Otimizações

### Buffer Reuse
- Preallocate slices onde possível
- Reusar buffers de parsing

### Concorrência
- Uma goroutine por conexão
- Registry com sync.RWMutex para acesso thread-safe
- Canais para comunicação entre goroutines

### Logging
- Prefixos categorizados: [PROTOCOL], [TCP], [SESSION], [MULTIMEDIA]
- Níveis: INFO, WARN, ERROR

## Extensibilidade

### Adicionar Novo Tipo de Mensagem

1. Adicionar constante em `protocol/types.go`:
```go
const MsgNewType uint16 = 0x1234
```

2. Criar estrutura de dados em `protocol/types.go`:
```go
type NewTypeMessage struct {
    Field1 uint16
    Field2 string
}
```

3. Adicionar parsing em `stream/jt808_session.go`:
```go
case protocol.MsgNewType:
    handleNewType(msg)
```

### Adicionar Novo Parser (Ex: JT1077)

1. Criar `protocol/jt1077.go` estendendo `BaseParser`
2. Implementar `Push()` e `Encode()` methods
3. Usar no router baseado em tipo de conexão

## Segurança

- Validação de checksum obrigatória
- Limites de tamanho de buffer
- Timeout de conexão
- Validação de Device ID
- Logs de segurança para operações críticas

## Monitoramento e Logs

```go
[PROTOCOL] BCD decoded: 00 00 00 00 00 01 → 000000000001
[TCP] New connection from 127.0.0.1:56789
[SESSION] Device identified: 000000000001
[SESSION] Received message: Location Report (0x0200)
[MULTIMEDIA] Frame parsed: MsgID=0x0800, BodyLen=9
```

## Próximas Melhorias

1. [ ] Implementar persistência de dados (banco de dados)
2. [ ] Adicionar autenticação de dispositivos
3. [ ] Implementar cache de localização
4. [ ] Adicionar métricas Prometheus
5. [ ] Implementar circuit breaker para conexões instáveis
6. [ ] Adicionar suporte para múltiplos servidores (cluster)
7. [ ] Implementar message queue para tratativa assíncrona

