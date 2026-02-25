#!/bin/bash
# Generate a valid H.264 test file

mkdir -p streams

# Create a binary file with SPS, PPS, and IDR frame
{
    # SPS (Sequence Parameter Set) with 4-byte start code
    printf '\x00\x00\x00\x01'  # 4-byte start code
    printf '\x67'              # NAL type 7 (SPS)
    printf '\x42\x00\x0a\xff\xe1\x00\x16\x68\xd9\x40\x50\x00\x00\x00\x00\x00'
    printf '\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00'
    
    # PPS (Picture Parameter Set) with 4-byte start code
    printf '\x00\x00\x00\x01'  # 4-byte start code
    printf '\x68'              # NAL type 8 (PPS)
    printf '\xae\x08'
    
    # IDR frame with 4-byte start code
    printf '\x00\x00\x00\x01'  # 4-byte start code
    printf '\x65'              # NAL type 5 (IDR)
    printf '\x00\x00\x00\x00\x00\x00\x00\x00'
    printf '\x00\x00\x00\x00\x00\x00\x00\x00'
    
} > streams/test_synthetic.h264

echo "✅ Created streams/test_synthetic.h264"
xxd streams/test_synthetic.h264 | head -15
