package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Player plays card sounds with the command-line ffplay that OnionOS and
// NextUI already ship. Decoding runs in that separate native process, so
// sounds cost Flashback itself almost nothing.
type Player struct {
	bin string
	env []string
	tmp string

	mu   sync.Mutex
	gen  int
	proc *os.Process
}

// playerPaths lists where each system keeps its ffplay.
var playerPaths = map[string][]string{
	"miyoo":  {"/mnt/SDCARD/.tmp_update/bin/ffplay"},
	"tg5040": {"/mnt/SDCARD/.system/shared/bin/ffplay", "/mnt/SDCARD/.system/tg5040/bin/ffplay"},
}

// NewPlayer finds the system's player; it returns nil when there is none, and
// Flashback then just marks sounds on the card as before.
func NewPlayer(platform string) *Player {
	bin := os.Getenv("FLASHBACK_PLAYER")
	for _, p := range playerPaths[platform] {
		if bin != "" {
			break
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			bin = p
		}
	}
	if bin == "" {
		if p, err := exec.LookPath("ffplay"); err == nil {
			bin = p
		}
	}
	if bin == "" {
		log.Printf("audio: no ffplay found, sounds won't play")
		return nil
	}
	env := os.Environ()
	if platform == "miyoo" {
		env = miyooAudioEnv(env)
	}
	tmp := filepath.Join(os.TempDir(), "flashback-sound")
	os.RemoveAll(tmp)
	os.MkdirAll(tmp, 0o755)
	log.Printf("audio: playing sounds with %s", bin)
	return &Player{bin: bin, env: env, tmp: tmp}
}

// miyooAudioEnv makes sure ffplay finds OnionOS's libraries and, while
// OnionOS's audio server holds the sound device, goes through its shim.
func miyooAudioEnv(env []string) []string {
	get := func(k string) (int, string) {
		for i, kv := range env {
			if strings.HasPrefix(kv, k+"=") {
				return i, kv[len(k)+1:]
			}
		}
		return -1, ""
	}
	set := func(k, v string) {
		if i, _ := get(k); i >= 0 {
			env[i] = k + "=" + v
		} else {
			env = append(env, k+"="+v)
		}
	}
	_, lib := get("LD_LIBRARY_PATH")
	for _, d := range []string{"/mnt/SDCARD/.tmp_update/lib", "/mnt/SDCARD/miyoo/lib", "/config/lib", "/customer/lib", "/lib"} {
		if !strings.Contains(":"+lib+":", ":"+d+":") {
			lib = strings.TrimPrefix(lib+":"+d, ":")
		}
	}
	set("LD_LIBRARY_PATH", lib)
	const padsp = "/mnt/SDCARD/miyoo/lib/libpadsp.so"
	if _, pre := get("LD_PRELOAD"); !strings.Contains(pre, "libpadsp") && processRunning("audioserver") {
		if _, err := os.Stat(padsp); err == nil {
			set("LD_PRELOAD", strings.TrimPrefix(pre+" "+padsp, " "))
		}
	}
	return env
}

func processRunning(name string) bool {
	dirs, _ := os.ReadDir("/proc")
	for _, d := range dirs {
		if _, err := strconv.Atoi(d.Name()); err != nil {
			continue
		}
		if b, err := os.ReadFile("/proc/" + d.Name() + "/comm"); err == nil && strings.TrimSpace(string(b)) == name {
			return true
		}
	}
	return false
}

// Play stops whatever is playing and plays the named sounds one after another.
func (p *Player) Play(media MediaSource, names []string) {
	if p == nil {
		return
	}
	p.Stop()
	if len(names) == 0 || media == nil {
		return
	}
	p.mu.Lock()
	gen := p.gen
	p.mu.Unlock()
	go func() {
		for i, name := range names {
			if !p.current(gen) {
				return
			}
			data, err := media.ReadMedia(name)
			if err != nil {
				log.Printf("audio: %s: %v", name, err)
				continue
			}
			file := filepath.Join(p.tmp, strconv.Itoa(gen)+"-"+strconv.Itoa(i)+strings.ToLower(filepath.Ext(name)))
			if err := os.WriteFile(file, data, 0o644); err != nil {
				log.Printf("audio: %v", err)
				return
			}
			cmd := exec.Command(p.bin, "-nodisp", "-autoexit", "-loglevel", "error", file)
			cmd.Env = p.env
			cmd.Dir = filepath.Dir(p.bin)
			p.mu.Lock()
			if p.gen != gen {
				p.mu.Unlock()
				os.Remove(file)
				return
			}
			err = cmd.Start()
			if err == nil {
				p.proc = cmd.Process
			}
			p.mu.Unlock()
			if err != nil {
				log.Printf("audio: starting %s: %v", p.bin, err)
				os.Remove(file)
				return
			}
			cmd.Wait()
			os.Remove(file)
			p.mu.Lock()
			if p.proc == cmd.Process {
				p.proc = nil
			}
			p.mu.Unlock()
		}
	}()
}

func (p *Player) current(gen int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gen == gen
}

// Stop ends the current sound and anything queued after it.
func (p *Player) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.gen++
	proc := p.proc
	p.proc = nil
	p.mu.Unlock()
	if proc != nil {
		// ffplay closes the sound device cleanly on SIGTERM; make sure it goes
		proc.Signal(syscall.SIGTERM)
		time.AfterFunc(500*time.Millisecond, func() { proc.Kill() })
	}
}
