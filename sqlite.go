package main

// A small read-only SQLite reader: enough to walk table and index b-trees and
// decode records, which is all an Anki collection needs. Pure Go, no cgo, so
// the binary stays a single static ARM executable.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf16"
)

type sqliteDB struct {
	r        io.ReaderAt
	pageSize int
	usable   int
	nPages   int
	enc      int // 1 utf-8, 2 utf-16le, 3 utf-16be
	cache    map[int][]byte
	order    []int
	tables   map[string]sqliteTable
}

type sqliteTable struct {
	Root    int
	SQL     string
	NoRowid bool
}

// Row is one decoded record. Values are nil, int64, float64, string or []byte.
type Row struct {
	Rowid int64
	Vals  []any
}

func (r Row) Int(i int) int64 {
	if i >= len(r.Vals) {
		return 0
	}
	switch v := r.Vals[i].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		var n int64
		fmt.Sscan(v, &n)
		return n
	}
	return 0
}

func (r Row) Str(i int) string {
	if i >= len(r.Vals) {
		return ""
	}
	switch v := r.Vals[i].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case int64:
		return fmt.Sprint(v)
	case float64:
		return fmt.Sprint(v)
	}
	return ""
}

func (r Row) Blob(i int) []byte {
	if i >= len(r.Vals) {
		return nil
	}
	switch v := r.Vals[i].(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	}
	return nil
}

var errNotSQLite = errors.New("not an SQLite database")

