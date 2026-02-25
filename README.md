# JT-Broker - JT808/JT1078 Protocol Broker

Projeto limpo e funcional do broker de protocolos JT808 e JT1078 para comunicação com dispositivos GPS/multimídia.

## 📋 Estrutura do Projeto

```
jt-broker/
├── cmd/                      # Executáveis principais
│   ├── server/              # Servidor principal JT808/JT1078
│   ├── camera/              # Aplicação de câmera
│   ├── h264_diag/           # Diagnóstico de H264
│   ├── test_jt1078_client/  # Cliente de teste JT1078
│   └── test_jt1078_correct/ # Validador de protocolo
├── internal/                # Código interno da aplicação
│   ├── config/              # Configurações
│   ├── ffmpeg/              # Worker FFmpeg
│   ├── http/                # API HTTP e player web
│   ├── protocol/            # Implementação dos protocolos
│   ├── stream/              # Gerenciamento de streams
│   └── tcp/                 # Camada TCP
├── auth/                    # Scripts de autenticação
├── docs/                    # Documentação completa
├── bin/                     # Binários compilados
├── logs/                    # Arquivos de log
└── streams/                 # Streams H264 salvos
```

## 🚀 Características Principais

- ✅ Suporte completo aos protocolos JT808 e JT1078
- ✅ Streaming de vídeo H264 em tempo real
- ✅ Buffer inteligente para tratamento de fragmentação
- ✅ Detecção e correção de NAL units
- ✅ Player web integrado para visualização
- ✅ API HTTP para controle e monitoramento
- ✅ Conversão FFmpeg para RTSP (opcional)
- ✅ Persistência de streams em disco
- ✅ Gerenciamento de sessões com timeout

## 🛠️ Compilação

```bash
# Compilar o servidor principal
go build -o bin/jt808-server cmd/server/main.go

# Compilar todas as ferramentas
go build -o bin/camera cmd/camera/main.go
go build -o bin/h264_diag cmd/h264_diag/main.go
go build -o bin/test_jt1078_client cmd/test_jt1078_client/main.go
go build -o bin/test_jt1078_correct cmd/test_jt1078_correct/main.go
```

## 🏃 Execução

```bash
# Executar o servidor
./bin/jt808-server

# O servidor estará disponível em:
# - JT808: porta 6808
# - JT1078 (Multimedia): porta 6078
# - HTTP API: porta 8080
```

## 🌐 Endpoints HTTP

- `GET /` - Player web de vídeo
- `GET /api/devices` - Lista dispositivos conectados
- `GET /api/devices/:phoneNumber/streams` - Streams disponíveis
- `GET /api/video/live/:phoneNumber/:channel` - Stream de vídeo ao vivo
- `GET /api/video/playback/:phoneNumber/:channel/:time` - Reprodução de vídeo histórico

## 📚 Documentação

Toda a documentação está disponível em `/docs`:

- [API.md](docs/API.md) - Documentação da API HTTP
- [PROTOCOL_SPECIFICATION.md](docs/PROTOCOL_SPECIFICATION.md) - Especificação dos protocolos
- [VIDEO_STREAMING_GUIDE.md](docs/VIDEO_STREAMING_GUIDE.md) - Guia de streaming de vídeo
- [TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) - Solução de problemas
- E muitos outros...

## 🔧 Testes

```bash
# Executar testes unitários
go test ./internal/stream/

# Testar conexão JT1078
./bin/test_jt1078_client

# Diagnosticar arquivo H264
./bin/h264_diag streams/arquivo.h264
```

## 📝 Registro de Dispositivos

```bash
# Registrar um dispositivo usando Node.js
node auth/jt808-reg.js
```

## ⚙️ Dependências

- Go 1.21 ou superior
- FFmpeg (opcional, para conversão RTSP)

## 🎯 Status do Projeto

✅ **Totalmente funcional e testado**

Este projeto foi validado e está funcionando com maestria para:
- Recepção de pacotes JT808/JT1078
- Tratamento de fragmentação de pacotes
- Detecção e correção de NAL units H264
- Streaming em tempo real
- Player web integrado
- API REST completa

## 📄 Licença

Projeto interno - Grupo JL

---

**Data de criação desta versão limpa**: 25 de Fevereiro de 2026
