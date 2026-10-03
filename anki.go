package main

// Reading Anki packages (.apkg / .colpkg).
//
// A package is a zip holding one SQLite collection plus numbered media files.
// Three generations exist in the wild:
//   collection.anki2   schema 11, plain SQLite (old Anki, genanki, most scripts)
//   collection.anki21  schema 11, plain SQLite (Anki 2.1 "support older versions" export)
//   collection.anki21b schema 18, zstd-compressed SQLite (Anki 2.1.50+), media zstd too
// Newer packages also carry a stub collection.anki2 that only says "please
// update Anki", so the newest collection present always wins.
//
// Opening a package converts it once into a compact cache (index.json + notes.bin)
// so later launches are instant and big decks don't have to live in RAM.

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

const cacheFormat = 3 // 3: note type CSS

type Template struct {
	Name string `json:"name"`
	Q    string `json:"q"`
	A    string `json:"a"`
}

type Notetype struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Cloze     bool       `json:"cloze"`
	Fields    []string   `json:"fields"`
	Templates []Template `json:"templates"`
	CSS       string     `json:"css,omitempty"`
}

type DeckInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"` // "Parent::Child"
}

type NoteRef struct {
	ID   int64  `json:"id"`
	Mid  int64  `json:"mid"`
	Off  int64  `json:"off"`
	Len  int    `json:"len"`
	Tags string `json:"tags,omitempty"`
}

type CardRef struct {
	ID   int64 `json:"id"`
	Note int   `json:"n"` // index into Notes
	Deck int64 `json:"d"`
	Ord  int   `json:"o"`
}

type MediaRef struct {
	Zip  string `json:"z"`            // entry name inside the package
	Zstd bool   `json:"zs,omitempty"` // stored zstd-compressed (new packages)
}

// Package is an opened, cached Anki package.
type Package struct {
	Format    int                 `json:"format"`
	Key       string              `json:"key"`
	Source    string              `json:"source"`
	Title     string              `json:"title"`
	Schema    string              `json:"schema"`
	Notetypes map[int64]*Notetype `json:"notetypes"`
	Decks     []DeckInfo          `json:"decks"`
	Notes     []NoteRef           `json:"notes"`
	Cards     []CardRef           `json:"cards"`
	Media     map[string]MediaRef `json:"media"`

	dir   string
	mu    sync.Mutex
	notes *os.File
	zr    *zip.ReadCloser
}

// packageKey identifies a package file by path, size and modification time.
func packageKey(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%d", path, st.Size(), st.ModTime().UnixNano(), cacheFormat)))
	return hex.EncodeToString(h[:8]), nil
}

func cacheDirFor(cacheRoot, path string) string {
	h := sha1.Sum([]byte(path))
	return filepath.Join(cacheRoot, hex.EncodeToString(h[:6]))
}

// OpenPackage returns the cached package, converting it first if needed.
// progress (may be nil) receives short status lines during conversion.
func OpenPackage(path, cacheRoot string, progress func(string)) (*Package, error) {
	key, err := packageKey(path)
	if err != nil {
		return nil, err
	}
	dir := cacheDirFor(cacheRoot, path)
	if p, err := loadCached(dir, key); err == nil {
		p.Source = path
		return p, nil
	}
	if progress == nil {
		progress = func(string) {}
	}
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p, err := convertPackage(path, dir, key, progress)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return p, nil
}

func loadCached(dir, key string) (*Package, error) {
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return nil, err
	}
	var p Package
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.Format != cacheFormat || p.Key != key {
		return nil, errors.New("stale cache")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.bin")); err != nil {
		return nil, err
	}
	p.dir = dir
	return &p, nil
}

