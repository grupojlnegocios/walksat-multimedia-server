package protocol

import (
	"fmt"
	"log"
)

// SPSData contém informações extraídas do SPS (Sequence Parameter Set)
type SPSData struct {
	ProfileIdc                uint8
	LevelIdc                  uint8
	SeqParamSetID             uint8
	Log2MaxFrameNumMinus4     uint8
	PicOrderCntType           uint8
	Log2MaxPicOrderCntLsb     uint8
	NumRefFrames              uint8
	FrameMBSOnlyFlag          uint8
	PicWidthInMBsMinus1       uint32
	PicHeightInMapUnitsMinus1 uint32
	CropLeft                  uint32
	CropRight                 uint32
	CropTop                   uint32
	CropBottom                uint32
	// Calculated values
	Width  uint32
	Height uint32
	Valid  bool
}

// BitStream helper for parsing
type BitStream struct {
	Data  []byte
	Index int // bit index
}

// ReadBit reads a single bit
func (bs *BitStream) ReadBit() (uint8, error) {
	if bs.Index >= len(bs.Data)*8 {
		return 0, fmt.Errorf("bitstream overflow")
	}
	byteIdx := bs.Index / 8
	bitIdx := 7 - (bs.Index % 8)
	bit := (bs.Data[byteIdx] >> uint(bitIdx)) & 1
	bs.Index++
	return bit, nil
}

// ReadBits reads n bits
func (bs *BitStream) ReadBits(n int) (uint32, error) {
	if n <= 0 || n > 32 {
		return 0, fmt.Errorf("invalid bit count: %d", n)
	}
	var result uint32
	for i := 0; i < n; i++ {
		bit, err := bs.ReadBit()
		if err != nil {
			return 0, err
		}
		result = (result << 1) | uint32(bit)
	}
	return result, nil
}

// ReadExpGolomb reads Exp-Golomb coded unsigned integer
func (bs *BitStream) ReadExpGolomb() (uint32, error) {
	leadingZeroBits := 0
	for {
		bit, err := bs.ReadBit()
		if err != nil {
			return 0, err
		}
		if bit != 0 {
			break
		}
		leadingZeroBits++
	}

	if leadingZeroBits == 0 {
		return 0, nil
	}

	bits, err := bs.ReadBits(leadingZeroBits)
	if err != nil {
		return 0, err
	}

	return (1 << uint(leadingZeroBits)) - 1 + bits, nil
}

