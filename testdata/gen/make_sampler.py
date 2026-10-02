# Builds the small test decks (new.apkg, legacy.apkg, science_only.apkg) with
# Anki's own Python library: pip install anki genanki. Run from testdata/.
import os, shutil, struct, zlib, zipfile
from anki.collection import Collection, ExportAnkiPackageOptions
from anki.exporting import AnkiPackageExporter

def png(path, w, h, rgb):
    raw = b''.join(b'\x00' + bytes(rgb) * w for _ in range(h))
    def ch(t, d): return struct.pack('>I', len(d)) + t + d + struct.pack('>I', zlib.crc32(t + d) & 0xffffffff)
    open(path, 'wb').write(b'\x89PNG\r\n\x1a\n' + ch(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0)) + ch(b'IDAT', zlib.compress(raw)) + ch(b'IEND', b''))

shutil.rmtree('col', ignore_errors=True); os.makedirs('col')
col = Collection('col/collection.anki2')
mm = col.models
top = col.decks.id('Spanish Basics')
sub = col.decks.id('Spanish Basics::Food')
basic = mm.by_name('Basic'); rev = mm.by_name('Basic (and reversed card)'); cloze = mm.by_name('Cloze')
def add(model, deck, fields, tags=()):
    n = col.new_note(model)
    for i, f in enumerate(fields): n.fields[i] = f
    n.tags = list(tags)
    col.add_note(n, deck)
add(basic, top, ['hello', '<b>hola</b>'], ['greeting'])
add(basic, top, ['thank you', 'gracias<br><i>(informal: graciñas? no)</i>'])
add(rev, top, ['goodbye', 'adiós'])
add(rev, sub, ['apple', 'la manzana <img src="apple.png">'])
add(basic, sub, ['bread &amp; butter', 'pan y mantequilla[sound:pan.mp3]'])
png('apple.png', 64, 48, (220, 30, 30))
col.media.add_file('apple.png')
open('pan.mp3','wb').write(b'ID3fake'); col.media.add_file('pan.mp3')
d2 = col.decks.id('Science')
add(cloze, d2, ['The {{c1::mitochondria}} is the {{c2::powerhouse::energy}} of the cell.', 'Extra: biology 101'])
add(cloze, d2, ['Water boils at {{c1::100}} °C at sea level.', ''])
for i in range(40):
    add(basic, d2, [f'Q{i}: what is {i} squared?', f'{i*i}'])
d3 = col.decks.id('日本語')
add(basic, d3, ['猫', 'ねこ — cat'])
add(basic, d3, ['犬', 'いぬ — dog <ul><li>one</li><li>two</li></ul>'])
long = ' '.join(['This is a very long answer field that should overflow an SQLite page.'] * 120)
add(basic, d2, ['long one', long])
# a custom note type with conditionals and FrontSide
m = mm.new('Vocab+')
for f in ['Word', 'Meaning', 'Example']: mm.add_field(m, mm.new_field(f))
t = mm.new_template('Recognition')
t['qfmt'] = '<div class=w>{{Word}}</div>{{#Example}}<div>e.g. {{Example}}</div>{{/Example}}{{^Example}}(no example){{/Example}}'
t['afmt'] = '{{FrontSide}}<hr id=answer>{{Meaning}}{{hint:Example}}'
mm.add_template(m, t); mm.add(m)
m = mm.by_name('Vocab+')
add(m, top, ['perro', 'dog', 'El perro ladra.'])
add(m, top, ['gato', 'cat', ''])

# image occlusion: a 'map' with three labelled regions
png('map.png', 320, 200, (120, 170, 120))
io_nt = col.models.by_name('Image Occlusion')
if io_nt is None:
    col.add_image_occlusion_notetype(); io_nt = col.models.by_name('Image Occlusion')
col.decks.select(d2)
occ = ('{{c1::image-occlusion:rect:left=.1:top=.1:width=.3:height=.2:oi=1}}'
       '{{c2::image-occlusion:ellipse:left=.5:top=.4:rx=.15:ry=.2:oi=1}}'
       '{{c3::image-occlusion:polygon:points=.2,.7 .4,.95 .05,.95:oi=1}}')
col.add_image_occlusion_note(notetype_id=io_nt['id'], image_path=os.path.abspath('map.png'), occlusions=occ, header='Label the map', back_extra='', tags=[])


opts = ExportAnkiPackageOptions(with_scheduling=False, with_deck_configs=False, with_media=True, legacy=False)
from anki.collection import ExportLimit, DeckIdLimit
col.export_anki_package(out_path=os.path.abspath('new.apkg'), options=opts, limit=None)
opts.legacy = True
col.export_anki_package(out_path=os.path.abspath('legacy.apkg'), options=opts, limit=None)
col.export_anki_package(out_path=os.path.abspath('science_only.apkg'), options=opts, limit=DeckIdLimit(deck_id=d2))
col.close()
for f in ['new.apkg', 'legacy.apkg', 'science_only.apkg']:
    print(f, [ (i.filename, i.file_size) for i in zipfile.ZipFile(f).infolist()])

# genanki-style package (schema 11 only, as most script-made community decks are)
import genanki
_m = genanki.Model(1607392319, 'Simple Model', fields=[{'name': 'Question'}, {'name': 'Answer'}],
  templates=[{'name': 'Card 1', 'qfmt': '{{Question}}', 'afmt': '{{FrontSide}}<hr id="answer">{{Answer}}'}])
_d = genanki.Deck(2059400110, 'Country Capitals')
for q, a in [('France', 'Paris'), ('Japan', 'Tokyo'), ('Kenya', 'Nairobi'), ('Peru', 'Lima')]:
    _d.add_note(genanki.Note(model=_m, fields=[q, a]))
genanki.Package(_d).write_to_file('genanki.apkg')
