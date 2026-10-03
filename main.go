package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"
)

const version = "0.2.0"

// Config lives in App/Flashback/data/config.json.
type Config struct {
	DeckFolders   []string `json:"deck_folders"`    // where to look for .apkg files
	NewPerDay     int      `json:"new_per_day"`     // new cards introduced per deck file per day
	ReviewsPerDay int      `json:"reviews_per_day"` // review cap per deck file per day
	TextSize      int      `json:"text_size"`       // card text size in pixels (16–34)

	Problem string `json:"-"`
}

func defaultConfig() Config {
	return Config{DeckFolders: []string{"/mnt/SDCARD/Anki"}, NewPerDay: 20, ReviewsPerDay: 200, TextSize: 22}
}

type Env struct {
	AppDir  string
	DataDir string
	Config  Config
	Now     func() time.Time
}

func newEnv() *Env {
	exe, _ := os.Executable()
	appDir := filepath.Dir(exe)
	env := &Env{AppDir: appDir, DataDir: filepath.Join(appDir, "data"), Now: time.Now}
	if d := os.Getenv("FLASHBACK_DATA"); d != "" {
		env.DataDir = d
	}
	os.MkdirAll(env.DataDir, 0o755)
	env.Config = loadConfig(filepath.Join(env.DataDir, "config.json"))
	return env
}

func loadConfig(path string) Config {
	cfg := defaultConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		b, _ := json.MarshalIndent(cfg, "", "  ")
		os.WriteFile(path, b, 0o644)
		return cfg
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		cfg = defaultConfig()
		cfg.Problem = fmt.Sprintf("config.json couldn't be read (%v). Using the defaults until it's fixed.", err)
	}
	d := defaultConfig()
	if len(cfg.DeckFolders) == 0 {
		cfg.DeckFolders = d.DeckFolders
	}
	if cfg.NewPerDay < 0 {
		cfg.NewPerDay = 0
	}
	if cfg.ReviewsPerDay <= 0 {
		cfg.ReviewsPerDay = d.ReviewsPerDay
	}
	if cfg.TextSize < 16 || cfg.TextSize > 34 {
		cfg.TextSize = d.TextSize
	}
	return cfg
}

func (env *Env) SaveConfig() {
	b, err := json.MarshalIndent(env.Config, "", "  ")
	if err == nil {
		os.WriteFile(filepath.Join(env.DataDir, "config.json"), b, 0o644)
	}
}

// FindPackages lists .apkg/.colpkg files in the deck folders (and one level of subfolders).
func (env *Env) FindPackages() []string {
	var out []string
	seen := map[string]bool{}
	add := func(dir string) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			p := filepath.Join(dir, e.Name())
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !e.IsDir() && (ext == ".apkg" || ext == ".colpkg") && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	for _, d := range env.Config.DeckFolders {
		os.MkdirAll(d, 0o755)
		add(d)
		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				add(filepath.Join(d, e.Name()))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(filepath.Base(out[i])) < strings.ToLower(filepath.Base(out[j]))
	})
	return out
}

// PruneCache removes cached conversions for packages that no longer exist.
func (env *Env) PruneCache(pkgs []string) {
	keep := map[string]bool{}
	root := filepath.Join(env.DataDir, "cache")
	for _, p := range pkgs {
		keep[cacheDirFor(root, p)] = true
	}
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		d := filepath.Join(root, e.Name())
		if !keep[d] {
			os.RemoveAll(d)
		}
	}
}

func main() {
	shot := flag.String("shot", "", "render a scene to PNG instead of running on the device (library|empty|decks|study|answer|cloze|occlusion|browse|view|done|unpack|bye)")
	out := flag.String("out", "shot.png", "PNG path for -shot")
	w := flag.Int("w", 640, "screen width for -shot")
	h := flag.Int("h", 480, "screen height for -shot")
	bench := flag.Int("bench", 0, "with -shot: render the scene N times and print the average frame time")
	icon := flag.String("icon", "", "write the 74×74 OnionOS app icon to this PNG path and exit")
	flag.Parse()
	if *icon != "" {
		f, err := os.Create(*icon)
		if err != nil {
			log.Fatal(err)
		}
		png.Encode(f, appIcon(74))
		f.Close()
		return
	}

	env := newEnv()
	loadCardFonts(env.AppDir)
	if *shot != "" {
		c := NewCanvas(*w, *h)
		ui := NewUI(env, c.W, c.H)
		demoScene(ui, *shot)
		ui.Draw(c)
		if *bench > 0 {
			t0 := time.Now()
			for i := 0; i < *bench; i++ {
				ui.frame++
				ui.Draw(c)
			}
			fmt.Printf("%s: %.1f ms/frame\n", *shot, float64(time.Since(t0).Microseconds())/1000/float64(*bench))
		}
		f, err := os.Create(*out)
		if err != nil {
			log.Fatal(err)
		}
		png.Encode(f, c)
		f.Close()
		return
	}

	// the Mini has 128 MB of RAM shared with OnionOS; collect garbage early
	debug.SetMemoryLimit(48 << 20)

	logPath := filepath.Join(env.DataDir, "flashback.log")
	if st, err := os.Stat(logPath); err == nil && st.Size() > 512<<10 {
		os.Rename(logPath, logPath+".1")
	}
	if logf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); logf != nil {
		log.SetOutput(logf)
	}
	pl, err := detectPlatform()
	if err != nil {
		log.Fatalf("platform: %v", err)
	}
	log.Printf("flashback %s starting on %s (input %s), folders=%v", version, pl.Name, pl.Input, env.Config.DeckFolders)

	fb, err := OpenFramebuffer(pl.Flip)
	if err != nil {
		log.Fatalf("framebuffer: %v", err)
	}
	defer fb.Close()
	log.Printf("screen %dx%d, %d bpp", fb.W, fb.H, fb.bpp)
	c := NewCanvas(fb.W, fb.H)
	keys := make(chan int, 32)
	if err := ReadInput(pl, keys); err != nil {
		log.Fatalf("input: %v", err)
	}
	// powering off (or the OS closing the app) sends a signal: save first
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	ui := NewUI(env, c.W, c.H)
	ui.Start()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	lastMinute := -1
	dirty := true
	for !ui.quit {
		select {
		case k := <-keys:
			ui.Key(k)
			dirty = true
		case f := <-ui.post:
			f()
			dirty = true
		case s := <-stop:
			log.Printf("%v: saving and exiting", s)
			ui.saveAll()
			return
		case <-tick.C:
			ui.frame++
			ui.Tick()
			if ui.TickNeedsDraw() {
				dirty = true
			}
			if m := time.Now().Minute(); m != lastMinute {
				lastMinute = m
				dirty = true
			}
		}
		if dirty {
			ui.Draw(c)
			fb.Present(c)
			dirty = false
		}
	}
	ui.saveAll()
	ui.scr = scrBye
	ui.byeStart = ui.frame
	for i := 0; i < 16; i++ {
		ui.frame++
		ui.Draw(c)
		fb.Present(c)
		time.Sleep(40 * time.Millisecond)
	}
	fb.Clear()
}
