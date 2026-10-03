// Command lighten makes a copy of an Anki package (.apkg / .colpkg) whose
// pictures are no bigger than a handheld can show, so cards open faster on a
// slow CPU. Cards, notes, file names and IDs are untouched, so Flashback
// carries progress over when the copy replaces the original (same file name).
//
//	go run ./tools/lighten -in "Deck.apkg" -out "light/Deck.apkg"
//
// Pictures larger than the box (default 880x304, the biggest picture area
// Flashback has on a TrimUI Brick; the Miyoo Mini's is 544x188) are scaled
// down to fit. JPEGs stay JPEG and PNGs stay PNG, so transparency is kept.
// Progressive JPEGs, which decode about twice as slowly, are re-saved as
// baseline. GIF, WebP, BMP, SVG and sounds are copied as they are. A picture
// is only replaced when the new file is smaller (or no longer progressive).
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"
	xdraw "golang.org/x/image/draw"
)

func main() {
	in := flag.String("in", "", "input .apkg or .colpkg")
	out := flag.String("out", "", "output file (must differ from -in)")
	box := flag.String("box", "880x304", "largest picture size kept, WxH")
	quality := flag.Int("quality", 85, "JPEG quality for re-saved pictures")
	flag.Parse()
	var bw, bh int
	if _, err := fmt.Sscanf(*box, "%dx%d", &bw, &bh); err != nil || bw <= 0 || bh <= 0 || *in == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if a, _ := filepath.Abs(*in); a == mustAbs(*out) {
		log.Fatal("-out must be a different file from -in")
	}
	if err := lighten(*in, *out, bw, bh, *quality); err != nil {
		log.Fatal(err)
	}
}

func mustAbs(p string) string {
	a, _ := filepath.Abs(p)
	return a
}

// mediaEntry is one media file of the package: its zip entry and, for the
// newer zstd format, its record in the protobuf media list.
type mediaEntry struct {
	zip    string
	name   string
	record []byte // protobuf MediaEntry, newer format only
}

type result struct {
	data    []byte // nil: copy the original entry unchanged
	resized bool
}

func lighten(inPath, outPath string, bw, bh, quality int) error {
	zr, err := zip.OpenReader(inPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	media, zstdMedia, err := readMedia(files)
	if err != nil {
		return fmt.Errorf("media list: %w", err)
	}
	byZip := map[string]*mediaEntry{}
	for i := range media {
		byZip[media[i].zip] = &media[i]
	}

	// Re-encode pictures in parallel; the zip is written in the original order.
	results := make([]result, len(zr.File))
	var next, done, resized, rebased atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(zr.File) {
					return
				}
				f := zr.File[i]
				if byZip[f.Name] == nil {
					continue
				}
				raw, err := readEntry(f, zstdMedia)
				if err != nil {
					log.Printf("%s: %v (copied unchanged)", byZip[f.Name].name, err)
					continue
				}
				data, didResize, didRebase := shrink(raw, bw, bh, quality)
				if data != nil {
					results[i] = result{data: data, resized: didResize}
					if didResize {
						resized.Add(1)
					} else if didRebase {
						rebased.Add(1)
					}
				}
				if n := done.Add(1); n%2000 == 0 {
					log.Printf("%d / %d media files checked", n, len(media))
				}
			}
		}()
	}
	wg.Wait()

	tmp := outPath + ".part"
	of, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(of)
	var enc *zstd.Encoder
	if zstdMedia {
		enc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	}
	var before, after int64
	for i, f := range zr.File {
		me := byZip[f.Name]
		if me != nil {
			before += int64(f.UncompressedSize64)
		}
		switch {
		case f.Name == "media" && zstdMedia:
			if err := writeMediaList(zw, f, media, results, zr.File, enc); err != nil {
				return err
			}
		case me != nil && results[i].data != nil:
			data := results[i].data
			after += int64(len(data))
			if zstdMedia {
				data = enc.EncodeAll(data, nil)
			}
			h := f.FileHeader
			h.Method = zip.Store // pictures are already compressed
			h.CompressedSize64, h.UncompressedSize64, h.CRC32 = 0, 0, 0
			w, err := zw.CreateHeader(&h)
			if err != nil {
				return err
			}
			if _, err := w.Write(data); err != nil {
				return err
			}
		default:
			if me != nil {
				after += int64(f.UncompressedSize64)
			}
			if err := zw.Copy(f); err != nil {
				return err
			}
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := of.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, outPath); err != nil {
		return err
	}
	log.Printf("%d media files: %d pictures scaled down, %d re-saved as baseline JPEG", len(media), resized.Load(), rebased.Load())
	if !zstdMedia {
		log.Printf("media %.0f MB -> %.0f MB", float64(before)/1e6, float64(after)/1e6)
	}
	return nil
}

