package main

import "flashback/internal/qr"

// QR draws text as a QR code with a white quiet zone, module px per cell. Returns the size drawn.
func (c *Canvas) QR(x, y, module int, text string) int {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return 0
	}
	n := code.Size + 8
	c.Fill(x, y, n*module, n*module, rgb(0xff, 0xff, 0xff))
	for yy := 0; yy < code.Size; yy++ {
		for xx := 0; xx < code.Size; xx++ {
			if code.Black(xx, yy) {
				c.Fill(x+(xx+4)*module, y+(yy+4)*module, module, module, colBlack)
			}
		}
	}
	return n * module
}

// QRSize is the drawn size of a QR code for text at a given module size.
func QRSize(text string, module int) int {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return 0
	}
	return (code.Size + 8) * module
}
