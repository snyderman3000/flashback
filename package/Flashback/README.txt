FLASHBACK — Anki flashcards for OnionOS (Miyoo Mini Plus)
==========================================================
Built by Claude, Anthropic's AI model, for snyderman3000.
Source, updates and bug reports: https://github.com/snyderman3000/flashback

Flashback opens Anki deck files (.apkg) — the shared decks from AnkiWeb
or ones you export from Anki yourself — and lets you study them with
spaced repetition, browse every card, or shuffle through a deck. Cards
are typed onto ruled index cards. Works offline; no account needed.

INSTALL
  1. Copy the "App" folder from this zip to the root of your SD card
     (merge it with the App folder that's already there).
  2. On a computer, download decks from https://ankiweb.net/shared/decks
     (or in Anki: File → Export → "Anki Deck Package").
  3. Copy the .apkg files into the "Anki" folder at the root of your SD
     card. (Flashback creates the folder the first time it runs.)
     Subfolders one level deep are fine.
  4. Open Apps → Flashback. The first time it sees a deck it unpacks it
     (a few seconds for big decks); after that it opens instantly.

CONTROLS
  CATALOG  Up/Down choose a deck file · A open · SELECT rescan · B exit
  DECKS    Up/Down choose a deck · A study · X browse · Y shuffle · B back
  STUDY    A show answer, then Y again · A good · X easy
           Up/Down scroll long cards · SELECT text size · B back (saves)
  BROWSE   Up/Down move · L1/R1 page · A open card · B back
  CARD     A flip · Left/Right previous/next · Y reshuffle (shuffle mode)
  MENU exits from anywhere. Progress is saved as you go.

HOW STUDYING WORKS
  Like Anki: new cards go through short steps (1 and 10 minutes), then
  come back after days, weeks and months. "Again" sends a card back to
  the start. Up to 20 new cards and 200 reviews per deck file per day.
  Progress is kept per file name in data/progress, so you can replace a
  deck with an updated version of the same file and keep your progress.
  (Flashback keeps its own progress; it doesn't sync with Anki or
  AnkiWeb, and your scheduling in Anki isn't imported.)

WHAT CARDS CAN SHOW
  Text with bold/italic/colours, lists, simple tables, cloze deletions,
  type-in-the-answer cards (the answer is shown), hints, furigana,
  pictures (PNG, JPEG, GIF, WebP, BMP and SVG) and image occlusion.
  Japanese, Chinese and Korean use the font OnionOS already has. To add
  another script, drop a .ttf/.otf font into App/Flashback/fonts.
  Maths (MathJax) is shown as readable text: F = (q₁ q₂)/(4πε₀ r²).
  Note types that build cards with JavaScript show their cloze text or
  fields instead. Whatever a note type's CSS hides (display: none)
  stays hidden, e.g. AnKing's tag list and second copy of the cloze;
  buttons are left out. Not supported: sound and video (cards show a ♪),
  [latex] images, and other card styling from CSS.

SETTINGS (App/Flashback/data/config.json, created on first run)
  deck_folders     folders to look in        default ["/mnt/SDCARD/Anki"]
  new_per_day      new cards per day         default 20
  reviews_per_day  reviews per day           default 200
  text_size        card text size (16–34)    default 22 (SELECT on a card)

FILES
  data/cache     unpacked decks (safe to delete; rebuilt automatically)
  data/progress  your study progress — keep this
  data/flashback.log  log for bug reports
