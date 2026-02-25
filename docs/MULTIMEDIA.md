# JT808 Multimedia Implementation

## Features Implemented

### 1. Multimedia Message Parsing

#### 0x0800 - Multimedia Event Upload
- Parses multimedia event notifications from devices
- Extracts:
  - Multimedia ID (unique identifier)
  - Media Type (Image/Audio/Video)
  - Media Format (JPEG, MP4, H.264, etc.)
  - Event Code (Platform Command, Timed Action, Alarm triggers)
  - Channel ID

#### 0x0801 - Multimedia Data Upload
- Handles actual multimedia file data transfer
- Supports:
  - Optional location report (28 bytes)
  - Variable-length data packets
  - Multiple packet reassembly

### 2. Multimedia Storage System

**MultimediaStore** manages file storage with:
- Automatic directory creation per device
- File naming: `YYYYMMDD_HHMMSS_chX_idY.ext`
- Packet reassembly for multi-part uploads
- Missing packet detection and retransmission requests
- Automatic file extension detection based on format

### 3. Supported Media Formats

#### Images
- JPEG (.jpg)
- TIF (.tif)

#### Audio
- WAV (.wav)
- MP3 (.mp3)

#### Video
- AVI (.avi)
- WMV (.wmv)
- RMVB (.rmvb)
- FLV (.flv)
- MP4 (.mp4)
- H.264 (.h264)

### 4. Event Codes Supported

- Platform Command
- Timed Action
- Robbery Alarm
- Collision Rollover Alarm
- Door Open/Close Capture
- Door Open Over Threshold
- Distance Capture

## File Structure

```
./streams/
└── {DeviceID}/
    └── multimedia/
        ├── 20260211_143025_ch1_id1234.jpg
        ├── 20260211_143126_ch1_id1235.mp4
        └── ...
```

## Message Flow

### Single Packet Upload (0x0801)
1. Device sends multimedia data with complete file
2. Server parses and extracts data
3. Server saves file to disk
4. Server responds with 0x8800 (no retransmission needed)

### Multi-Packet Upload (0x0800 + multiple 0x0801)
1. Device sends 0x0800 event notification
2. Server initializes upload tracking
3. Device sends multiple 0x0801 packets
4. Server tracks received packets
5. Server detects missing packets
6. Server responds with 0x8800 requesting retransmission
7. Device retransmits missing packets
8. Server completes and saves file

## Response Messages

### 0x8800 - Multimedia Upload Response
- Confirms receipt of multimedia data
- Includes list of missing packet IDs for retransmission
- Empty list = all packets received successfully

## Logging

All multimedia operations are logged with:
- `[MULTIMEDIA]` - Protocol-level parsing
- `[MULTIMEDIA_STORE]` - Storage operations
- `[JT808_SESSION]` - Session-level handling

Example logs:
```
[MULTIMEDIA] Event parsed - ID: 1234, Type: Video, Format: MP4, Event: Platform Command, Channel: 1
[MULTIMEDIA_STORE] Started upload - Device: 000000000000, ID: 1234, Type: Video, File: ./streams/000000000000/multimedia/20260211_143025_ch1_id1234.mp4
[MULTIMEDIA] Data parsed - ID: 1234, Type: Video, Format: MP4, Event: Platform Command, Channel: 1, Total: 524288 bytes
[MULTIMEDIA_STORE] Upload completed - Device: 000000000000, ID: 1234, File: ./streams/000000000000/multimedia/20260211_143025_ch1_id1234.mp4, Size: 524288 bytes, Duration: 2.5s
```

## Future Enhancements

- Database storage of multimedia metadata
- HTTP API to browse and download files
- Thumbnail generation for images/videos
- Streaming support for large files
- Cleanup of old files
- Compression support