// readMedia returns the package's media files and whether they use the newer
// zstd format (protobuf list, each file zstd-compressed).
func readMedia(files map[string]*zip.File) ([]mediaEntry, bool, error) {
	mf := files["media"]
	if mf == nil {
		return nil, false, nil
	}
	rc, err := mf.Open()
	if err != nil {
		return nil, false, err
	}
	raw, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return nil, false, err
	}
	if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '{' {
		var m map[string]string
		if err := json.Unmarshal(t, &m); err != nil {
			return nil, false, err
		}
		var out []mediaEntry
		for z, n := range m {
			out = append(out, mediaEntry{zip: z, name: n})
		}
		return out, false, nil
	}
	zd, err := zstd.NewReader(nil)
	if err != nil {
		return nil, false, err
	}
	defer zd.Close()
	list, err := zd.DecodeAll(raw, nil)
	if err != nil {
		return nil, false, err
	}
	// MediaEntries { repeated MediaEntry entries = 1 }
	// MediaEntry { string name = 1; uint32 size = 2; bytes sha1 = 3; optional uint32 legacy_zip_filename = 255 }
	var out []mediaEntry
	err = eachField(list, func(field int, _ uint64, b []byte) {
		if field != 1 {
			return
		}
		e := mediaEntry{zip: strconv.Itoa(len(out)), record: b}
		eachField(b, func(f int, v uint64, bb []byte) {
			switch f {
			case 1:
				e.name = string(bb)
			case 255:
				e.zip = strconv.FormatUint(v, 10)
			}
		})
		out = append(out, e)
	})
	return out, true, err
}

func readEntry(f *zip.File, zstdMedia bool) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if !zstdMedia {
		return io.ReadAll(rc)
	}
	zd, err := zstd.NewReader(rc, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	defer zd.Close()
	return io.ReadAll(zd)
}

// shrink returns a smaller version of a JPEG or PNG picture, or nil to keep it.
func shrink(raw []byte, bw, bh, quality int) (data []byte, resized, rebased bool) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width == 0 || cfg.Height == 0 {
		return nil, false, false
	}
	progressive := format == "jpeg" && isProgressive(raw)
	scale := min(float64(bw)/float64(cfg.Width), float64(bh)/float64(cfg.Height))
	if scale >= 1 && !progressive {
		return nil, false, false
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, false, false
	}
	img := src
	if scale < 1 {
		w := max(1, int(float64(cfg.Width)*scale+0.5))
		h := max(1, int(float64(cfg.Height)*scale+0.5))
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
		img = dst
		resized = true
	}
	var buf bytes.Buffer
	if format == "jpeg" {
		q := quality
		if !resized {
			q = max(quality, 90) // only re-saving: keep generation loss low
		}
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: q})
	} else {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img)
	}
	if err != nil {
		return nil, false, false
	}
	if buf.Len() >= len(raw) && !progressive {
		return nil, false, false
	}
	return buf.Bytes(), resized, progressive && !resized
}

// isProgressive reports whether a JPEG's first frame header is progressive (SOF2).
func isProgressive(b []byte) bool {
	for i := 2; i+3 < len(b); {
		if b[i] != 0xFF {
			return false
		}
		switch m := b[i+1]; {
		case m == 0xC2:
			return true
		case m == 0xC0 || m == 0xC1 || m == 0xDA:
			return false
		case m == 0xD8 || (m >= 0xD0 && m <= 0xD7) || m == 0x01:
			i += 2
		default:
			i += 2 + int(binary.BigEndian.Uint16(b[i+2:]))
		}
	}
	return false
}

// writeMediaList rewrites the newer format's media list with the new sizes
// and SHA-1s of the pictures that were replaced.
func writeMediaList(zw *zip.Writer, f *zip.File, media []mediaEntry, results []result, all []*zip.File, enc *zstd.Encoder) error {
	newData := map[string][]byte{}
	for i, zf := range all {
		if results[i].data != nil {
			newData[zf.Name] = results[i].data
		}
	}
	var list []byte
	for _, e := range media {
		rec := e.record
		if d, ok := newData[e.zip]; ok {
			sum := sha1.Sum(d)
			var r []byte
			eachField(e.record, func(field int, v uint64, b []byte) {
				switch field {
				case 2, 3: // size and sha1 are rewritten below
				case 1:
					r = appendBytes(r, 1, b)
				case 255:
					r = appendVarint(r, 255, v)
				}
			})
			r = appendVarint(r, 2, uint64(len(d)))
			r = appendBytes(r, 3, sum[:])
			rec = r
		}
		list = appendBytes(list, 1, rec)
	}
	h := f.FileHeader
	h.CompressedSize64, h.UncompressedSize64, h.CRC32 = 0, 0, 0
	w, err := zw.CreateHeader(&h)
	if err != nil {
		return err
	}
	_, err = w.Write(enc.EncodeAll(list, nil))
	return err
}

// eachField walks a protobuf message's top-level fields.
func eachField(msg []byte, fn func(field int, v uint64, b []byte)) error {
	for len(msg) > 0 {
		key, n := binary.Uvarint(msg)
		if n <= 0 {
			return fmt.Errorf("bad protobuf key")
		}
		msg = msg[n:]
		field, wire := int(key>>3), key&7
		switch wire {
		case 0:
			v, n := binary.Uvarint(msg)
			if n <= 0 {
				return fmt.Errorf("bad varint")
			}
			fn(field, v, nil)
			msg = msg[n:]
		case 2:
			l, n := binary.Uvarint(msg)
			if n <= 0 || uint64(len(msg)-n) < l {
				return fmt.Errorf("bad length")
			}
			fn(field, 0, msg[n:n+int(l)])
			msg = msg[n+int(l):]
		case 1:
			msg = msg[min(8, len(msg)):]
		case 5:
			msg = msg[min(4, len(msg)):]
		default:
			return fmt.Errorf("unsupported wire type %d", wire)
		}
	}
	return nil
}

func appendVarint(b []byte, field int, v uint64) []byte {
	b = binary.AppendUvarint(b, uint64(field)<<3)
	return binary.AppendUvarint(b, v)
}

func appendBytes(b []byte, field int, v []byte) []byte {
	b = binary.AppendUvarint(b, uint64(field)<<3|2)
	b = binary.AppendUvarint(b, uint64(len(v)))
	return append(b, v...)
}