func openSQLite(r io.ReaderAt, size int64) (*sqliteDB, error) {
	hdr := make([]byte, 100)
	if _, err := r.ReadAt(hdr, 0); err != nil {
		return nil, errNotSQLite
	}
	if string(hdr[:16]) != "SQLite format 3\x00" {
		return nil, errNotSQLite
	}
	ps := int(binary.BigEndian.Uint16(hdr[16:]))
	if ps == 1 {
		ps = 65536
	}
	if ps < 512 || ps&(ps-1) != 0 {
		return nil, fmt.Errorf("bad page size %d", ps)
	}
	db := &sqliteDB{r: r, pageSize: ps, usable: ps - int(hdr[20]), cache: map[int][]byte{}, tables: map[string]sqliteTable{}}
	db.enc = int(binary.BigEndian.Uint32(hdr[56:]))
	if db.enc == 0 {
		db.enc = 1
	}
	db.nPages = int(size / int64(ps))
	// sqlite_master: type, name, tbl_name, rootpage, sql
	err := db.scanTable(1, func(row Row) error {
		if row.Str(0) == "table" {
			sql := row.Str(4)
			db.tables[strings.ToLower(row.Str(1))] = sqliteTable{
				Root:    int(row.Int(3)),
				SQL:     sql,
				NoRowid: strings.Contains(strings.ToLower(sql), "without rowid"),
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (db *sqliteDB) HasTable(name string) bool {
	_, ok := db.tables[strings.ToLower(name)]
	return ok
}

// Scan visits every row of a table in key order.
func (db *sqliteDB) Scan(table string, fn func(Row) error) error {
	t, ok := db.tables[strings.ToLower(table)]
	if !ok {
		return fmt.Errorf("no table %q", table)
	}
	if t.NoRowid {
		return db.scanIndex(t.Root, fn, 0)
	}
	return db.scanTable(t.Root, fn)
}

func (db *sqliteDB) page(n int) ([]byte, error) {
	if p, ok := db.cache[n]; ok {
		return p, nil
	}
	if n < 1 || (db.nPages > 0 && n > db.nPages) {
		return nil, fmt.Errorf("page %d out of range", n)
	}
	p := make([]byte, db.pageSize)
	if _, err := db.r.ReadAt(p, int64(n-1)*int64(db.pageSize)); err != nil && err != io.EOF {
		return nil, err
	}
	// keep a small cache: interior pages get revisited, leaves rarely
	if len(db.order) >= 64 {
		delete(db.cache, db.order[0])
		db.order = db.order[1:]
	}
	db.cache[n] = p
	db.order = append(db.order, n)
	return p, nil
}

func (db *sqliteDB) scanTable(root int, fn func(Row) error) error {
	return db.walk(root, fn, 0)
}

func (db *sqliteDB) walk(pg int, fn func(Row) error, depth int) error {
	if depth > 40 {
		return errors.New("b-tree too deep (corrupt?)")
	}
	p, err := db.page(pg)
	if err != nil {
		return err
	}
	h := 0
	if pg == 1 {
		h = 100
	}
	typ := p[h]
	n := int(binary.BigEndian.Uint16(p[h+3:]))
	switch typ {
	case 0x05: // interior table
		right := int(binary.BigEndian.Uint32(p[h+8:]))
		ptrs := cellPtrs(p, h+12, n)
		for _, off := range ptrs {
			child := int(binary.BigEndian.Uint32(p[off:]))
			if err := db.walk(child, fn, depth+1); err != nil {
				return err
			}
		}
		return db.walk(right, fn, depth+1)
	case 0x0d: // leaf table
		for _, off := range cellPtrs(p, h+8, n) {
			plen, k := uvarint(p[off:])
			off += k
			rowid, k := uvarint(p[off:])
			off += k
			payload, err := db.payload(p, off, int(plen), db.usable-35)
			if err != nil {
				return err
			}
			vals, err := db.record(payload)
			if err != nil {
				return err
			}
			if err := fn(Row{Rowid: int64(rowid), Vals: vals}); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unexpected page type %#x in table b-tree", typ)
}

func (db *sqliteDB) scanIndex(pg int, fn func(Row) error, depth int) error {
	if depth > 40 {
		return errors.New("b-tree too deep (corrupt?)")
	}
	p, err := db.page(pg)
	if err != nil {
		return err
	}
	h := 0
	if pg == 1 {
		h = 100
	}
	typ := p[h]
	n := int(binary.BigEndian.Uint16(p[h+3:]))
	maxLocal := (db.usable-12)*64/255 - 23
	emit := func(off int) error {
		plen, k := uvarint(p[off:])
		off += k
		payload, err := db.payload(p, off, int(plen), maxLocal)
		if err != nil {
			return err
		}
		vals, err := db.record(payload)
		if err != nil {
			return err
		}
		return fn(Row{Vals: vals})
	}
	switch typ {
	case 0x02: // interior index: left subtree, then the cell's own key
		right := int(binary.BigEndian.Uint32(p[h+8:]))
		for _, off := range cellPtrs(p, h+12, n) {
			child := int(binary.BigEndian.Uint32(p[off:]))
			if err := db.scanIndex(child, fn, depth+1); err != nil {
				return err
			}
			if err := emit(off + 4); err != nil {
				return err
			}
		}
		return db.scanIndex(right, fn, depth+1)
	case 0x0a:
		for _, off := range cellPtrs(p, h+8, n) {
			if err := emit(off); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unexpected page type %#x in index b-tree", typ)
}

func cellPtrs(p []byte, at, n int) []int {
	out := make([]int, 0, n)
	for i := 0; i < n && at+2*i+2 <= len(p); i++ {
		out = append(out, int(binary.BigEndian.Uint16(p[at+2*i:])))
	}
	return out
}

// payload gathers a cell's payload, following overflow pages if it spilled.
func (db *sqliteDB) payload(p []byte, off, plen, maxLocal int) ([]byte, error) {
	u := db.usable
	if plen <= maxLocal {
		if off+plen > len(p) {
			return nil, errors.New("cell runs past page")
		}
		return p[off : off+plen], nil
	}
	minLocal := (u-12)*32/255 - 23
	local := minLocal + (plen-minLocal)%(u-4)
	if local > maxLocal {
		local = minLocal
	}
	if off+local+4 > len(p) {
		return nil, errors.New("cell runs past page")
	}
	out := make([]byte, 0, plen)
	out = append(out, p[off:off+local]...)
	next := int(binary.BigEndian.Uint32(p[off+local:]))
	for len(out) < plen && next != 0 {
		op, err := db.page(next)
		if err != nil {
			return nil, err
		}
		take := plen - len(out)
		if take > u-4 {
			take = u - 4
		}
		out = append(out, op[4:4+take]...)
		next = int(binary.BigEndian.Uint32(op))
	}
	if len(out) < plen {
		return nil, errors.New("overflow chain ended early")
	}
	return out, nil
}

func (db *sqliteDB) record(b []byte) ([]any, error) {
	hlen, k := uvarint(b)
	if int(hlen) > len(b) || k == 0 {
		return nil, errors.New("bad record header")
	}
	var types []uint64
	for i := k; i < int(hlen); {
		t, n := uvarint(b[i:])
		if n == 0 {
			return nil, errors.New("bad serial type")
		}
		types = append(types, t)
		i += n
	}
	pos := int(hlen)
	vals := make([]any, len(types))
	for i, t := range types {
		var size int
		switch {
		case t == 0, t == 8, t == 9:
			size = 0
		case t <= 4:
			size = int(t)
		case t == 5:
			size = 6
		case t == 6, t == 7:
			size = 8
		case t >= 12:
			size = int(t-12) / 2
		default:
			return nil, fmt.Errorf("reserved serial type %d", t)
		}
		if pos+size > len(b) {
			return nil, errors.New("record runs past payload")
		}
		d := b[pos : pos+size]
		switch {
		case t == 0:
			vals[i] = nil
		case t == 8:
			vals[i] = int64(0)
		case t == 9:
			vals[i] = int64(1)
		case t <= 6:
			var v int64
			for _, c := range d {
				v = v<<8 | int64(c)
			}
			// sign-extend
			shift := uint(64 - 8*size)
			vals[i] = v << shift >> shift
		case t == 7:
			vals[i] = math.Float64frombits(binary.BigEndian.Uint64(d))
		case t%2 == 0:
			vals[i] = append([]byte(nil), d...)
		default:
			vals[i] = db.text(d)
		}
		pos += size
	}
	return vals, nil
}

func (db *sqliteDB) text(d []byte) string {
	if db.enc == 1 {
		return string(d)
	}
	u := make([]uint16, len(d)/2)
	for i := range u {
		if db.enc == 2 {
			u[i] = binary.LittleEndian.Uint16(d[2*i:])
		} else {
			u[i] = binary.BigEndian.Uint16(d[2*i:])
		}
	}
	return string(utf16.Decode(u))
}

// uvarint decodes SQLite's big-endian varint (1–9 bytes).
func uvarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < 9 && i < len(b); i++ {
		if i == 8 {
			return v<<8 | uint64(b[i]), 9
		}
		v = v<<7 | uint64(b[i]&0x7f)
		if b[i]&0x80 == 0 {
			return v, i + 1
		}
	}
	return v, 0
}
