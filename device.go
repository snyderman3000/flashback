package main

// Framebuffer + evdev input for the handhelds Flashback runs on.
//
// Miyoo Mini / Mini+ (OnionOS): mirrors Onion's own display_reset(): read var
// screeninfo, pan to yoffset 0, then write rotated 180° (the panel is mounted
// upside down). Buttons arrive as keyboard keys on /dev/input/event0.
//
// TrimUI Brick / Brick Pro / Smart Pro (NextUI, NX Redux; "tg5040"): the
// framebuffer is upright, and buttons come from the "TRIMUI Player1" gamepad,
// with the d-pad as a hat axis and no key repeat of its own.

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	fbioGetVScreenInfo = 0x4600
	fbioPutVScreenInfo = 0x4601
	fbioGetFScreenInfo = 0x4602
	fbioPanDisplay     = 0x4606
)

// Platform is the device family Flashback is running on.
type Platform struct {
	Name  string // "miyoo" or "tg5040"
	Flip  bool   // panel mounted upside down: draw rotated 180°
	Input string // evdev device to read buttons from
	Pad   bool   // Input is a gamepad (hat d-pad, BTN_* codes, no key repeat)
}

// detectPlatform picks the device family from FLASHBACK_PLATFORM (set by the
// launch script), MinUI's PLATFORM variable, or the input devices present.
func detectPlatform() (Platform, error) {
	name := os.Getenv("FLASHBACK_PLATFORM")
	if name == "" {
		name = os.Getenv("PLATFORM")
	}
	pad := findInputDevice("TRIMUI Player1")
	if name == "" && pad != "" {
		name = "tg5040"
	}
	switch name {
	case "tg5040", "tg5050":
		if pad == "" {
			return Platform{}, fmt.Errorf("no TRIMUI Player1 input device (found: %s)", strings.Join(inputDeviceNames(), ", "))
		}
		return Platform{Name: "tg5040", Input: pad, Pad: true}, nil
	}
	return Platform{Name: "miyoo", Flip: os.Getenv("FLASHBACK_NOFLIP") == "", Input: "/dev/input/event0"}, nil
}

// findInputDevice returns the /dev/input/eventN whose device name matches.
func findInputDevice(name string) string {
	paths, _ := filepath.Glob("/sys/class/input/event*/device/name")
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) == name {
			return "/dev/input/" + filepath.Base(filepath.Dir(filepath.Dir(p)))
		}
	}
	return ""
}

func inputDeviceNames() []string {
	var out []string
	paths, _ := filepath.Glob("/sys/class/input/event*/device/name")
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			out = append(out, filepath.Base(filepath.Dir(filepath.Dir(p)))+"="+strings.TrimSpace(string(b)))
		}
	}
	return out
}

type Framebuffer struct {
	f      *os.File
	mem    []byte
	W, H   int
	stride int
	bpp    int  // 32 or 16 (RGB565)
	redLow bool // 32 bpp with red in the low byte (RGBA in memory) rather than BGRA
	flip   bool
}

func ioctl(fd uintptr, req uintptr, p unsafe.Pointer) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(p))
	if e != 0 {
		return e
	}
	return nil
}

