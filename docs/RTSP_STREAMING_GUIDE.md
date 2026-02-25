# RTSP Streaming Implementation Guide

## Architecture

```
JT1078 TCP (6208) 
    ↓ (H.264 packets)
Parse JT1078 packets
    ↓ (raw H.264 data)
Stream Buffer (64KB)
    ├─→ Save to file (streams/DEVICE_CH_DATE.h264)
    └─→ Pipe to ffmpeg stdin
         ↓
      ffmpeg -f h264 -i pipe:0 -c copy -f rtsp -rtsp_transport tcp rtsp://localhost:8554/cam_DEVICE_CH
         ↓
      RTSP Server (MediaMTX)
         ↓
      rtsp://localhost:8554/cam_DEVICE_CH (available for playback)
```

## Components

### 1. StreamBuffer Structure
- **FileHandle**: Persistent file for H.264 backup
- **BufferSize**: 64KB in-memory accumulation threshold
- **FFmpegProcess**: ffmpeg -f h264 subprocess piping
- **FFmpegStdin**: Write H.264 data here for RTSP
- **RTSPURL**: Target RTSP endpoint

### 2. Key Functions

#### startFFmpegStream()
- Launches ffmpeg with H.264 input from stdin
- Connects to RTSP server
- Runs in background goroutine
- Command: `ffmpeg -f h264 -i pipe:0 -c copy -f rtsp -rtsp_transport tcp rtsp://...`

#### flushStreamBuffer()
- Dual-write: file + ffmpeg stdin
- Triggered when buffer reaches 64KB or I-frame detected
- Non-blocking writes to ffmpeg (continue if pipe full)

#### flushAllStreams()
- On disconnect: close ffmpeg stdin pipes
- Signal EOF to ffmpeg processes
- Close file handles
- Clean up resources

## Setup Instructions

### Step 1: Ensure ffmpeg is installed
```bash
which ffmpeg  # Should output /usr/bin/ffmpeg
```

### Step 2: Start RTSP Server (optional - only if you don't have one)
```bash
bash start_rtsp_server.sh
# Or use existing MediaMTX/Live555 instance
# RTSP server must listen on port 8554
```

### Step 3: Start JT808 Broker
```bash
cd /home/grupo-jl/jt808-broker
./server
# Monitor output for RTSP stream startup messages
```

### Step 4: Send H.264 Stream via JT1078
```bash
# From camera/device that supports JT1078
# TCP to localhost:6208 with H.264 data
```

### Step 5: Playback RTSP Stream
```bash
# Option A: FFplay (fastest feedback)
ffplay rtsp://localhost:8554/cam_DEVICE_CHANNEL
ffplay rtsp://localhost:8554/cam_816200000119_ch0

# Option B: VLC
vlc rtsp://localhost:8554/cam_816200000119_ch0

# Option C: Curl verify
curl -I rtsp://localhost:8554/cam_816200000119_ch0

# Option D: ffprobe inspect
ffprobe -rtsp_transport tcp rtsp://localhost:8554/cam_816200000119_ch0
```

## Troubleshooting

### No RTSP streams appearing
1. Check server logs for `startFFmpegStream()` messages
2. Verify ffmpeg path: `/usr/bin/ffmpeg`
3. Confirm RTSP server running on port 8554
4. Check firewall: `lsof -i :8554`

### ffmpeg processes accumulating
1. Check `ps aux | grep ffmpeg`
2. Verify flushAllStreams() is being called on disconnect
3. Monitor stderr output for ffmpeg errors

### H.264 data issues
1. Verify H.264 has proper NAL start codes (0x00 0x00 0x00 0x01)
2. Check ffmpeg command syntax with manual test:
   ```bash
   cat test.h264 | ffmpeg -f h264 -i pipe:0 -c copy -f rtsp -rtsp_transport tcp rtsp://localhost:8554/test
   ```
3. Inspect saved file: `ffprobe streams/DEVICE_CH_*.h264`

### Buffer handling
1. Monitor buffer flush frequency in logs
2. Current threshold: 64KB per device/channel
3. Increase/decrease in startFFmpegStream() if needed

## Performance Considerations

- **Dual-write overhead**: File + RTSP < 1ms latency impact
- **ffmpeg startup**: ~50ms per stream (acceptable)
- **Memory per stream**: ~65KB (64KB buffer + overhead)
- **Max concurrent streams**: Limited by system resources (file descriptors, memory)

## Security Notes

- RTSP connections are unencrypted (use firewalls)
- No authentication implemented (add if needed)
- ffmpeg runs with same privileges as JT808 process
- Firewall: Allow outbound to RTSP server port 8554

## Monitoring

```bash
# Watch active RTSP streams
watch -n 1 'ps aux | grep "ffmpeg.*rtsp"'

# Monitor file creation
watch -n 1 'ls -lah streams/ | tail -20'

# Check RTSP server connectivity
nc -zv localhost 8554

# Monitor memory usage
ps aux | grep server | grep -v grep | awk '{print $6}' # RSS in KB
```

## Next Steps

1. **Verify RTSP playback**: Connect to stream and confirm video
2. **Monitor fragmentation**: Track if 163-file issue is resolved
3. **Optimize buffer size**: Adjust based on network conditions
4. **Add error recovery**: Handle ffmpeg crashes gracefully
5. **Implement health checks**: Monitor stream status

## File Locations

- **Server binary**: `/home/grupo-jl/jt808-broker/server`
- **Source code**: `/home/grupo-jl/jt808-broker/internal/tcp/media_listener.go`
- **H.264 backup files**: `/home/grupo-jl/jt808-broker/streams/`
- **ffmpeg binary**: `/usr/bin/ffmpeg`
- **RTSP server**: `localhost:8554` (default port)

## Default RTSP URLs

- Format: `rtsp://localhost:8554/cam_DEVICEID_chCHANNEL`
- Examples:
  - `rtsp://localhost:8554/cam_816200000119_ch0` 
  - `rtsp://localhost:8554/cam_816200000119_ch147`
