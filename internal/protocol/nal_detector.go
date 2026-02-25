package protocol

import (
	"bytes"
	"fmt"
	"log"
)

// ============================================================================
// H.264 NAL Unit Detection and Validation
// ============================================================================

// NAL Unit Types (lower 5 bits of NAL header)
const (
	NALUnitTypeUnspecified      uint8 = 0
	NALUnitTypeCodedSliceNonIDR uint8 = 1 // P-frame
	NALUnitTypeCodedSliceIDR    uint8 = 5 // I-frame (keyframe)
	NALUnitTypeSEI              uint8 = 6 // Supplemental Enhancement Information
	NALUnitTypeSPS              uint8 = 7 // Sequence Parameter Set
	NALUnitTypePPS              uint8 = 8 // Picture Parameter Set
	NALUnitTypeAUD              uint8 = 9 // Access Unit Delimiter
	NALUnitTypeEndOfSequence    uint8 = 10
	NALUnitTypeEndOfStream      uint8 = 11
	NALUnitTypeFiller           uint8 = 12
)

// H264NALUnit represents a single NAL unit
type H264NALUnit struct {
	Type       uint8  // NAL unit type (lower 5 bits)
	Data       []byte // Complete NAL unit including header
	StartCode  []byte // Start code that preceded it (0x000001 or 0x00000001)
	RefIdc     uint8  // nal_ref_idc (bits 5-6)
	HeaderByte uint8  // Complete NAL header byte
}

// H264StreamInfo contains stream parameters
type H264StreamInfo struct {
	HasSPS      bool
	HasPPS      bool
	HasIDR      bool
	SPS         []byte
	PPS         []byte
	SPSUnits    []*H264NALUnit
	PPSUnits    []*H264NALUnit
	IsValid     bool
	TotalNALs   int
	KeyFrames   int
	PFrames     int
	StreamReady bool // true when has SPS+PPS+IDR
}

// NALDetector provides NAL unit detection and stream validation
type NALDetector struct {
	info H264StreamInfo
}

// NewNALDetector creates a new NAL detector
func NewNALDetector() *NALDetector {
	return &NALDetector{
		info: H264StreamInfo{
			SPSUnits: make([]*H264NALUnit, 0),
			PPSUnits: make([]*H264NALUnit, 0),
		},
	}
}

// ExtractNALUnits extracts all NAL units from H.264 data
func (nd *NALDetector) ExtractNALUnits(data []byte) []*H264NALUnit {
	if len(data) < 4 {
		return nil
	}

	var units []*H264NALUnit
	pos := 0

	for pos < len(data) {
		// Find next start code
		startCodePos, startCodeLen := findStartCode(data, pos)
		if startCodePos < 0 {
			break
		}

		// Find next start code to determine NAL unit boundary
		nextStartPos, _ := findStartCode(data, startCodePos+startCodeLen)

		var nalData []byte
		if nextStartPos < 0 {
			// Last NAL unit
			nalData = data[startCodePos+startCodeLen:]
		} else {
			// NAL unit up to next start code
			nalData = data[startCodePos+startCodeLen : nextStartPos]
		}

		// Extract NAL unit if valid
		if len(nalData) > 0 {
			unit := &H264NALUnit{
				HeaderByte: nalData[0],
				Type:       nalData[0] & 0x1F,
				RefIdc:     (nalData[0] >> 5) & 0x03,
				Data:       nalData,
				StartCode:  data[startCodePos : startCodePos+startCodeLen],
			}
			units = append(units, unit)
		}

		// Move to next position
		if nextStartPos < 0 {
			break
		}
		pos = nextStartPos
	}

	return units
}

// findStartCode finds the next NAL start code (0x000001 or 0x00000001)
// Returns position and length of start code, or (-1, 0) if not found
func findStartCode(data []byte, start int) (int, int) {
	for i := start; i < len(data)-2; i++ {
		// Check for 4-byte start code: 0x00 0x00 0x00 0x01
		if i < len(data)-3 &&
			data[i] == 0x00 &&
			data[i+1] == 0x00 &&
			data[i+2] == 0x00 &&
			data[i+3] == 0x01 {
			return i, 4
		}

		// Check for 3-byte start code: 0x00 0x00 0x01
		if data[i] == 0x00 &&
			data[i+1] == 0x00 &&
			data[i+2] == 0x01 {
			return i, 3
		}
	}
	return -1, 0
}

