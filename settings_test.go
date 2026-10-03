package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSettingsScreen changes every kind of setting with the d-pad and checks
// they're saved to config.json and put to work.
func TestSettingsScreen(t *testing.T) {
	saved := sched
	defer func() { sched = saved }()
	u, env := newTestUI(t, "new.apkg")
	c := NewCanvas(640, 480)
	u.Draw(c)
	press(u, c, BtnStart)
	if u.scr != scrSettings {
		t.Fatalf("START didn't open settings: scr=%v", u.scr)
	}
	press(u, c,
		BtnRight,       // new per day 20 -> 25
		BtnDown, BtnR1, // reviews 200 -> 500 (big step)
		BtnDown, BtnLeft, // target recall 90% -> 89%
		BtnDown, BtnLeft, // longest interval: no limit -> 10 years
		BtnDown, BtnRight, // new order: deck -> random
		BtnDown, BtnRight, // text size 22 -> 26
		BtnDown, BtnRight, // sounds: auto -> only with START
		BtnUp, BtnUp, BtnUp, // wraps around fine
	)
	cfg := env.Config
	if cfg.NewPerDay != 25 || cfg.ReviewsPerDay != 500 || cfg.DesiredRetention != 0.89 || cfg.MaxInterval != 3650 ||
		cfg.NewOrder != "random" || cfg.TextSize != 26 || cfg.AutoplayAudio {
		t.Fatalf("settings not changed as expected: %+v", cfg)
	}
	press(u, c, BtnB)
	if u.scr != scrLibrary {
		t.Fatalf("B didn't go back: scr=%v", u.scr)
	}
	b, err := os.ReadFile(filepath.Join(env.DataDir, "config.json"))
	if err != nil || !strings.Contains(string(b), `"desired_retention": 0.89`) || !strings.Contains(string(b), `"new_order": "random"`) {
		t.Fatalf("config.json not saved: %s %v", b, err)
	}
	if sched.retention != 0.89 || sched.maxIvl != 3650 || !sched.randomNew {
		t.Fatalf("scheduler not updated: %+v", sched)
	}
	if got := loadConfig(filepath.Join(env.DataDir, "config.json")); got.NewPerDay != 25 || got.MaxInterval != 3650 {
		t.Fatalf("reloaded config %+v", got)
	}
}