// ParseSPS parses H.264 SPS and extracts width/height
// Input: SPS data WITHOUT start code, starting at NAL header byte
func ParseSPS(spsData []byte) (*SPSData, error) {
	if len(spsData) < 4 {
		return nil, fmt.Errorf("SPS too short: %d bytes", len(spsData))
	}

	// First byte is NAL header
	nalHeader := spsData[0]
	nalRefIdc := (nalHeader >> 5) & 0x03
	nalUnitType := nalHeader & 0x1F

	if nalUnitType != 7 {
		return nil, fmt.Errorf("not an SPS (NAL type %d, expected 7)", nalUnitType)
	}

	log.Printf("[SPS_PARSER] NAL RefIdc=%d, UnitType=%d\n", nalRefIdc, nalUnitType)

	// Create bitstream from SPS payload (skip NAL header)
	bs := &BitStream{
		Data:  spsData,
		Index: 8, // Skip first byte (NAL header)
	}

	sps := &SPSData{}

	// profile_idc (8 bits)
	profileIdc, err := bs.ReadBits(8)
	if err != nil {
		return nil, err
	}
	sps.ProfileIdc = uint8(profileIdc)
	log.Printf("[SPS_PARSER] profile_idc=%d\n", sps.ProfileIdc)

	// constraint flags (6 bits) + reserved (2 bits)
	_, err = bs.ReadBits(8)
	if err != nil {
		return nil, err
	}

	// level_idc (8 bits)
	levelIdc, err := bs.ReadBits(8)
	if err != nil {
		return nil, err
	}
	sps.LevelIdc = uint8(levelIdc)
	log.Printf("[SPS_PARSER] level_idc=%d (level %.1f)\n", sps.LevelIdc, float32(sps.LevelIdc)/10.0)

	// seq_parameter_set_id (ue(v))
	seqParamSetID, err := bs.ReadExpGolomb()
	if err != nil {
		return nil, err
	}
	sps.SeqParamSetID = uint8(seqParamSetID)

	// log2_max_frame_num_minus4 (ue(v))
	log2MaxFrameNum, err := bs.ReadExpGolomb()
	if err != nil {
		return nil, err
	}
	sps.Log2MaxFrameNumMinus4 = uint8(log2MaxFrameNum)

	// pic_order_cnt_type (ue(v))
	picOrderCntType, err := bs.ReadExpGolomb()
	if err != nil {
		return nil, err
	}
	sps.PicOrderCntType = uint8(picOrderCntType)

	if sps.PicOrderCntType == 0 {
		// log2_max_pic_order_cnt_lsb_minus4 (ue(v))
		log2MaxPicOrderCnt, err := bs.ReadExpGolomb()
		if err != nil {
			return nil, err
		}
		sps.Log2MaxPicOrderCntLsb = uint8(log2MaxPicOrderCnt)
	} else if sps.PicOrderCntType == 1 {
		// delta_pic_order_always_zero_flag (1 bit) - skip
		_, err := bs.ReadBit()
		if err != nil {
			return nil, err
		}
		// offset_for_non_ref_pic (se(v)) - skip
		_, err = bs.ReadExpGolomb()
		if err != nil {
			return nil, err
		}
		// offset_for_top_to_bottom_field (se(v)) - skip
		_, err = bs.ReadExpGolomb()
		if err != nil {
			return nil, err
		}
		// num_ref_frames_in_pic_order_cnt_cycle (ue(v))
		numRefFrames, err := bs.ReadExpGolomb()
		if err != nil {
			return nil, err
		}
		// offset_for_ref_frame (se(v)) - read numRefFrames times
		for i := 0; i < int(numRefFrames); i++ {
			_, err := bs.ReadExpGolomb()
			if err != nil {
				return nil, err
			}
		}
	}

	// num_ref_frames (ue(v))
	numRefFrames, err := bs.ReadExpGolomb()
	if err != nil {
		return nil, err
	}
	sps.NumRefFrames = uint8(numRefFrames)

	// gaps_in_frame_num_value_allowed_flag (1 bit) - skip
	_, err = bs.ReadBit()
	if err != nil {
		return nil, err
	}

	// pic_width_in_mbs_minus1 (ue(v))
	picWidthInMBsMinus1, err := bs.ReadExpGolomb()
	if err != nil {
		return nil, err
	}
	sps.PicWidthInMBsMinus1 = picWidthInMBsMinus1
	log.Printf("[SPS_PARSER] pic_width_in_mbs_minus1=%d\n", sps.PicWidthInMBsMinus1)

	// pic_height_in_map_units_minus1 (ue(v))
	picHeightInMapUnitsMinus1, err := bs.ReadExpGolomb()
	if err != nil {
		return nil, err
	}
	sps.PicHeightInMapUnitsMinus1 = picHeightInMapUnitsMinus1
	log.Printf("[SPS_PARSER] pic_height_in_map_units_minus1=%d\n", sps.PicHeightInMapUnitsMinus1)

	// frame_mbs_only_flag (1 bit)
	frameMBSOnly, err := bs.ReadBit()
	if err != nil {
		return nil, err
	}
	sps.FrameMBSOnlyFlag = frameMBSOnly
	log.Printf("[SPS_PARSER] frame_mbs_only_flag=%d\n", sps.FrameMBSOnlyFlag)

	if sps.FrameMBSOnlyFlag == 0 {
		// mbaff (mb_adaptive_frame_field_flag) - skip
		_, err := bs.ReadBit()
		if err != nil {
			return nil, err
		}
	}

	// direct_8x8_inference_flag (1 bit) - skip
	_, err = bs.ReadBit()
	if err != nil {
		return nil, err
	}

	// frame_cropping_flag (1 bit)
	frameCroppingFlag, err := bs.ReadBit()
	if err != nil {
		return nil, err
	}

	if frameCroppingFlag != 0 {
		// frame_crop_left_offset (ue(v))
		cropLeft, err := bs.ReadExpGolomb()
		if err != nil {
			return nil, err
		}
		sps.CropLeft = cropLeft

		// frame_crop_right_offset (ue(v))
		cropRight, err := bs.ReadExpGolomb()
		if err != nil {
			return nil, err
		}
		sps.CropRight = cropRight

		// frame_crop_top_offset (ue(v))
		cropTop, err := bs.ReadExpGolomb()
		if err != nil {
			return nil, err
		}
		sps.CropTop = cropTop

		// frame_crop_bottom_offset (ue(v))
		cropBottom, err := bs.ReadExpGolomb()
		if err != nil {
			return nil, err
		}
		sps.CropBottom = cropBottom

		log.Printf("[SPS_PARSER] Frame cropping: L=%d R=%d T=%d B=%d\n",
			sps.CropLeft, sps.CropRight, sps.CropTop, sps.CropBottom)
	}

	// Calculate actual width and height
	// Width = ((pic_width_in_mbs_minus1 + 1) * 16) - frame_crop_left_offset * 2 - frame_crop_right_offset * 2
	// Height = ((pic_height_in_map_units_minus1 + 1) * 16) - frame_crop_top_offset * 2 - frame_crop_bottom_offset * 2
	//         (for frame_mbs_only_flag = 1)
	// or     = ((pic_height_in_map_units_minus1 + 1) * 32) - ... (for frame_mbs_only_flag = 0)

	sps.Width = ((sps.PicWidthInMBsMinus1 + 1) * 16) - (sps.CropLeft+sps.CropRight)*2
	if sps.FrameMBSOnlyFlag != 0 {
		sps.Height = ((sps.PicHeightInMapUnitsMinus1 + 1) * 16) - (sps.CropTop+sps.CropBottom)*2
	} else {
		sps.Height = ((sps.PicHeightInMapUnitsMinus1 + 1) * 32) - (sps.CropTop+sps.CropBottom)*2
	}

	log.Printf("[SPS_PARSER] ✓ Calculated resolution: %dx%d\n", sps.Width, sps.Height)

	// Validate
	if sps.Width == 0 || sps.Height == 0 {
		return nil, fmt.Errorf("invalid resolution: %dx%d", sps.Width, sps.Height)
	}

	if sps.Width > 8192 || sps.Height > 8192 {
		return nil, fmt.Errorf("resolution out of bounds: %dx%d", sps.Width, sps.Height)
	}

	sps.Valid = true
	return sps, nil
}