// HasStartCode checks if data begins with a NAL start code
func HasStartCode(data []byte) bool {
	if len(data) < 3 {
		return false
	}
	// 4-byte start code
	if len(data) >= 4 && data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x00 && data[3] == 0x01 {
		return true
	}
	// 3-byte start code
	if data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01 {
		return true
	}
	return false
}

// AnalyzeStream analyzes H.264 data and updates stream info
func (nd *NALDetector) AnalyzeStream(data []byte) *H264StreamInfo {
	units := nd.ExtractNALUnits(data)

	nd.info.TotalNALs += len(units)

	for _, unit := range units {
		switch unit.Type {
		case NALUnitTypeSPS:
			if !nd.info.HasSPS {
				nd.info.HasSPS = true
				nd.info.SPS = unit.Data
				log.Printf("[NAL_DETECTOR] ✓ Found SPS: %d bytes\n", len(unit.Data))
			}
			nd.info.SPSUnits = append(nd.info.SPSUnits, unit)

		case NALUnitTypePPS:
			if !nd.info.HasPPS {
				nd.info.HasPPS = true
				nd.info.PPS = unit.Data
				log.Printf("[NAL_DETECTOR] ✓ Found PPS: %d bytes\n", len(unit.Data))
			}
			nd.info.PPSUnits = append(nd.info.PPSUnits, unit)

		case NALUnitTypeCodedSliceIDR:
			nd.info.HasIDR = true
			nd.info.KeyFrames++
			log.Printf("[NAL_DETECTOR] ✓ Found IDR (I-frame): %d bytes\n", len(unit.Data))

		case NALUnitTypeCodedSliceNonIDR:
			nd.info.PFrames++
		}
	}

	// Check if stream is ready
	nd.info.StreamReady = nd.info.HasSPS && nd.info.HasPPS && nd.info.HasIDR
	nd.info.IsValid = nd.info.StreamReady

	return &nd.info
}

// GetStreamInfo returns current stream info
func (nd *NALDetector) GetStreamInfo() *H264StreamInfo {
	return &nd.info
}

// IsStreamReady checks if stream has required NAL units for playback
func (nd *NALDetector) IsStreamReady() bool {
	return nd.info.HasSPS && nd.info.HasPPS && nd.info.HasIDR
}

// Reset resets the detector state
func (nd *NALDetector) Reset() {
	nd.info = H264StreamInfo{
		SPSUnits: make([]*H264NALUnit, 0),
		PPSUnits: make([]*H264NALUnit, 0),
	}
}

// ExtractSPSPPS extracts first SPS and PPS from data
func ExtractSPSPPS(data []byte) (sps []byte, pps []byte, found bool) {
	detector := NewNALDetector()
	units := detector.ExtractNALUnits(data)

	for _, unit := range units {
		if unit.Type == NALUnitTypeSPS && sps == nil {
			// Include start code + data
			sps = make([]byte, len(unit.StartCode)+len(unit.Data))
			copy(sps, unit.StartCode)
			copy(sps[len(unit.StartCode):], unit.Data)
		}
		if unit.Type == NALUnitTypePPS && pps == nil {
			// Include start code + data
			pps = make([]byte, len(unit.StartCode)+len(unit.Data))
			copy(pps, unit.StartCode)
			copy(pps[len(unit.StartCode):], unit.Data)
		}
		if sps != nil && pps != nil {
			found = true
			return
		}
	}

	return sps, pps, found
}

