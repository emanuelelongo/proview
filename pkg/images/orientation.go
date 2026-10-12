package images

import (
	"bytes"
	"encoding/binary"
)

// JPEGOrientation reads only the main image's EXIF orientation. Missing or
// malformed metadata leaves the pixels unchanged; thumbnails and other IFDs
// must not influence the displayed image.
func JPEGOrientation(data []byte) Transform {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return Transform{}
	}
	for pos := 2; pos < len(data); {
		if data[pos] != 0xff {
			return Transform{}
		}
		for pos < len(data) && data[pos] == 0xff {
			pos++
		}
		if pos >= len(data) {
			break
		}
		marker := data[pos]
		pos++
		if marker == 0xda || marker == 0xd9 {
			break
		} // Do not scan compressed image data.
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if len(data)-pos < 2 {
			break
		}
		length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if length < 2 || length > len(data)-pos {
			break
		}
		payload := data[pos+2 : pos+length]
		if marker == 0xe1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
			if orientation, ok := tiffOrientation(payload[6:]); ok {
				return orientationTransform(orientation)
			}
		}
		pos += length
	}
	return Transform{}
}

func tiffOrientation(data []byte) (uint16, bool) {
	if len(data) < 8 {
		return 0, false
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0, false
	}
	offset := uint64(order.Uint32(data[4:8]))
	if offset < 8 || offset > uint64(len(data)-2) {
		return 0, false
	}
	pos := int(offset)
	count := int(order.Uint16(data[pos : pos+2]))
	pos += 2
	if count > (len(data)-pos)/12 {
		return 0, false
	}
	for i := 0; i < count; i++ {
		entry := data[pos+i*12 : pos+(i+1)*12]
		if order.Uint16(entry[:2]) != 0x0112 {
			continue
		}
		if order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			return 0, false
		}
		value := order.Uint16(entry[8:10])
		return value, value >= 1 && value <= 8
	}
	return 0, false
}

func orientationTransform(orientation uint16) Transform {
	switch orientation {
	case 2:
		return Transform{FlipX: true}
	case 3:
		return Transform{Turns: 2}
	case 4:
		return Transform{FlipY: true}
	case 5:
		return Transform{Turns: 1, FlipY: true}
	case 6:
		return Transform{Turns: 1}
	case 7:
		return Transform{Turns: 1, FlipX: true}
	case 8:
		return Transform{Turns: 3}
	default:
		return Transform{}
	}
}

// TIFFOrientation reads the orientation of the first TIFF image directory.
func TIFFOrientation(data []byte) Transform {
	orientation, ok := tiffOrientation(data)
	if !ok {
		return Transform{}
	}
	return orientationTransform(orientation)
}
