# Builds ug_new.apkg / ug_legacy.apkg from the Ultimate Geography deck's CrowdAnki
# export (CC BY 3.0, github.com/anki-geo/ultimate-geography): download
# Ultimate_Geography_v5.3_EN.zip from its releases, unzip into ./ug, then run.
import json, os, glob, shutil
from anki.collection import Collection, ExportAnkiPackageOptions
base = glob.glob('ug/*/')[0]
d = json.load(open(base + 'deck.json'))
shutil.rmtree('ugcol', ignore_errors=True); os.makedirs('ugcol')
col = Collection('ugcol/collection.anki2')
mm = col.models
uuid2id = {}
for m in d['note_models']:
    nt = mm.new(m['name'])
    for f in m['flds']:
        mm.add_field(nt, mm.new_field(f['name']))
    for t in m['tmpls']:
        tt = mm.new_template(t['name']); tt['qfmt'] = t['qfmt']; tt['afmt'] = t['afmt']
        mm.add_template(nt, tt)
    nt['css'] = m['css']
    nt['type'] = m.get('type', 0)
    mm.add(nt)
    uuid2id[m['crowdanki_uuid']] = mm.by_name(m['name'])['id']
did = col.decks.id(d['name'])
for n in d['notes']:
    nt = mm.get(uuid2id[n['note_model_uuid']])
    note = col.new_note(nt)
    for i, f in enumerate(n['fields'][:len(note.fields)]):
        note.fields[i] = f
    note.tags = n.get('tags', [])
    col.add_note(note, did)
for f in glob.glob(glob.escape(base) + 'media/*'):
    col.media.add_file(f)
print('notes', col.note_count(), 'cards', col.card_count(), 'models', [m['name'] for m in d['note_models']])
o = ExportAnkiPackageOptions(with_scheduling=False, with_deck_configs=False, with_media=True, legacy=False)
col.export_anki_package(out_path=os.path.abspath('ug_new.apkg'), options=o, limit=None)
o.legacy = True
col.export_anki_package(out_path=os.path.abspath('ug_legacy.apkg'), options=o, limit=None)
col.close()