// ValidateH264Stream validates H.264 stream structure
func ValidateH264Stream(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("data too short: %d bytes", len(data))
	}

	// Check for start code at beginning
	if !HasStartCode(data) {
		return fmt.Errorf("missing NAL start code at beginning")
	}

	detector := NewNALDetector()
	info := detector.AnalyzeStream(data)

	if info.TotalNALs == 0 {
		return fmt.Errorf("no NAL units found")
	}

	if !info.HasSPS {
		return fmt.Errorf("missing SPS (type 7)")
	}

	if !info.HasPPS {
		return fmt.Errorf("missing PPS (type 8)")
	}

	if !info.HasIDR {
		return fmt.Errorf("missing IDR frame (type 5)")
	}

	log.Printf("[NAL_DETECTOR] Stream validated: %d NAL units, SPS=%v, PPS=%v, IDR=%v\n",
		info.TotalNALs, info.HasSPS, info.HasPPS, info.HasIDR)

	return nil
}

// PrependSPSPPS prepends SPS and PPS to data if not present
func PrependSPSPPS(data []byte, sps, pps []byte) []byte {
	// Check if data already starts with SPS
	if len(data) > 5 && HasStartCode(data) {
		nalType := data[4] & 0x1F
		if nalType == NALUnitTypeSPS {
			// Already has SPS at start
			return data
		}
	}

	// Prepend SPS and PPS
	result := make([]byte, 0, len(sps)+len(pps)+len(data))
	result = append(result, sps...)
	result = append(result, pps...)
	result = append(result, data...)

	log.Printf("[NAL_DETECTOR] Prepended SPS (%d bytes) + PPS (%d bytes) to frame\n",
		len(sps), len(pps))

	return result
}

// ReconstructWithStartCodes ensures all NAL units have proper 4-byte start codes
func ReconstructWithStartCodes(data []byte) []byte {
	detector := NewNALDetector()
	units := detector.ExtractNALUnits(data)

	if len(units) == 0 {
		return data
	}

	var result bytes.Buffer
	startCode := []byte{0x00, 0x00, 0x00, 0x01}

	for _, unit := range units {
		result.Write(startCode)
		result.Write(unit.Data)
	}

	return result.Bytes()
}

// GetNALTypeName returns human-readable NAL type name
func GetNALTypeName(nalType uint8) string {
	switch nalType {
	case NALUnitTypeUnspecified:
		return "Unspecified"
	case NALUnitTypeCodedSliceNonIDR:
		return "P-frame"
	case NALUnitTypeCodedSliceIDR:
		return "I-frame (IDR)"
	case NALUnitTypeSEI:
		return "SEI"
	case NALUnitTypeSPS:
		return "SPS"
	case NALUnitTypePPS:
		return "PPS"
	case NALUnitTypeAUD:
		return "AUD"
	case NALUnitTypeEndOfSequence:
		return "End of Sequence"
	case NALUnitTypeEndOfStream:
		return "End of Stream"
	case NALUnitTypeFiller:
		return "Filler"
	default:
		return fmt.Sprintf("Unknown (%d)", nalType)
	}
}

// LogNALUnits logs information about NAL units
func LogNALUnits(data []byte, prefix string) {
	detector := NewNALDetector()
	units := detector.ExtractNALUnits(data)

	log.Printf("[%s] Found %d NAL units:\n", prefix, len(units))
	for i, unit := range units {
		log.Printf("[%s]   NAL[%d]: Type=%d (%s), Size=%d, RefIdc=%d\n",
			prefix, i, unit.Type, GetNALTypeName(unit.Type), len(unit.Data), unit.RefIdc)
	}
}

// FindFirstIDRPosition finds the position of the first IDR frame
func FindFirstIDRPosition(data []byte) int {
	detector := NewNALDetector()
	units := detector.ExtractNALUnits(data)

	for _, unit := range units {
		if unit.Type == NALUnitTypeCodedSliceIDR {
			// Find position in original data
			return bytes.Index(data, unit.Data)
		}
	}

	return -1
}

// ContainsSPSPPS checks if data contains both SPS and PPS
func ContainsSPSPPS(data []byte) bool {
	detector := NewNALDetector()
	units := detector.ExtractNALUnits(data)

	hasSPS := false
	hasPPS := false

	for _, unit := range units {
		if unit.Type == NALUnitTypeSPS {
			hasSPS = true
		}
		if unit.Type == NALUnitTypePPS {
			hasPPS = true
		}
		if hasSPS && hasPPS {
			return true
		}
	}

	return false
}
