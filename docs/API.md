# JT808 Broker - HTTP API Usage

## Starting the Server

```bash
./jt808-broker
```

The server will start:
- **JT808 TCP Server**: Port 6207
- **HTTP API**: Port 8080

## API Endpoints

### 1. List Connected Devices

```bash
curl http://localhost:8189/devices
```

Response:
```json
{
  "count": 1,
  "devices": [
    {
      "device_id": "000000000000",
      "address": "177.69.143.153:56566"
    }
  ]
}
```

### 2. Get Device Status

```bash
curl http://localhost:8080/device/000000000000
```

Response:
```json
{
  "device_id": "000000000000",
  "status": "connected"
}
```

### 3. Request Camera Capture

**Take 1 photo:**
```bash
curl -X POST "http://localhost:8080/camera/capture?device=000000000000&channel=1&shots=1&resolution=4&quality=5"
```

**Take 5 photos with 2 second interval:**
```bash
curl -X POST "http://localhost:8189/camera/capture?device=000000000000&channel=1&shots=5&interval=2&resolution=4&quality=5"
```

**Start video recording:**
```bash
curl -X POST "http://localhost:8080/camera/capture?device=000000000000&channel=1&shots=65535&resolution=4&quality=5"
```

Parameters:
- `device` (required): Device ID (12 hex digits)
- `channel` (default: 1): Camera channel number
- `shots` (default: 1): Number of photos (1-65534) or 65535 for video
- `interval` (default: 0): Seconds between shots
- `resolution` (default: 4):
  - 1 = 320x240
  - 2 = 640x480
  - 3 = 800x600
  - 4 = 1024x768 (default)
  - 5 = 176x144 (QCIF)
  - 6 = 352x288 (CIF)
  - 7 = 704x288 (HALF D1)
  - 8 = 704x576 (D1)
- `quality` (default: 5): 1-10 (1=best, 10=lowest)

Response:
```json
{
  "success": true,
  "device_id": "000000000000",
  "command": "camera_capture",
  "channel": 1,
  "shots": 1,
  "interval": 0,
  "resolution": 4,
  "quality": 5
}
```

## Testing Workflow

1. **Start the server:**
   ```bash
   ./jt808-broker
   ```

2. **Wait for device to connect** (check logs for authentication)

3. **List connected devices:**
   ```bash
   curl http://localhost:8080/devices
   ```

4. **Request photo capture:**
   ```bash
   curl -X POST "http://localhost:8080/camera/capture?device=000000000000"
   ```

5. **Check for multimedia upload in logs:**
   ```
   [JT808_SESSION] Multimedia event from device 000000000000
   [MULTIMEDIA] Event parsed - ID: 1234, Type: Image, Format: JPEG...
   [MULTIMEDIA_STORE] Upload completed - ...
   ```

6. **Find saved file:**
   ```bash
   ls -la ./streams/000000000000/multimedia/
   ```

## Expected Multimedia Flow

After sending camera command:

1. Device receives 0x8801 command
2. Device captures photo/video
3. Device sends 0x0800 (Multimedia Event)
4. Server responds with 0x8800
5. Device sends 0x0801 (Multimedia Data)
6. Server saves file
7. Server responds with 0x8800

## Saved Files Location

```
./streams/
└── {DeviceID}/
    └── multimedia/
        └── YYYYMMDD_HHMMSS_ch{channel}_id{multimedia_id}.{ext}
```

Example:
```
./streams/000000000000/multimedia/20260211_171500_ch1_id1234.jpg
```

## Troubleshooting

**Device not in list?**
- Check if device is authenticated (look for "Device identified" in logs)
- Verify device is connected (not disconnected)

**No multimedia received?**
- Device might not support camera commands
- Check device configuration/firmware
- Verify channel number is correct
- Look for error responses from device in logs

**Command sent but no response?**
- Some devices require specific configuration
- Check device manual for multimedia settings
- Enable platform multimedia features on device
