package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"

	"github.com/alexeikramm/sovereign-client/internal/ui/render"
)

func main() {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	icoBytes := generateICO(sizes)
	if err := os.WriteFile("cmd/sovereign-client/app.ico", icoBytes, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write ico: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Wrote cmd/sovereign-client/app.ico")

	// Run rsrc to create cmd/sovereign-client/rsrc.syso
	cmd := exec.Command("rsrc", "-ico", "cmd/sovereign-client/app.ico", "-o", "cmd/sovereign-client/rsrc.syso")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "rsrc: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Generated cmd/sovereign-client/rsrc.syso")
}

func generateICO(sizes []int) []byte {
	type entry struct {
		size int
		png  []byte
	}
	var entries []entry
	for _, sz := range sizes {
		img := render.RenderSovereignLogo(sz)
		// Convert to non-premultiplied NRGBA for PNG encoding
		nrgba := image.NewNRGBA(image.Rect(0, 0, sz, sz))
		for y := 0; y < sz; y++ {
			for x := 0; x < sz; x++ {
				off := img.PixOffset(x, y)
				a := img.Pix[off+3]
				if a == 0 {
					continue
				}
				r := uint8((uint32(img.Pix[off]) * 255) / uint32(a))
				g := uint8((uint32(img.Pix[off+1]) * 255) / uint32(a))
				b := uint8((uint32(img.Pix[off+2]) * 255) / uint32(a))
				nrgba.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
			}
		}

		var buf bytes.Buffer
		if err := png.Encode(&buf, nrgba); err != nil {
			panic(err)
		}
		entries = append(entries, entry{size: sz, png: buf.Bytes()})
	}

	var out bytes.Buffer
	out.Write([]byte{0, 0}) // reserved
	binary.Write(&out, binary.LittleEndian, uint16(1)) // ICON type
	binary.Write(&out, binary.LittleEndian, uint16(len(entries))) // count

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		w := byte(e.size)
		if e.size >= 256 {
			w = 0
		}
		out.WriteByte(w)
		out.WriteByte(w)
		out.WriteByte(0) // palette
		out.WriteByte(0) // reserved
		binary.Write(&out, binary.LittleEndian, uint16(1)) // planes
		binary.Write(&out, binary.LittleEndian, uint16(32)) // bpp
		binary.Write(&out, binary.LittleEndian, uint32(len(e.png)))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(e.png)
	}

	for _, e := range entries {
		out.Write(e.png)
	}
	return out.Bytes()
}
