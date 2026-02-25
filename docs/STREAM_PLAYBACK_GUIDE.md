# Guia de Reprodução de Streams H.264

## Visão Geral

O servidor JT808-Broker agora oferece uma interface web completa para visualizar e gerenciar streams H.264 de vídeo. Os arquivos de vídeo são automaticamente salvos na pasta `streams/` e podem ser acessados e reproduzidos através do navegador.

## Como Funciona

```
Câmera/Dispositivo → Servidor JT1078 (porta 6208) → Arquivo H.264 (streams/)
                                                           ↓
                                                    HTTP API (porta 8189)
                                                           ↓
                                                      Web Player
```

## Endpoints HTTP Disponíveis

### 1. **Listar Streams Disponíveis**
```
GET /streams
Content-Type: application/json
```

**Resposta:**
```json
{
  "count": 3,
  "streams": [
    {
      "filename": "816200000119_CH147_20260224.h264",
      "size": 1024512,
      "modified": 1708860665,
      "url": "/streams/816200000119_CH147_20260224.h264",
      "stream_url": "/streams/816200000119_CH147_20260224.h264?stream=1",
      "player_html": "/?file=816200000119_CH147_20260224.h264"
    }
  ]
}
```

### 2. **Servir Stream (Reprodução/Download)**
```
GET /streams/{filename}
```

Retorna o arquivo H.264 bruto com suporte a:
- **Range requests** para seeking
- **MIME type** correto (video/h264)
- **Streaming** contínuo

Exemplos:
- Download direto: `/streams/816200000119_CH147_20260224.h264`
- Reprodução no player: `/?file=816200000119_CH147_20260224.h264`

### 3. **Web Player (Interface Principal)**
```
GET /
```

Interface interativa com:
- ✅ Lista de streams na lateral
- ✅ Player de vídeo ao vivo (com controles HTML5)
- ✅ Seleção de arquivo para reprodução
- ✅ Download de streams
- ✅ Tela cheia
- ✅ Auto-atualização a cada 10 segundos

## Usando a Interface Web

### 1. Abrir o Player
```bash
# No navegador acesse:
http://localhost:8189
# ou
http://<IP_DO_SERVIDOR>:8189
```

### 2. Estrutura da Interface

```
┌─────────────────────────────────────────────────────────┐
│              🎥 Video Stream Player                      │
│         Reproduza streams H.264 disponíveis             │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌──────────────────────────────┐  ┌──────────────┐   │
│  │                              │  │   Streams    │   │
│  │        VIDEO PLAYER          │  │ Disponíveis  │   │
│  │                              │  │              │   │
│  │   [▶ Play] [⛶ Fullscreen]   │  │  📹 stream1  │   │
│  └──────────────────────────────┘  │  📹 stream2  │   │
│                                     │  📹 stream3  │   │
│  Stream Selecionado:                │              │   │
│  [filename________________]          │ 🔄 Atualizar│   │
│  [⬇ Baixar Stream]                  └──────────────┘   │
│  [⛶ Tela Cheia]                                        │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

### 3. Reproduzir um Stream

1. **Na lateral**, clique em um arquivo de stream
2. O vídeo começará a carregar automaticamente
3. Clique em ▶ Play ou aguarde o carregamento automático
4. Use os controles HTML5 padrão para pausar/volume/legenda

### 4. Baixar um Stream

1. Selecione o stream na lista
2. Clique em "⬇ Baixar Stream"
3. O arquivo será baixado em `.h264` para seu computador

### 5. Visualizar em Tela Cheia

1. Reproduza um stream
2. Clique em "⛶ Tela Cheia"
3. Pressione `ESC` para sair

## Usando a API com cURL

### Listar todos os streams
```bash
curl http://localhost:8189/streams | jq
```

### Download de um stream específico
```bash
curl -O http://localhost:8189/streams/816200000119_CH147_20260224.h264
```

### Reproduzir stream com VLC (ffmpeg)
```bash
# Via HTTP streaming
ffplay http://localhost:8189/streams/816200000119_CH147_20260224.h264