func OpenFramebuffer(flip bool) (*Framebuffer, error) {
	f, err := os.OpenFile("/dev/fb0", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	fb := &Framebuffer{f: f, W: 640, H: 480, bpp: 32, flip: flip}
	var v [40]uint32 // struct fb_var_screeninfo
	xvirt := 640
	if ioctl(f.Fd(), fbioGetVScreenInfo, unsafe.Pointer(&v[0])) == nil && v[0] > 0 {
		fb.W, fb.H = int(v[0]), int(v[1])
		xvirt = int(v[2])
		if v[6] == 16 {
			fb.bpp = 16
		}
		fb.redLow = v[6] == 32 && v[8] == 0 // red.offset
		v[4], v[5] = 0, 0                   // xoffset, yoffset
		_ = ioctl(f.Fd(), fbioPutVScreenInfo, unsafe.Pointer(&v[0]))
		_ = ioctl(f.Fd(), fbioPanDisplay, unsafe.Pointer(&v[0]))
	}
	px := fb.bpp / 8
	fb.stride = xvirt * px
	smem := fb.stride * fb.H
	var fix [96]byte // struct fb_fix_screeninfo
	if ioctl(f.Fd(), fbioGetFScreenInfo, unsafe.Pointer(&fix[0])) == nil {
		ul := int(unsafe.Sizeof(uintptr(0)))
		sl := int(binary.LittleEndian.Uint32(fix[16+ul:]))
		lloff := 16 + ul + 4*4 + 2*3
		lloff = (lloff + 3) &^ 3
		if ll := int(binary.LittleEndian.Uint32(fix[lloff:])); ll >= fb.W*px {
			fb.stride = ll
		}
		if sl >= fb.stride*fb.H {
			smem = sl
		}
	}
	mem, err := syscall.Mmap(int(f.Fd()), 0, smem, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, err
	}
	fb.mem = mem
	return fb, nil
}

// Present copies the canvas (RGBA) to the framebuffer, rotated if needed.
func (fb *Framebuffer) Present(c *Canvas) {
	w, h := fb.W, fb.H
	if c.W < w {
		w = c.W
	}
	if c.H < h {
		h = c.H
	}
	for y := 0; y < h; y++ {
		sy := y
		if fb.flip {
			sy = fb.H - 1 - y
		}
		if sy >= c.H {
			continue
		}
		src := c.Pix[sy*c.Stride:]
		dst := fb.mem[y*fb.stride:]
		for x := 0; x < w; x++ {
			so := x * 4
			if fb.flip {
				if fb.W-1-x >= c.W {
					continue
				}
				so = (fb.W - 1 - x) * 4
			}
			r, g, b := src[so], src[so+1], src[so+2]
			switch {
			case fb.bpp == 16:
				v := uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
				dst[x*2], dst[x*2+1] = byte(v), byte(v>>8)
			case fb.redLow:
				d := x * 4
				dst[d], dst[d+1], dst[d+2], dst[d+3] = r, g, b, 0xff
			default:
				d := x * 4
				dst[d], dst[d+1], dst[d+2], dst[d+3] = b, g, r, 0xff
			}
		}
	}
}

func (fb *Framebuffer) Clear() {
	for i := range fb.mem {
		fb.mem[i] = 0
	}
}

func (fb *Framebuffer) Close() {
	if fb.mem != nil {
		syscall.Munmap(fb.mem)
	}
	fb.f.Close()
}

// Miyoo button → Linux key code mapping. These codes are also Flashback's
// button identifiers on every device.
const (
	BtnMenu   = 1   // ESC
	BtnL2     = 15  // TAB
	BtnR2     = 14  // BACKSPACE
	BtnL1     = 18  // E
	BtnR1     = 20  // T
	BtnStart  = 28  // ENTER
	BtnB      = 29  // LCTRL
	BtnX      = 42  // LSHIFT
	BtnY      = 56  // LALT
	BtnA      = 57  // SPACE
	BtnSelect = 97  // RCTRL
	BtnUp     = 103 //
	BtnLeft   = 105
	BtnRight  = 106
	BtnDown   = 108
	BtnPower  = 116
)

// trimuiButtons maps the TRIMUI Player1 gamepad's codes to Flashback buttons.
// TrimUI reports the Nintendo layout with the BTN_SOUTH… codes shifted
// (A = BTN_EAST, B = BTN_SOUTH, X = BTN_WEST, Y = BTN_NORTH), matching
// NextUI's JOY_A=1, JOY_B=0, JOY_X=3, JOY_Y=2 for tg5040.
var trimuiButtons = map[int]int{
	305: BtnA,
	304: BtnB,
	308: BtnX,
	307: BtnY,
	310: BtnL1,
	311: BtnR1,
	314: BtnSelect,
	315: BtnStart,
	316: BtnMenu,
}

const (
	evKey    = 1
	evAbs    = 3
	absHat0X = 16
	absHat0Y = 17
)

func repeatable(b int) bool {
	return b == BtnUp || b == BtnDown || b == BtnLeft || b == BtnRight || b == BtnL1 || b == BtnR1
}

// ReadInput streams button presses from the platform's input device.
func ReadInput(pl Platform, out chan<- int) error {
	f, err := os.Open(pl.Input)
	if err != nil {
		return err
	}
	events := make(chan [3]int, 64) // type, code, value
	go func() {
		defer f.Close()
		defer close(events)
		ev := 16
		if unsafe.Sizeof(uintptr(0)) == 8 {
			ev = 24
		}
		buf := make([]byte, ev)
		for {
			if _, err := io.ReadFull(f, buf); err != nil {
				return
			}
			o := ev - 8
			events <- [3]int{
				int(binary.LittleEndian.Uint16(buf[o:])),
				int(binary.LittleEndian.Uint16(buf[o+2:])),
				int(int32(binary.LittleEndian.Uint32(buf[o+4:]))),
			}
		}
	}()
	if pl.Pad {
		go padButtons(events, out, 350*time.Millisecond, 80*time.Millisecond)
	} else {
		go keyButtons(events, out)
	}
	return nil
}

func send(out chan<- int, b int) {
	select {
	case out <- b:
	default:
	}
}

// keyButtons handles keyboard-style input: presses, plus the kernel's
// auto-repeat for the d-pad and shoulders.
func keyButtons(events <-chan [3]int, out chan<- int) {
	for e := range events {
		if e[0] == evKey && (e[2] == 1 || e[2] == 2 && repeatable(e[1])) {
			send(out, e[1])
		}
	}
}

// padButtons handles a gamepad: buttons mapped through trimuiButtons, the
// d-pad from the hat axes, and auto-repeat done here.
func padButtons(events <-chan [3]int, out chan<- int, delay, rate time.Duration) {
	held := 0 // button being repeated, 0 = none
	var next <-chan time.Time
	hat := map[int]int{} // axis → direction button pressed on it
	press := func(b int) {
		send(out, b)
		if repeatable(b) {
			held, next = b, time.After(delay)
		}
	}
	release := func(b int) {
		if b != 0 && held == b {
			held, next = 0, nil
		}
	}
	for {
		select {
		case e, ok := <-events:
			if !ok {
				return
			}
			switch e[0] {
			case evKey:
				b := trimuiButtons[e[1]]
				switch {
				case b == 0:
				case e[2] == 1:
					press(b)
				case e[2] == 0:
					release(b)
				}
			case evAbs:
				if e[1] != absHat0X && e[1] != absHat0Y {
					continue
				}
				b := 0
				switch {
				case e[2] < 0 && e[1] == absHat0X:
					b = BtnLeft
				case e[2] > 0 && e[1] == absHat0X:
					b = BtnRight
				case e[2] < 0:
					b = BtnUp
				case e[2] > 0:
					b = BtnDown
				}
				if b == hat[e[1]] {
					continue
				}
				release(hat[e[1]])
				hat[e[1]] = b
				if b != 0 {
					press(b)
				}
			}
		case <-next:
			send(out, held)
			next = time.After(rate)
		}
	}
}