func convertPackage(path, dir, key string, progress func(string)) (*Package, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("not a valid Anki package (zip): %w", err)
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	var colFile *zip.File
	schema := ""
	for _, n := range []string{"collection.anki21b", "collection.anki21", "collection.anki2"} {
		if f := files[n]; f != nil {
			colFile, schema = f, n
			break
		}
	}
	if colFile == nil {
		return nil, errors.New("no Anki collection inside this file")
	}
	progress("UNPACKING " + strings.ToUpper(strings.TrimPrefix(schema, "collection.")))

	tmp := filepath.Join(dir, "collection.tmp")
	defer os.Remove(tmp)
	if err := extractCollection(colFile, tmp, schema == "collection.anki21b"); err != nil {
		return nil, err
	}
	f, err := os.Open(tmp)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, _ := f.Stat()
	db, err := openSQLite(f, st.Size())
	if err != nil {
		return nil, err
	}

	p := &Package{Format: cacheFormat, Key: key, Source: path, Schema: schema,
		Notetypes: map[int64]*Notetype{}, Media: map[string]MediaRef{}, dir: dir}
	progress("READING NOTE TYPES")
	if db.HasTable("notetypes") {
		err = readNotetypes18(db, p)
		if err == nil {
			err = readDecks18(db, p)
		}
	} else {
		err = readCol11(db, p)
	}
	if err != nil {
		return nil, err
	}

	progress("READING NOTES")
	nf, err := os.Create(filepath.Join(dir, "notes.bin"))
	if err != nil {
		return nil, err
	}
	noteIdx := map[int64]int{}
	var off int64
	// notes: id, guid, mid, mod, usn, tags, flds, ...
	err = db.Scan("notes", func(r Row) error {
		id := r.Rowid
		if v := r.Int(0); v != 0 {
			id = v
		}
		flds := r.Str(6)
		n, err := nf.WriteString(flds)
		if err != nil {
			return err
		}
		noteIdx[id] = len(p.Notes)
		p.Notes = append(p.Notes, NoteRef{ID: id, Mid: r.Int(2), Off: off, Len: n, Tags: strings.TrimSpace(r.Str(5))})
		off += int64(n)
		if len(p.Notes)%2000 == 0 {
			progress(fmt.Sprintf("READING NOTES %d", len(p.Notes)))
		}
		return nil
	})
	if cerr := nf.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("reading notes: %w", err)
	}

	progress("READING CARDS")
	// cards: id, nid, did, ord, mod, usn, type, queue, due, ivl, factor, reps, lapses, left, odue, odid, ...
	err = db.Scan("cards", func(r Row) error {
		id := r.Rowid
		if v := r.Int(0); v != 0 {
			id = v
		}
		ni, ok := noteIdx[r.Int(1)]
		if !ok {
			return nil
		}
		did := r.Int(2)
		if od := r.Int(15); od != 0 {
			did = od // card sits in a filtered deck; use its home deck
		}
		p.Cards = append(p.Cards, CardRef{ID: id, Note: ni, Deck: did, Ord: int(r.Int(3))})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading cards: %w", err)
	}
	if len(p.Cards) == 0 {
		return nil, errors.New("this package has no cards")
	}
	// cards in creation order, the order Anki introduces new cards (by note, then template)
	sort.SliceStable(p.Cards, func(i, j int) bool {
		a, b := p.Cards[i], p.Cards[j]
		if a.Note != b.Note {
			return p.Notes[a.Note].ID < p.Notes[b.Note].ID
		}
		return a.Ord < b.Ord
	})
	p.pruneDecks()

	progress("INDEXING MEDIA")
	if err := readMediaMap(files, p); err != nil {
		// a broken media map shouldn't stop the cards from working
		p.Media = map[string]MediaRef{}
	}
	p.Title = p.guessTitle(path)

	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), b, 0o644); err != nil {
		return nil, err
	}
	return p, nil
}

func extractCollection(f *zip.File, dst string, compressed bool) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	var src io.Reader = rc
	if compressed {
		zd, err := zstd.NewReader(rc, zstd.WithDecoderLowmem(true), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(64<<20))
		if err != nil {
			out.Close()
			return err
		}
		defer zd.Close()
		src = zd
	}
	_, err = io.Copy(out, src)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("unpacking collection: %w", err)
	}
	return nil
}

// ---- schema 11: everything lives in JSON columns of the single col row ----

func readCol11(db *sqliteDB, p *Package) error {
	var models, decks string
	err := db.Scan("col", func(r Row) error {
		models, decks = r.Str(9), r.Str(10)
		return nil
	})
	if err != nil {
		return err
	}
	var ms map[string]struct {
		Name string `json:"name"`
		Type int    `json:"type"`
		Flds []struct {
			Name string `json:"name"`
			Ord  int    `json:"ord"`
		} `json:"flds"`
		Tmpls []struct {
			Name string `json:"name"`
			Ord  int    `json:"ord"`
			Q    string `json:"qfmt"`
			A    string `json:"afmt"`
		} `json:"tmpls"`
		CSS string `json:"css"`
	}
	if err := json.Unmarshal([]byte(models), &ms); err != nil {
		return fmt.Errorf("note types: %w", err)
	}
	for k, m := range ms {
		id, _ := strconv.ParseInt(k, 10, 64)
		nt := &Notetype{ID: id, Name: m.Name, Cloze: m.Type == 1, CSS: m.CSS}
		sort.SliceStable(m.Flds, func(i, j int) bool { return m.Flds[i].Ord < m.Flds[j].Ord })
		for _, f := range m.Flds {
			nt.Fields = append(nt.Fields, f.Name)
		}
		sort.SliceStable(m.Tmpls, func(i, j int) bool { return m.Tmpls[i].Ord < m.Tmpls[j].Ord })
		for _, t := range m.Tmpls {
			nt.Templates = append(nt.Templates, Template{Name: t.Name, Q: t.Q, A: t.A})
		}
		p.Notetypes[id] = nt
	}
	var ds map[string]struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(decks), &ds); err != nil {
		return fmt.Errorf("decks: %w", err)
	}
	for k, d := range ds {
		id, _ := strconv.ParseInt(k, 10, 64)
		p.Decks = append(p.Decks, DeckInfo{ID: id, Name: d.Name})
	}
	return nil
}