# Via VLC
vlc http://localhost:8189/streams/816200000119_CH147_20260224.h264
```

## Convertendo H.264 para MP4 (Offline)

Depois de baixar o stream H.264, você pode convertê-lo para MP4:

```bash
# Usando FFmpeg
ffmpeg -i stream.h264 -c:v libx264 -preset fast -crf 23 output.mp4

# Ou manter o codec original (mais rápido):
ffmpeg -i stream.h264 -c copy output.mp4
```

## Integração com FFmpeg em Tempo Real

Para streaming em tempo real com conversão automática, adicione FFmpeg como conversor:

### Requisitos
```bash
# Instalar FFmpeg
sudo apt-get install ffmpeg

# Verificar instalação
ffmpeg -version
```

### Conversão HLS (HTTP Live Streaming)

Para criar streams HLS em tempo real:

```bash
# Converter H.264 para HLS
ffmpeg -i stream.h264 \
  -c:v copy \
  -f hls \
  -hls_time 10 \
  -hls_list_size 6 \
  -hls_flags delete_segments \
  output.m3u8
```

Então acesse via navegador:
```
http://localhost:8189/streams/output.m3u8
```

## Troubleshooting

### ❌ "Stream não encontrado" (404)
- Verifique se o arquivo existe em `./streams/`
- Verifique o nome exato do arquivo (case-sensitive)

### ❌ "Vídeo não carrega"
- Verifique se o arquivo H.264 não está corrompido
- Tente com `ffprobe` para validar:
  ```bash
  ffprobe streams/arquivo.h264
  ```

### ❌ Streams antigos acumulados
Para limpar a pasta:
```bash
# Listar arquivos por data
ls -lhtr streams/

# Remover arquivos antigos (exemplo: mais de 7 dias)
find streams/ -name "*.h264" -mtime +7 -delete

# Ou remover tudo
rm streams/*.h264
```

### ❌ Player não carrega lista
- Verifique a porta 8189: `netstat -tlnp | grep 8189`
- Verifique firewall/acesso local
- Veja logs do servidor

## Exemplo Prático de Uso

### 1. Iniciar o servidor
```bash
cd /home/grupo-jl/jt808-broker
./server
```

Saída esperada:
```
[MAIN] Starting JT808 broker server...
[MAIN] HTTP API server on :8189
```

### 2. Abrir no navegador
```
http://localhost:8189
```

### 3. Conectar câmera/dispositivo
- Câmera se conecta na porta 6207 (JT808)
- Vídeo é recebido na porta 6208 (JT1078)
- Arquivo é salvo em `streams/`

### 4. Reproduzir
- Arquivo aparece automaticamente na lista lateral
- Clique para reproduzir
- Use controles de vídeo padrão

## Especificações Técnicas

### Portas
| Porta | Protocolo | Uso |
|-------|-----------|-----|
| 6207  | JT808     | Conexão de dispositivos GPS |
| 6208  | JT1078    | Recepção de streams de vídeo |
| 8189  | HTTP      | API e Web Player |

### Formatos Suportados
- **Vídeo**: H.264 (AVC), H.265 (HEVC)
- **Extensão**: `.h264`, `.h265`
- **MIME Types**: `video/h264`, `video/h265`

### Navegadores Suportados
- Chrome/Chromium 90+
- Firefox 88+
- Safari 14+
- Edge 90+

## Performance e Otimizações

### Streaming Eficiente
```javascript
// JavaScript automático:
// - Auto-resume após pause
// - Cache de metadados
// - Range requests habilitados
```

### Largura de Banda
- Streams são servidos com compressão gzip
- Support a range requests para seeking eficiente
- Cache headers configurados

## Próximos Passos

1. **Conversão Automática**: Integrar FFmpeg para converter H.264 → MP4/HLS automaticamente
2. **Gravação em Nuvem**: Enviar streams para cloud storage
3. **Análise de Vídeo**: Integrar detecção de movimento/objetos
4. **Dashboard**: Mostrar múltiplos streams simultaneamente
5. **Archive**: Sistema de retenção de vídeos com limpeza automática

## Suporte

Para problemas ou dúvidas:
1. Verifique os logs: `tail -f stdout.log`
2. Teste endpoints com `curl -v`
3. Valide arquivos H.264 com `ffprobe`
4. Verifique conectividade de rede

---

**Última atualização**: 24/02/2026
