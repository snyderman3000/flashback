package main

import (
	"testing"
	"time"
)

// collect gathers the buttons sent until the channel stays quiet for a while.
func collect(out <-chan int, quiet time.Duration) []int {
	var got []int
	for {
		select {
		case b := <-out:
			got = append(got, b)
		case <-time.After(quiet):
			return got
		}
	}
}

func TestPadButtons(t *testing.T) {
	events := make(chan [3]int, 64)
	out := make(chan int, 64)
	go padButtons(events, out, 40*time.Millisecond, 10*time.Millisecond)

	// buttons: A (BTN_EAST) and Y (BTN_NORTH) press once each; releases and unknown codes send nothing
	for _, e := range [][3]int{{evKey, 305, 1}, {evKey, 305, 0}, {evKey, 307, 1}, {evKey, 307, 0}, {evKey, 172, 1}, {0, 0, 0}} {
		events <- e
	}
	if got := collect(out, 30*time.Millisecond); len(got) != 2 || got[0] != BtnA || got[1] != BtnY {
		t.Fatalf("buttons: got %v", got)
	}

	// d-pad down held on the hat: one press, then repeats after the delay
	events <- [3]int{evAbs, absHat0Y, 1}
	time.Sleep(85 * time.Millisecond)
	events <- [3]int{evAbs, absHat0Y, 0}
	got := collect(out, 40*time.Millisecond)
	if len(got) < 3 {
		t.Fatalf("hat repeat: got %v", got)
	}
	for _, b := range got {
		if b != BtnDown {
			t.Fatalf("hat repeat: got %v", got)
		}
	}

	// released: no more repeats; left then right on the same axis switches direction
	events <- [3]int{evAbs, absHat0X, -1}
	events <- [3]int{evAbs, absHat0X, 1}
	events <- [3]int{evAbs, absHat0X, 0}
	if got := collect(out, 60*time.Millisecond); len(got) != 2 || got[0] != BtnLeft || got[1] != BtnRight {
		t.Fatalf("hat switch: got %v", got)
	}
	close(events)
}

func TestKeyButtons(t *testing.T) {
	events := make(chan [3]int, 16)
	out := make(chan int, 16)
	// Miyoo: presses, plus kernel repeats only for the d-pad and shoulders
	for _, e := range [][3]int{{evKey, BtnA, 1}, {evKey, BtnA, 2}, {evKey, BtnA, 0}, {evKey, BtnDown, 1}, {evKey, BtnDown, 2}, {3, 0, 1}} {
		events <- e
	}
	close(events)
	keyButtons(events, out)
	if got := collect(out, 10*time.Millisecond); len(got) != 3 || got[0] != BtnA || got[1] != BtnDown || got[2] != BtnDown {
		t.Fatalf("got %v", got)
	}
}
