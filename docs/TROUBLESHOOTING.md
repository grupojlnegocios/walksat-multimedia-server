# Diagnóstico: Device ID Zerado e Sem Vídeo

## Problema Identificado

### 1. Device ID = 000000000000
Os logs mostram claramente:
```
[JT808] Raw device ID bytes: [00 00 00 00 00 00]
[JT808] BCD decoded: 00 00 00 00 00 00 -> 000000000000
```

**Causa**: O dispositivo GPS/DVR **não está configurado** com um Device ID válido.

**Solução**: Configure o Device ID (IMEI ou número de telefone do SIM) no dispositivo através do software de configuração do fabricante.

### 2. Apenas Heartbeats (0x0002)
O dispositivo está enviando apenas:
- 0x0102 - Autenticação (conexão inicial)
- 0x0002 - Heartbeat (a cada 15 segundos)

**Não está enviando**:
- 0x0200 - Relatórios de localização GPS
- 0x0800 - Eventos multimídia
- 0x0801 - Upload de fotos/vídeos
- Dados de vídeo JT1078

## Por Que Não Tem Vídeo?

### Arquitetura JT808/JT1078

O protocolo tem **dois canais separados**:

1. **JT808** (porta 6207) - Controle e GPS
   - Autenticação
   - Heartbeat
   - Localização GPS
   - Comandos de câmera
   - Notificações de multimídia

2. **JT1078** (porta 1078) - Stream de Vídeo
   - Stream H.264/H.265 em tempo real
   - Conexão TCP separada
   - Iniciada após comando de vídeo

### O Que Está Acontecendo

Seu dispositivo:
- ✅ Conecta na porta 6207 (JT808)
- ✅ Autentica com sucesso
- ✅ Envia heartbeats regularmente
- ❌ Não envia GPS (0x0200)
- ❌ Não abre conexão JT1078 para vídeo
- ❌ Device ID zerado

## Soluções

### 1. Configurar o Device ID

Use o software de configuração do dispositivo (normalmente fornecido pelo fabricante) para:

```
Parâmetro: Device ID / Phone Number / IMEI
Valor: Número do SIM card ou IMEI do dispositivo
Exemplo: 351234567890123 (15 dígitos)
```

### 2. Habilitar Relatórios GPS

Configure no dispositivo:
```
Parâmetro: Upload Interval / Report Interval
Valor: 10-30 segundos
Tipo: Timed Report
```

### 3. Configurar Porta de Vídeo JT1078

Para receber stream de vídeo, você precisa:

**No servidor**, adicione listener na porta 1078:

```go
// No main.go
go func() {
    log.Println("[MAIN] Starting JT1078 video listener on :1078")
    tcp.Listen(":1078", router)
}()
```

**No dispositivo**, configure:
```
Server IP: seu_ip
JT808 Port: 6207 (controle)
JT1078 Port: 1078 (vídeo)
```

### 4. Enviar Comando de Vídeo

Depois de configurar, use a API HTTP:

```bash
# Listar dispositivos conectados
curl http://localhost:8080/devices

# Solicitar captura de vídeo
curl -X POST "http://localhost:8080/camera/capture?device=SEU_DEVICE_ID_REAL&shots=65535"
```

## Verificação Step-by-Step

### 1. Verificar Configuração do Dispositivo
- [ ] Device ID configurado (não pode ser 000000000000)
- [ ] Servidor JT808: seu_ip:6207
- [ ] Servidor JT1078: seu_ip:1078 (se suportar vídeo)
- [ ] Intervalo de GPS habilitado
- [ ] SIM card instalado e funcionando

### 2. Verificar Conectividade
```bash
# No servidor, verificar portas abertas
netstat -tlnp | grep 6207
netstat -tlnp | grep 1078

# Verificar dispositivo conectado
curl http://localhost:8080/devices
```

### 3. Verificar Logs do Servidor
Procure por:
```
[JT808] WARNING: Device ID is all zeros!  ❌ Precisa configurar
[JT808] Parsed message - ID: 0x0200       ✅ GPS funcionando
[JT808] Multimedia event from device      ✅ Câmera funcionando
[JT1078] Found frame                      ✅ Vídeo chegando
```

### 4. Teste de Foto/Vídeo
```bash
# Depois de configurar o Device ID correto
curl -X POST "http://localhost:8080/camera/capture?device=SEU_DEVICE_ID&channel=1&shots=1"
```

Aguarde resposta do dispositivo:
```
[JT808_SESSION] Multimedia event from device SEU_DEVICE_ID
[MULTIMEDIA_STORE] Upload completed
```

## Fabricantes Comuns e Software

- **Concox**: CMS Client / GT06 Protocol Tool
- **Jointech**: JT ConfigTool
- **Teltonika**: Teltonika Configurator
- **Queclink**: Queclink Configuration Tool
- **Generic JT808**: Busque "JT808 configuration tool" + marca do dispositivo

## Próximos Passos

1. **Configure o Device ID no dispositivo** usando o software do fabricante
2. **Verifique se GPS está habilitado** (deve enviar 0x0200)
3. **Se tem câmera**, verifique configuração de multimídia
4. **Para vídeo ao vivo**, adicione porta 1078 no servidor
5. **Reinicie o dispositivo** após configurar
6. **Teste a conexão** e verifique novos logs

## Referências

- JT808 Spec: Transporte e controle (porta 6207)
- JT1078 Spec: Stream de vídeo (porta 1078)
- Seu servidor já está pronto, só precisa configurar o dispositivo! 📱🔧
