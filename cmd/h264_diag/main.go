package main

import (
	"fmt"
	"log"
	"os"

	"jt808-broker/internal/protocol"
)

// H264 Stream Diagnostic Tool
// Validates H264 files and provides detailed analysis

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: h264_diag <file.h264>")
		fmt.Println("")
		fmt.Println("Example:")
		fmt.Println("  go run cmd/h264_diag/main.go streams/device_CH1_20260225.h264")
		os.Exit(1)
	}

	filePath := os.Args[1]

	fmt.Printf("╔════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║ H.264 Stream Diagnostic Tool                                   ║\n")
	fmt.Printf("╚════════════════════════════════════════════════════════════════╝\n")
	fmt.Printf("\n")
	fmt.Printf("Analyzing: %s\n\n", filePath)

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	fileSize := len(data)
	fmt.Printf("📁 File size: %d bytes (%.2f MB)\n\n", fileSize, float64(fileSize)/(1024*1024))

	// Check for start code at beginning
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("1. START CODE CHECK\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	if protocol.HasStartCode(data) {
		fmt.Printf("✅ File starts with valid NAL start code\n")
		if len(data) >= 4 && data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x00 && data[3] == 0x01 {
			fmt.Printf("   Start code: 4-byte (00 00 00 01)\n")
		} else {
			fmt.Printf("   Start code: 3-byte (00 00 01)\n")
		}
	} else {
		fmt.Printf("❌ File does NOT start with NAL start code\n")
		fmt.Printf("   First 16 bytes: % X\n", data[:min(16, len(data))])
	}
	fmt.Printf("\n")

	// Create detector and analyze
	detector := protocol.NewNALDetector()
	info := detector.AnalyzeStream(data)

	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("2. NAL UNIT ANALYSIS\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Total NAL units found: %d\n", info.TotalNALs)
	fmt.Printf("\n")

	// Extract and display NAL units
	units := detector.ExtractNALUnits(data)
	if len(units) > 0 {
		fmt.Printf("NAL Unit breakdown:\n")
		nalCounts := make(map[uint8]int)
		for _, unit := range units {
			nalCounts[unit.Type]++
		}

		for nalType, count := range nalCounts {
			fmt.Printf("  Type %2d (%s): %d units\n",
				nalType, protocol.GetNALTypeName(nalType), count)
		}
		fmt.Printf("\n")

		// Show first few NAL units
		fmt.Printf("First NAL units (max 10):\n")
		for i, unit := range units {
			if i >= 10 {
				break
			}
			fmt.Printf("  [%2d] Type=%d (%s), Size=%d bytes, StartCode=%d bytes\n",
				i, unit.Type, protocol.GetNALTypeName(unit.Type),
				len(unit.Data), len(unit.StartCode))
		}
	} else {
		fmt.Printf("❌ No NAL units found!\n")
	}
	fmt.Printf("\n")

	// Check stream readiness
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("3. STREAM VALIDATION\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	if info.HasSPS {
		fmt.Printf("✅ SPS (type 7) found: %d bytes\n", len(info.SPS))
	} else {
		fmt.Printf("❌ SPS (type 7) NOT FOUND - FFmpeg cannot decode\n")
	}

	if info.HasPPS {
		fmt.Printf("✅ PPS (type 8) found: %d bytes\n", len(info.PPS))
	} else {
		fmt.Printf("❌ PPS (type 8) NOT FOUND - FFmpeg cannot decode\n")
	}

	if info.HasIDR {
		fmt.Printf("✅ IDR frame (type 5) found: %d keyframes\n", info.KeyFrames)
	} else {
		fmt.Printf("❌ IDR frame (type 5) NOT FOUND - stream may not play\n")
	}

	fmt.Printf("\n")
	fmt.Printf("Frame statistics:\n")
	fmt.Printf("  I-frames (keyframes): %d\n", info.KeyFrames)
	fmt.Printf("  P-frames: %d\n", info.PFrames)
	fmt.Printf("\n")

	if info.StreamReady {
		fmt.Printf("✅ Stream is VALID and ready for playback\n")
	} else {
		fmt.Printf("❌ Stream is INVALID - missing required NAL units\n")
	}
	fmt.Printf("\n")

	// SPS/PPS extraction
	if info.HasSPS && info.HasPPS {
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("4. SPS/PPS DETAILS\n")
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

		// Parse SPS
		spsData, err := protocol.ParseSPS(info.SPS)
		if err != nil {
			fmt.Printf("❌ SPS parsing failed: %v\n", err)
		} else {
			fmt.Printf("SPS Information:\n")
			fmt.Printf("  Profile IDC: %d\n", spsData.ProfileIdc)
			fmt.Printf("  Level IDC: %d (%.1f)\n", spsData.LevelIdc, float32(spsData.LevelIdc)/10.0)
			if spsData.Width > 0 && spsData.Height > 0 {
				fmt.Printf("  Resolution: %d x %d\n", spsData.Width, spsData.Height)
			} else {
				fmt.Printf("  Resolution: Not parsed\n")
			}
		}
		fmt.Printf("\n")
	}

	// Validation test
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("5. VALIDATION TEST\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	err = protocol.ValidateH264Stream(data)
	if err != nil {
		fmt.Printf("❌ Validation FAILED: %v\n", err)
	} else {
		fmt.Printf("✅ Validation PASSED\n")
	}
	fmt.Printf("\n")

	// Summary
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("6. SUMMARY\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	if info.StreamReady && err == nil {
		fmt.Printf("🎉 Stream is VALID and can be played by FFmpeg/VLC\n")
		fmt.Printf("\n")
		fmt.Printf("Test playback with:\n")
		fmt.Printf("  ffplay -fflags nobuffer %s\n", filePath)
		fmt.Printf("  vlc %s\n", filePath)
	} else {
		fmt.Printf("❌ Stream is INVALID and cannot be played\n")
		fmt.Printf("\n")
		fmt.Printf("Issues found:\n")
		if !info.HasSPS {
			fmt.Printf("  • Missing SPS (Sequence Parameter Set)\n")
		}
		if !info.HasPPS {
			fmt.Printf("  • Missing PPS (Picture Parameter Set)\n")
		}
		if !info.HasIDR {
			fmt.Printf("  • Missing IDR frame (I-frame/keyframe)\n")
		}
		if !protocol.HasStartCode(data) {
			fmt.Printf("  • File doesn't start with NAL start code\n")
		}
	}

	fmt.Printf("\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