// ---- schema 18: separate tables with protobuf config blobs ----

func readNotetypes18(db *sqliteDB, p *Package) error {
	// notetypes: id, name, mtime, usn, config(Notetype.Config: kind = 1, css = 3)
	err := db.Scan("notetypes", func(r Row) error {
		id := r.Rowid
		if v := r.Int(0); v != 0 {
			id = v
		}
		nt := &Notetype{ID: id, Name: r.Str(1)}
		pbEach(r.Blob(4), func(field int, v uint64, b []byte) {
			switch {
			case field == 1 && v == 1:
				nt.Cloze = true
			case field == 3:
				nt.CSS = string(b)
			}
		})
		p.Notetypes[id] = nt
		return nil
	})
	if err != nil {
		return err
	}
	// fields: ntid, ord, name, config  (WITHOUT ROWID, so key order = ntid, ord)
	err = db.Scan("fields", func(r Row) error {
		if nt := p.Notetypes[r.Int(0)]; nt != nil {
			ord := int(r.Int(1))
			for len(nt.Fields) <= ord {
				nt.Fields = append(nt.Fields, "")
			}
			nt.Fields[ord] = r.Str(2)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// templates: ntid, ord, name, mtime, usn, config(Template.Config: q_format = 1, a_format = 2)
	return db.Scan("templates", func(r Row) error {
		nt := p.Notetypes[r.Int(0)]
		if nt == nil {
			return nil
		}
		t := Template{Name: r.Str(2)}
		pbEach(r.Blob(5), func(field int, _ uint64, b []byte) {
			switch field {
			case 1:
				t.Q = string(b)
			case 2:
				t.A = string(b)
			}
		})
		ord := int(r.Int(1))
		for len(nt.Templates) <= ord {
			nt.Templates = append(nt.Templates, Template{})
		}
		nt.Templates[ord] = t
		return nil
	})
}

func readDecks18(db *sqliteDB, p *Package) error {
	p.Decks = p.Decks[:0]
	return db.Scan("decks", func(r Row) error {
		id := r.Rowid
		if v := r.Int(0); v != 0 {
			id = v
		}
		p.Decks = append(p.Decks, DeckInfo{ID: id, Name: strings.ReplaceAll(r.Str(1), "\x1f", "::")})
		return nil
	})
}

// pbEach walks the top-level fields of a protobuf message. Varints come in v,
// length-delimited fields in b; fixed-width fields are skipped.
func pbEach(msg []byte, fn func(field int, v uint64, b []byte)) {
	for len(msg) > 0 {
		key, n := binary.Uvarint(msg)
		if n <= 0 {
			return
		}
		msg = msg[n:]
		field, wt := int(key>>3), key&7
		switch wt {
		case 0:
			v, n := binary.Uvarint(msg)
			if n <= 0 {
				return
			}
			msg = msg[n:]
			fn(field, v, nil)
		case 1:
			if len(msg) < 8 {
				return
			}
			msg = msg[8:]
		case 2:
			l, n := binary.Uvarint(msg)
			if n <= 0 || uint64(len(msg)-n) < l {
				return
			}
			fn(field, 0, msg[n:n+int(l)])
			msg = msg[n+int(l):]
		case 5:
			if len(msg) < 4 {
				return
			}
			msg = msg[4:]
		default:
			return
		}
	}
}

// ---- media ----

func readMediaMap(files map[string]*zip.File, p *Package) error {
	mf := files["media"]
	if mf == nil {
		return nil
	}
	rc, err := mf.Open()
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	rc.Close()
	if err != nil {
		return err
	}
	trim := bytes.TrimSpace(raw)
	if len(trim) > 0 && trim[0] == '{' {
		var m map[string]string
		if err := json.Unmarshal(trim, &m); err != nil {
			return err
		}
		for zipName, name := range m {
			p.Media[name] = MediaRef{Zip: zipName}
		}
		return nil
	}
	// new packages: zstd-compressed protobuf MediaEntries { repeated MediaEntry entries = 1 }
	// MediaEntry { string name = 1; uint32 size = 2; bytes sha1 = 3; optional uint32 legacy_zip_filename = 255 }
	zd, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return err
	}
	defer zd.Close()
	dec, err := zd.DecodeAll(raw, nil)
	if err != nil {
		return err
	}
	i := 0
	pbEach(dec, func(field int, _ uint64, b []byte) {
		if field != 1 {
			return
		}
		name, zipName := "", strconv.Itoa(i)
		pbEach(b, func(f int, v uint64, bb []byte) {
			switch f {
			case 1:
				name = string(bb)
			case 255:
				zipName = strconv.FormatUint(v, 10)
			}
		})
		if name != "" {
			p.Media[name] = MediaRef{Zip: zipName, Zstd: true}
		}
		i++
	})
	return nil
}

// ReadMedia returns the bytes of a media file referenced by a card.
func (p *Package) ReadMedia(name string) ([]byte, error) {
	ref, ok := p.Media[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.zr == nil {
		zr, err := zip.OpenReader(p.Source)
		if err != nil {
			return nil, err
		}
		p.zr = zr
	}
	for _, f := range p.zr.File {
		if f.Name != ref.Zip {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		var r io.Reader = rc
		if ref.Zstd {
			zd, err := zstd.NewReader(rc, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true))
			if err != nil {
				return nil, err
			}
			defer zd.Close()
			r = zd
		}
		return io.ReadAll(io.LimitReader(r, 32<<20))
	}
	return nil, os.ErrNotExist
}

// Fields reads one note's fields from the cache.
func (p *Package) Fields(noteIdx int) []string {
	if noteIdx < 0 || noteIdx >= len(p.Notes) {
		return nil
	}
	n := p.Notes[noteIdx]
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.notes == nil {
		f, err := os.Open(filepath.Join(p.dir, "notes.bin"))
		if err != nil {
			return nil
		}
		p.notes = f
	}
	buf := make([]byte, n.Len)
	if _, err := p.notes.ReadAt(buf, n.Off); err != nil && err != io.EOF {
		return nil
	}
	return strings.Split(string(buf), "\x1f")
}

func (p *Package) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.notes != nil {
		p.notes.Close()
		p.notes = nil
	}
	if p.zr != nil {
		p.zr.Close()
		p.zr = nil
	}
}

// pruneDecks drops decks with no cards (and no subdecks with cards), e.g. "Default".
func (p *Package) pruneDecks() {
	used := map[string]bool{}
	byID := map[int64]string{}
	for _, d := range p.Decks {
		byID[d.ID] = d.Name
	}
	for _, c := range p.Cards {
		name, ok := byID[c.Deck]
		if !ok {
			continue
		}
		parts := strings.Split(name, "::")
		for i := range parts {
			used[strings.Join(parts[:i+1], "::")] = true
		}
	}
	// cards whose deck vanished go into a catch-all
	var orphan bool
	for _, c := range p.Cards {
		if _, ok := byID[c.Deck]; !ok {
			orphan = true
			break
		}
	}
	out := p.Decks[:0]
	for _, d := range p.Decks {
		if used[d.Name] {
			out = append(out, d)
		}
	}
	// make sure every ancestor exists as a deck entry
	have := map[string]bool{}
	for _, d := range out {
		have[d.Name] = true
	}
	for name := range used {
		if !have[name] {
			out = append(out, DeckInfo{ID: -int64(len(out) + 1), Name: name})
		}
	}
	if orphan {
		out = append(out, DeckInfo{ID: 0, Name: "Unsorted"})
		for i := range p.Cards {
			if _, ok := byID[p.Cards[i].Deck]; !ok {
				p.Cards[i].Deck = 0
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return deckLess(out[i].Name, out[j].Name) })
	p.Decks = out
}

func deckLess(a, b string) bool {
	pa, pb := strings.Split(a, "::"), strings.Split(b, "::")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if x, y := strings.ToLower(pa[i]), strings.ToLower(pb[i]); x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

func (p *Package) guessTitle(path string) string {
	// a single top-level deck names the package; otherwise use the file name
	tops := map[string]bool{}
	for _, d := range p.Decks {
		tops[strings.SplitN(d.Name, "::", 2)[0]] = true
	}
	if len(tops) == 1 {
		for t := range tops {
			return t
		}
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// DeckIDsUnder returns the ids of the named deck and all of its subdecks.
func (p *Package) DeckIDsUnder(name string) map[int64]bool {
	ids := map[int64]bool{}
	for _, d := range p.Decks {
		if name == "" || d.Name == name || strings.HasPrefix(d.Name, name+"::") {
			ids[d.ID] = true
		}
	}
	return ids
}

// CardsIn lists card indexes in the named deck tree ("" = all), in creation order.
func (p *Package) CardsIn(name string) []int {
	ids := p.DeckIDsUnder(name)
	var out []int
	for i, c := range p.Cards {
		if name == "" || ids[c.Deck] {
			out = append(out, i)
		}
	}
	return out
}
