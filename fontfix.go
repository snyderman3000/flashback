package main

import (
	"encoding/binary"
	"errors"
	"sort"
)

// repackSFNT copies one font out of a TrueType file or collection into a fresh
// single-font file with every table on a 4-byte boundary. Some fonts in the
// wild — including the WenQuanYi CJK font OnionOS ships — have unaligned
// tables, which golang.org/x/image/font/sfnt refuses to parse.
func repackSFNT(b []byte, index int) ([]byte, error) {
	be := binary.BigEndian
	if len(b) < 12 {
		return nil, errors.New("font too short")
	}
	start := 0
	if string(b[:4]) == "ttcf" {
		n := int(be.Uint32(b[8:]))
		if index >= n || 12+4*n > len(b) {
			return nil, errors.New("font index out of range")
		}
		start = int(be.Uint32(b[12+4*index:]))
	}
	if start+12 > len(b) {
		return nil, errors.New("bad font offset")
	}
	numTables := int(be.Uint16(b[start+4:]))
	if start+12+16*numTables > len(b) {
		return nil, errors.New("bad table directory")
	}
	type rec struct {
		tag      [4]byte
		sum      uint32
		off, len int
	}
	recs := make([]rec, numTables)
	for i := range recs {
		r := b[start+12+16*i:]
		copy(recs[i].tag[:], r[:4])
		recs[i].sum = be.Uint32(r[4:])
		recs[i].off = int(be.Uint32(r[8:]))
		recs[i].len = int(be.Uint32(r[12:]))
		if recs[i].off+recs[i].len > len(b) || recs[i].off < 0 {
			return nil, errors.New("table runs past end of file")
		}
	}
	sort.Slice(recs, func(i, j int) bool { return string(recs[i].tag[:]) < string(recs[j].tag[:]) })
	hdr := 12 + 16*numTables
	size := hdr
	for _, r := range recs {
		size += (r.len + 3) &^ 3
	}
	out := make([]byte, size)
	copy(out[:4], b[start:start+4]) // sfnt version
	be.PutUint16(out[4:], uint16(numTables))
	copy(out[6:12], b[start+6:start+12])
	pos := hdr
	for i, r := range recs {
		d := out[12+16*i:]
		copy(d[:4], r.tag[:])
		be.PutUint32(d[4:], r.sum)
		be.PutUint32(d[8:], uint32(pos))
		be.PutUint32(d[12:], uint32(r.len))
		copy(out[pos:], b[r.off:r.off+r.len])
		pos += (r.len + 3) &^ 3
	}
	return out, nil
}
