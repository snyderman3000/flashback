# FLASHBACK

**Anki flashcards for OnionOS on the Miyoo Mini Plus, and NextUI / NX Redux on TrimUI handhelds (Brick, Brick Pro, Smart Pro).** Flashback opens Anki deck files (`.apkg`), the shared decks from AnkiWeb or ones you export from Anki, and lets you study them with spaced repetition, browse every card, or shuffle through a pile. Cards are typed onto ruled index cards. Works offline; no account needed.

> 🤖 **Built by Claude.** Flashback was designed and written by [Claude](https://claude.ai), Anthropic's AI model, at the request of [@snyderman3000](https://github.com/snyderman3000) after someone asked for an Anki reader in [Mixtape](https://github.com/snyderman3000/mixtape). Claude wrote the code, the documentation and the artwork. It shares its display and input code with [Mixtape](https://github.com/snyderman3000/mixtape) and [Rewind](https://github.com/snyderman3000/rewind). Please report bugs through [Issues](https://github.com/snyderman3000/flashback/issues).

| | |
|---|---|
| ![The card catalog](docs/library.png) | ![Decks in a file](docs/decks.png) |
| ![A flag card](docs/flag.png) | ![A map card, answer side](docs/map.png) |
| ![Cloze deletion](docs/cloze.png) | ![Image occlusion](docs/occlusion.png) |
| ![Japanese](docs/japanese.png) | ![Browsing a deck](docs/geo-browse.png) |

*Flag and map screenshots show the [Ultimate Geography](https://github.com/anki-geo/ultimate-geography) deck (CC BY 3.0). The other cards are test cards.*

## Features

- **Opens real Anki decks.** All three package generations: old `collection.anki2` (including decks made with genanki), `collection.anki21`, and the zstd-compressed `collection.anki21b` that current Anki exports. `.colpkg` collection backups work too.
- **Study with spaced repetition.** Anki-style scheduling: new cards go through 1- and 10-minute steps, then come back after days, weeks and months. **Again / Good / Easy**, with the next interval shown on each button. Sibling cards (the reverse of a card you just saw) wait until another day. Daily limits for new cards and reviews.
- **Browse** every card in a deck with question and answer previews, then flip through them one by one.
- **Shuffle** a deck and flip through it for quick practice, without touching your schedule.
- **Cards look like Anki's.** Note types and templates from the deck itself, conditionals, cloze deletions (with hints, nested clozes), type-in-the-answer cards, hints, furigana, bold/italic/colour, lists, simple tables, and pictures: PNG, JPEG, GIF, WebP, BMP and **SVG** (flags, diagrams). **Image occlusion** cards are drawn with their masks.
- **Every language.** Text is typed in Courier Prime; DejaVu Sans fills in Greek, Cyrillic, maths and symbols, and Japanese, Chinese and Korean use the CJK font OnionOS already has. Drop any other `.ttf`/`.otf` into `App/Flashback/fonts` to add a script.
- **Subdecks** as a tree, with new / learning / due counts for each.
- **Big decks are fine.** Each deck is unpacked once into a compact cache; after that it opens instantly and needs only a few MB of memory, even at 50,000 cards. Pictures are read straight from the `.apkg` when a card needs them, so nothing is copied twice.
- **Index-card look.** Text sits on the card's ruled lines under a red header rule, cards flip over when you show the answer, deck files are pulled out of a card-catalog drawer, and decks are listed on a ledger sheet.

## Install

1. Download `Flashback-vX.Y.Z-OnionOS.zip` from [Releases](https://github.com/snyderman3000/flashback/releases).
2. Extract it and copy the `App` folder to the root of your SD card, merging it with the `App` folder already there.
3. On a computer, download decks from [ankiweb.net/shared/decks](https://ankiweb.net/shared/decks), or export your own from Anki (**File → Export → Anki Deck Package**).
4. Copy the `.apkg` files into the `Anki` folder at the root of the SD card. Flashback creates the folder the first time it runs, and also looks one folder level down.
5. Open **Apps → Flashback**.

AnkiWeb only lets signed-in users download shared decks, so decks are fetched on a computer rather than on the Mini. With no decks yet, Flashback shows these steps and a QR code for the shared-deck page.

Requires OnionOS 4.2 or newer (Miyoo Mini Plus or Mini Flip). No Wi-Fi needed.

**NextUI / NX Redux (TrimUI Brick, Brick Pro, Smart Pro):** use `Flashback-vX.Y.Z-NextUI.zip` instead, copy its `Tools` folder to the root of the SD card, and open **Tools → Flashback**. The controls are the same; the d-pad and shoulder buttons repeat when held.

## Controls

| Screen | Buttons |
|---|---|
| Card catalog | ↑↓ choose a file · ←→ / **L1/R1** page · **A** open · **SELECT** rescan · **B** exit |
| Decks | ↑↓ choose · **A** study · **X** browse · **Y** shuffle · **B** back |
| Study | **A** show answer, then **Y** again · **A** good · **X** easy · ↑↓ scroll · **SELECT** text size · **B** back |
| Browse | ↑↓ move · ←→ / **L1/R1** page · **A** open · **B** back |
| Card | **A** flip (next card in shuffle) · ←→ previous/next · **Y** reshuffle · ↑↓ scroll · **SELECT** text size · **B** back |

MENU exits from anywhere. Progress is saved as you go.

## Settings

`App/Flashback/data/config.json` (created on first run):

| Key | Default | Meaning |
|---|---|---|
| `deck_folders` | `["/mnt/SDCARD/Anki"]` | Folders to look in for `.apkg` / `.colpkg` files |
| `new_per_day` | `20` | New cards introduced per deck file per day |
| `reviews_per_day` | `200` | Reviews per deck file per day |
| `text_size` | `22` | Card text size in pixels, 16–34 (**SELECT** on a card cycles through sizes) |

## Good to know

- Flashback keeps its own progress in `App/Flashback/data/progress`, per deck **file name**. Replace a deck with an updated export of the same name and your progress carries over. It doesn't sync with Anki or AnkiWeb, and scheduling exported with a deck isn't imported.
- `data/cache` holds unpacked decks. It's safe to delete; it's rebuilt automatically.
- Maths written for MathJax (`\( … \)`) is shown as readable text with symbols, superscripts and subscripts (`F = (q₁ q₂)/(4πε₀ r²)`), not typeset equations.
- Some note types build the card with JavaScript (for example "Cloze Overlapping"). Flashback can't run that, so it shows the note's cloze text (or its fields) instead.
- Parts of a card that the note type's CSS hides (`display: none`) stay hidden, as on AnkiMobile/AnkiDroid — e.g. the tag list and the second copy of the cloze on AnKing cards. Buttons are left out, since there's nothing to click them with.
- Not supported yet: sound and video (cards show ♪ instead), [latex] images, and the deck's own CSS beyond hiding, colours, sizes and bold/italic.

## Building

Needs Go 1.24 or newer.

```sh
./build.sh          # runs the tests, cross-compiles for ARMv7 (OnionOS) and arm64 (NextUI) and writes
                    # dist/Flashback-vX.Y.Z-OnionOS.zip and dist/Flashback-vX.Y.Z-NextUI.zip
go run . -shot study -out study.png   # render a screen on your computer (library, decks, study, answer, cloze,
                                      # occlusion, browse, view, japanese, flag, map, done, empty, bye)
```

`go test ./...` checks package reading (all three formats), templates and cloze, the scheduler, the SVG renderer and the whole UI driven by button presses. The small test decks in `testdata/` were made with Anki's own library by `testdata/gen/make_sampler.py`. `make_ug.py` and `make_big.py` build larger decks (a real shared deck, and 50,000 cards) for the extra tests, which are skipped when those files are absent.

How it works: a small read-only SQLite reader and protobuf decoder (`sqlite.go`, `anki.go`) read the collection without cgo, so the app is a single static binary. `template.go` renders Anki's card templates, `layout.go` lays out the resulting HTML on a ruled card, `svg.go` rasterizes SVG images, and `scheduler.go` is the spaced-repetition scheduler.

## License

MIT, see [LICENSE](LICENSE). Bundled fonts and libraries are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Flashback is an independent app and isn't affiliated with or endorsed by Anki or AnkiWeb.
