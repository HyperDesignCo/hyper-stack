// Package brand draws the Hyper Stack icon, so every platform shows the same one.
package brand

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

// PNG returns the icon as a size x size PNG: a rounded indigo square with a white "H".
func PNG(size int) []byte {
	const n = 32 // the design grid
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{0x4f, 0x46, 0xe5, 0xff}
	fg := color.RGBA{0xff, 0xff, 0xff, 0xff}
	isH := func(x, y int) bool {
		switch {
		case y >= 8 && y < 24 && (x >= 9 && x <= 11 || x >= 20 && x <= 22):
			return true
		case (y == 15 || y == 16) && x >= 11 && x < 21:
			return true
		}
		return false
	}
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			x, y := px*n/size, py*n/size
			dx, dy := min(x, n-1-x), min(y, n-1-y)
			if dx < 6 && dy < 6 && (6-dx)*(6-dx)+(6-dy)*(6-dy) > 36 {
				continue // rounded corner
			}
			if isH(x, y) {
				img.Set(px, py, fg)
			} else {
				img.Set(px, py, bg)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// ICO returns a 32x32 PNG-in-ICO file.
func ICO() []byte {
	const n = 32
	p := PNG(n)
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{0, 1, 1}) // reserved, type=icon, count
	ico.Write([]byte{n, n, 0, 0})                                  // width, height, colors, reserved
	_ = binary.Write(&ico, binary.LittleEndian, []uint16{1, 32})   // planes, bpp
	_ = binary.Write(&ico, binary.LittleEndian, uint32(len(p)))    //nolint:gosec // a 32x32 PNG is far below 4 GiB
	_ = binary.Write(&ico, binary.LittleEndian, uint32(22))        // data offset
	ico.Write(p)
	return ico.Bytes()
}
