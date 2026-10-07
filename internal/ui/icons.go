package ui

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
)

// TrayState is the coarse state the tray icon reflects (PLAN.md §3 "System tray states").
type TrayState int

const (
	StateOffline    TrayState = iota // server not running / out of range
	StateConnecting                  // found the phone; establishing or recovering the link
	StateReady                       // authenticated and idle; hotkeys armed
	StateDictating                   // hotkey active; phone mic recording
	StateError                       // unpaired, session busy, or transport failure
)

// stateColor is the pill's state dot colour, reused for the tray so the two surfaces agree.
func stateColor(s TrayState) color.NRGBA {
	switch s {
	case StateConnecting:
		return color.NRGBA{R: 0xF5, G: 0xA5, B: 0x24, A: 0xFF} // amber
	case StateReady:
		return color.NRGBA{R: 0x8B, G: 0x5C, B: 0xF6, A: 0xFF} // violet
	case StateDictating:
		return color.NRGBA{R: 0xE5, G: 0x39, B: 0x35, A: 0xFF} // red
	case StateError:
		return color.NRGBA{R: 0xFF, G: 0x6D, B: 0x00, A: 0xFF} // orange
	default:
		return color.NRGBA{R: 0x9E, G: 0x9E, B: 0x9E, A: 0xFF} // grey
	}
}

// stateLabel is the one-word state name used in the tray tooltip and status item.
func stateLabel(s TrayState) string {
	switch s {
	case StateConnecting:
		return "Connecting"
	case StateReady:
		return "Ready"
	case StateDictating:
		return "Dictating"
	case StateError:
		return "Error"
	default:
		return "Offline"
	}
}

// trayIcon returns the Windows .ico bytes for the state's dot.
func trayIcon(s TrayState) []byte {
	c := stateColor(s)
	return encodeICO([]*image.NRGBA{circleIcon(c, 16), circleIcon(c, 32)})
}

// circleIcon renders an anti-aliased filled disc of radius size/2-1, using 4x4 supersampling so
// the small tray sizes stay smooth without pulling in an image-scaling dependency.
func circleIcon(c color.NRGBA, size int) *image.NRGBA {
	const ss = 4
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	cx := float64(size) / 2
	cy := float64(size) / 2
	r := cx - 1
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			in := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					dx, dy := px-cx, py-cy
					if dx*dx+dy*dy <= r*r {
						in++
					}
				}
			}
			if in == 0 {
				continue
			}
			out := c
			out.A = uint8(int(c.A) * in / (ss * ss))
			img.SetNRGBA(x, y, out)
		}
	}
	return img
}

// encodeICO serialises one or more same-or-smaller square images into a Windows icon file. Each
// image is written as a 32-bpp BITMAPINFOHEADER DIB (bottom-up BGRA) plus the legacy 1-bpp AND
// mask, which Windows Vista+ ignores in favour of the alpha channel but older shells still read.
func encodeICO(imgs []*image.NRGBA) []byte {
	blobs := make([][]byte, len(imgs))
	for i, img := range imgs {
		blobs[i] = encodeDIB(img)
	}

	var buf bytes.Buffer
	buf.Write([]byte{0, 0})              // reserved
	writeUint16(&buf, 1)                 // type: icon
	writeUint16(&buf, uint16(len(imgs))) // image count
	offset := 6 + 16*len(imgs)
	for i, img := range imgs {
		b := img.Bounds()
		w, h := b.Dx(), b.Dy()
		buf.WriteByte(dimByte(w))
		buf.WriteByte(dimByte(h))
		buf.WriteByte(0)      // palette colours (0 = >=256)
		buf.WriteByte(0)      // reserved
		writeUint16(&buf, 1)  // colour planes
		writeUint16(&buf, 32) // bits per pixel
		writeUint32(&buf, uint32(len(blobs[i])))
		writeUint32(&buf, uint32(offset))
		offset += len(blobs[i])
	}
	for _, b := range blobs {
		buf.Write(b)
	}
	return buf.Bytes()
}

// encodeDIB builds the BITMAPINFOHEADER + XOR + AND payload for one icon image.
func encodeDIB(img *image.NRGBA) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	andRow := ((w + 31) / 32) * 4

	var buf bytes.Buffer
	// BITMAPINFOHEADER (40 bytes).
	writeUint32(&buf, 40)
	writeUint32(&buf, uint32(w))
	writeUint32(&buf, uint32(2*h)) // XOR plane + AND plane
	writeUint16(&buf, 1)
	writeUint16(&buf, 32)
	writeUint32(&buf, 0)                      // BI_RGB
	writeUint32(&buf, uint32(w*h*4+andRow*h)) // biSizeImage
	writeUint32(&buf, 0)                      // biXPelsPerMeter
	writeUint32(&buf, 0)                      // biYPelsPerMeter
	writeUint32(&buf, 0)                      // biClrUsed
	writeUint32(&buf, 0)                      // biClrImportant

	// XOR plane: BGRA, bottom-up.
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			px := img.NRGBAAt(b.Min.X+x, b.Min.Y+y)
			buf.Write([]byte{px.B, px.G, px.R, px.A})
		}
	}

	// AND plane: one bit per pixel, 1 = transparent, rows padded to 4 bytes.
	row := make([]byte, andRow)
	for y := h - 1; y >= 0; y-- {
		for i := range row {
			row[i] = 0
		}
		for x := 0; x < w; x++ {
			if img.NRGBAAt(b.Min.X+x, b.Min.Y+y).A < 128 {
				row[x/8] |= 0x80 >> uint(x%8)
			}
		}
		buf.Write(row)
	}
	return buf.Bytes()
}

func dimByte(n int) byte {
	if n >= 256 {
		return 0 // 256 is encoded as 0
	}
	return byte(n)
}

func writeUint16(b *bytes.Buffer, v uint16) {
	var tmp [2]byte
	binary.LittleEndian.PutUint16(tmp[:], v)
	b.Write(tmp[:])
}

func writeUint32(b *bytes.Buffer, v uint32) {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], v)
	b.Write(tmp[:])
}
