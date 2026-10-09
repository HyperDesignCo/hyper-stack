//go:build windows

// Package tray shows the Hyper Stack icon in the system tray (Windows).
package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

// icon returns a 32x32 PNG-in-ICO: a rounded indigo square with a white "H".
func icon() []byte {
	const n = 32
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	bg := color.RGBA{0x4f, 0x46, 0xe5, 0xff}
	fg := color.RGBA{0xff, 0xff, 0xff, 0xff}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			// rounded corners, radius 6
			dx, dy := min(x, n-1-x), min(y, n-1-y)
			if dx < 6 && dy < 6 && (6-dx)*(6-dx)+(6-dy)*(6-dy) > 36 {
				continue
			}
			img.Set(x, y, bg)
		}
	}
	for y := 8; y < 24; y++ { // letter H: two stems and a bar
		for _, x := range []int{9, 10, 11, 20, 21, 22} {
			img.Set(x, y, fg)
		}
	}
	for x := 11; x < 21; x++ {
		img.Set(x, 15, fg)
		img.Set(x, 16, fg)
	}
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)

	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{0, 1, 1})    // reserved, type=icon, count
	ico.Write([]byte{n, n, 0, 0})                                     // width, height, colors, reserved
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{1, 32})      // planes, bpp
	_ = binary.Write(&ico, binary.LittleEndian, uint32(pngBuf.Len())) //nolint:gosec // a 32x32 PNG is far below 4 GiB
	_ = binary.Write(&ico, binary.LittleEndian, uint32(22))           // data offset
	ico.Write(pngBuf.Bytes())
	return ico.Bytes()
}