// ValidateSPSIntegrity checks if SPS data is complete and valid
func ValidateSPSIntegrity(spsData []byte) bool {
	if len(spsData) < 4 {
		log.Printf("[SPS_PARSER] SPS too short: %d bytes\n", len(spsData))
		return false
	}

	// Must have start code
	if !(len(spsData) >= 4 &&
		spsData[0] == 0x00 && spsData[1] == 0x00 &&
		spsData[2] == 0x00 && spsData[3] == 0x01) &&
		!(len(spsData) >= 3 &&
			spsData[0] == 0x00 && spsData[1] == 0x00 &&
			spsData[2] == 0x01) {
		log.Printf("[SPS_PARSER] SPS missing start code\n")
		return false
	}

	// Must be NAL type 7
	var nalStart int
	if spsData[0] == 0x00 && spsData[1] == 0x00 {
		if spsData[2] == 0x00 {
			nalStart = 4
		} else {
			nalStart = 3
		}
	}

	if nalStart >= len(spsData) {
		log.Printf("[SPS_PARSER] No data after start code\n")
		return false
	}

	nalType := spsData[nalStart] & 0x1F
	if nalType != 7 {
		log.Printf("[SPS_PARSER] Not SPS (NAL type %d)\n", nalType)
		return false
	}

	return true
}
