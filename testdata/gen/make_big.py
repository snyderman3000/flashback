# Builds big.apkg: 25,000 notes / 50,000 cards across 10 subdecks.
import os, shutil, random
from anki.collection import Collection, ExportAnkiPackageOptions
shutil.rmtree('bigcol', ignore_errors=True); os.makedirs('bigcol')
col = Collection('bigcol/collection.anki2')
rev = col.models.by_name('Basic (and reversed card)')
random.seed(1)
words = open('/usr/share/dict/words').read().split() if os.path.exists('/usr/share/dict/words') else [f'word{i}' for i in range(5000)]
decks = [col.decks.id(f'Big::Part {i}') for i in range(10)]
for i in range(25000):
    n = col.new_note(rev)
    n.fields[0] = ' '.join(random.choice(words) for _ in range(6)) + f' #{i}'
    n.fields[1] = '<div>' + ' '.join(random.choice(words) for _ in range(40)) + '</div>'
    col.add_note(n, decks[i % 10])
o = ExportAnkiPackageOptions(with_scheduling=False, with_deck_configs=False, with_media=True, legacy=False)
col.export_anki_package(out_path=os.path.abspath('big.apkg'), options=o, limit=None)
col.close()
