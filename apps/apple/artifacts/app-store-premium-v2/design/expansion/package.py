"""Validate and package the iPad, Mac, and product-page campaign expansion."""
from pathlib import Path
import difflib
import hashlib
import html
import json
import struct
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[2]
DESIGN = ROOT / 'design/expansion'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def png_info(path):
    data = path.read_bytes()
    assert data[:8] == b'\x89PNG\r\n\x1a\n', path
    w, h, depth, color = struct.unpack('>IIBB', data[16:26])
    return dict(width=w, height=h, bit_depth=depth, color_type=color)


def inspect(path, dimensions):
    file = ROOT / path
    info = png_info(file)
    assert (info['width'], info['height']) == dimensions, (path, info)
    assert info['color_type'] == 2 and info['bit_depth'] == 8, (path, info)
    profile = subprocess.check_output(['sips', '-g', 'profile', str(file)], text=True)
    assert 'sRGB' in profile, (path, profile)
    return dict(path=path, **info, profile='sRGB', alpha=False, sha256=digest(file))


iphone = json.loads((ROOT / 'manifest.json').read_text())
for item in iphone['screenshots']:
    verified = inspect(item['path'], (1284, 2778))
    assert verified['sha256'] == item['sha256'], f'iPhone artwork changed: {item["path"]}'
original_hashes = json.loads((ROOT / 'design/original-set-hashes.json').read_text())
for name, expected in original_hashes.items():
    assert digest(ROOT.parent / 'app-store' / name) == expected, name

groups = {}
for platform, dimensions in [('ipad', (2752, 2064)), ('mac', (2880, 1800))]:
    campaign = json.loads((DESIGN / f'{platform}-campaign.json').read_text())
    assert len(campaign) == 8
    entries = []
    for c in campaign:
        n = c['n']
        path = f'{platform}/{n:02d}-{c["slug"]}.png'
        raw = f'design/expansion/{platform}-{n:02d}-generated-original.png'
        native = f'native/{platform}/{c["native"]}'
        prompt = f'design/expansion/{platform}-{n:02d}-prompt.txt'
        (ROOT / prompt).write_text(c['prompt'] + '\n')
        entries.append(dict(index=n, title=c['title'], subtitle=c['sub'], theme=c['theme'],
                            **inspect(path, dimensions), native_reference=native,
                            native_reference_info=png_info(ROOT / native),
                            generated_original=raw, generated_original_info=png_info(ROOT / raw),
                            prompt=prompt))
    groups[platform] = entries

creatives = []
for title, filename, dimensions, prompt in [
    ('Header', 'Mindwire-Header-3840x1646.png', (3840, 1646), 'header'),
    ('Search Results', 'Mindwire-Search-3840x2560.png', (3840, 2560), 'search'),
]:
    raw = f'design/expansion/{prompt}-generated-original.png'
    creatives.append(dict(title=title, **inspect('product-page/' + filename, dimensions),
                          prompt=f'design/expansion/{prompt}-prompt.txt', generated_original=raw,
                          generated_original_info=png_info(ROOT / raw)))
groups['store'] = creatives

manifest = dict(
    status='complete', tool='built-in image_gen', date='2026-10-21',
    screenshots=groups, existing_iphone_manifest='manifest.json',
    existing_iphone_preserved=True, previous_original_set_preserved=True,
    export='Generated originals resampled with sips to the exact target canvas, opaque 8-bit RGB PNG with sRGB profile.',
    demo_content='Creative marketing treatments based on isolated native offline fixtures. Conversations and projects are fictional.',
    publication='Local files only. Nothing uploaded to App Store Connect.',
    specification_sources=[
        'https://developer.apple.com/help/app-store-connect/reference/app-information/screenshot-specifications',
        'https://developer.apple.com/help/app-store-connect/reference/app-information/creative-assets-specifications',
    ],
)
(ROOT / 'manifest-expansion.json').write_text(json.dumps(manifest, indent=2) + '\n')

archives = {
    'ipad': 'Mindwire-iPad-Premium-v2-8.zip',
    'mac': 'Mindwire-Mac-Premium-v2-8.zip',
    'store': 'Mindwire-Store-Creatives-Premium-v2.zip',
    'iphone': 'Mindwire-iPhone-Premium-v2-8.zip',
}
for key, entries in groups.items():
    with zipfile.ZipFile(ROOT / archives[key], 'w', zipfile.ZIP_DEFLATED) as z:
        for entry in entries:
            z.write(ROOT / entry['path'], Path(entry['path']).name)
    with zipfile.ZipFile(ROOT / archives[key]) as z:
        assert len(z.namelist()) == len(entries) and z.testzip() is None

all_entries = iphone['screenshots'] + groups['ipad'] + groups['mac'] + creatives
with zipfile.ZipFile(ROOT / 'Mindwire-Premium-All-Platforms.zip', 'w', zipfile.ZIP_DEFLATED) as z:
    for entry in all_entries:
        z.write(ROOT / entry['path'], entry['path'])
with zipfile.ZipFile(ROOT / 'Mindwire-Premium-All-Platforms.zip') as z:
    assert len(z.namelist()) == 26 and z.testzip() is None

groups['iphone'] = iphone['screenshots']
labels = {'store':'Store creatives', 'iphone':'iPhone', 'ipad':'iPad', 'mac':'Mac'}
details = {'store':'Header · 3840 × 1646 / Search Results · 3840 × 2560',
           'iphone':'8 screens · 1284 × 2778', 'ipad':'8 screens · 2752 × 2064', 'mac':'8 screens · 2880 × 1800'}
sections = []
for key in labels:
    cards = []
    for index, entry in enumerate(groups[key], start=1):
        path, title = html.escape(entry['path']), html.escape(entry['title'])
        cards.append(f'<a class="card" href="{path}" target="_blank" rel="noopener">'
                     f'<img src="{path}" alt="{title}" loading="lazy" width="{entry["width"]}" height="{entry["height"]}">'
                     f'<div class="caption"><span>{index:02d}</span><strong>{title}</strong>'
                     f'<small>{entry["width"]} × {entry["height"]}</small></div></a>')
    sections.append(f'<section id="panel-{key}" class="grid {key}" aria-label="{labels[key]}"'+('' if key == 'store' else ' hidden')+'>'+''.join(cards)+'</section>')
buttons = ''.join(f'<button type="button" data-tab="{key}" aria-pressed="{str(key == "store").lower()}">{title}</button>' for key, title in labels.items())
page = '''<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Mindwire — Premium store collection</title><style>
@font-face{font-family:Gellix;src:url('../../Mindwire/Resources/Fonts/Gellix-Regular.ttf')}
@font-face{font-family:Gellix;src:url('../../Mindwire/Resources/Fonts/Gellix-SemiBold.ttf');font-weight:600}
*{box-sizing:border-box}[hidden]{display:none!important}body{margin:0;background:#f2efe9;color:#252622;font-family:Gellix,system-ui,sans-serif;padding:40px}
header{display:flex;align-items:center;justify-content:space-between;gap:24px;margin-bottom:30px}.identity{display:flex;align-items:center;gap:14px}.mark{width:36px;height:36px;border:3px solid;border-radius:10px}h1{font-size:27px;line-height:1;margin:0 0 7px;letter-spacing:-.7px}header p{margin:0;color:#6c6c64;font-size:16px}
a{color:inherit}.all{display:inline-flex;align-items:center;border:1px solid #d6d1c7;border-radius:99px;padding:13px 20px;text-decoration:none;font-size:15px}
nav{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:22px}.tabs{display:flex;gap:4px;background:#e5e0d7;border-radius:99px;padding:4px}button{font:inherit;border:0;border-radius:99px;background:none;padding:11px 20px;cursor:pointer;color:#66675f;white-space:nowrap}button[aria-pressed=true]{background:#fcfbf9;color:#252622;box-shadow:0 2px 5px #25262210}.download{display:inline-flex;padding:14px 24px;border-radius:99px;background:#252622;color:#fff;text-decoration:none;font-size:15px;white-space:nowrap}
.details{font-size:15px;color:#66675f;margin:0 0 22px}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:24px;align-items:start}.grid.iphone{grid-template-columns:repeat(4,minmax(0,1fr))}.card{background:#fcfbf9;border-radius:14px;overflow:hidden;text-decoration:none;box-shadow:0 4px 14px #25262208;transition:transform .18s}.card:hover{transform:translateY(-3px)}.card img{display:block;width:100%;height:auto}.caption{display:flex;align-items:center;gap:12px;padding:15px 18px;font-size:15px}.caption span{font-size:12px;color:#9e582f}.caption strong{font-weight:400}.caption small{margin-left:auto;color:#797a71;white-space:nowrap;font-size:12px}.iphone .caption small{display:none}
footer{color:#6c6c64;font-size:14px;line-height:1.6;margin-top:28px}footer a{text-underline-offset:3px}a:focus-visible,button:focus-visible{outline:3px solid #9e582f;outline-offset:4px}
@media(max-width:900px){body{padding:24px}.grid.iphone{grid-template-columns:repeat(2,minmax(0,1fr))}.grid{grid-template-columns:1fr}nav{align-items:stretch;flex-direction:column}.download{align-self:flex-start}header{align-items:flex-start}.all{font-size:13px}.caption{flex-wrap:wrap}}
@media(max-width:520px){body{padding:18px}.tabs{overflow:auto}button{padding:10px 13px;font-size:14px}.all{display:none}.caption small{display:none}}
</style></head><body>
<header><div class="identity"><span class="mark" aria-hidden="true"></span><div><h1>Mindwire</h1><p>Premium App Store collection · 26 assets</p></div></div><a class="all" href="Mindwire-Premium-All-Platforms.zip">Download everything ↗</a></header>
<nav aria-label="Asset collections"><div class="tabs">'''+buttons+'''</div><a class="download" id="download" href="Mindwire-Store-Creatives-Premium-v2.zip">Download this set ↗</a></nav>
<p class="details" id="details">'''+details['store']+'''</p><main>'''+''.join(sections)+'''</main>
<footer>Click any artwork to open the full-size PNG. Opaque RGB · sRGB.<br>Built-in image generation using native offline app references. Original sets preserved. Nothing uploaded.<br><a href="manifest-expansion.json">Asset details &amp; prompts</a> · <a href="preview.html">Compare original iPhone direction</a></footer>
<script>
const details='''+json.dumps(details)+''';const downloads='''+json.dumps(archives)+''';
function select(){let key=location.hash.slice(1);if(!(key in details))key='store';document.querySelectorAll('[data-tab]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.tab===key)));document.querySelectorAll('.grid').forEach(s=>s.hidden=s.id!=='panel-'+key);document.getElementById('details').textContent=details[key];document.getElementById('download').href=downloads[key]}
document.querySelectorAll('[data-tab]').forEach(b=>b.addEventListener('click',()=>{location.hash=b.dataset.tab}));window.addEventListener('hashchange',select);select();
</script></body></html>'''
(ROOT / 'collection.html').write_text(page)

readme = '''Mindwire — Premium App Store campaign

Open collection.html to browse all 26 assets. Click each image for full size.
preview.html preserves the original versus premium iPhone comparison.

UPLOAD LOCATIONS
Product page > Header: product-page/Mindwire-Header-3840x1646.png
Product page > Search Results: product-page/Mindwire-Search-3840x2560.png
iPad 13-inch screenshots: ipad/01 through 08 — 2752 × 2064 landscape
Mac screenshots: mac/01 through 08 — 2880 × 1800 landscape
iPhone screenshots: iphone/01 through 08 — existing 1284 × 2778 portrait

All final PNGs: opaque 8-bit RGB, sRGB profile, verified exact dimensions.
Separate ZIP files contain only that set's upload PNGs. The all-platform ZIP
contains 26 PNGs arranged into their respective directories.

CREATION
Created with built-in image generation using the approved premium direction
and captured native offline demo screens. These are creative marketing
treatments, not pixel-identical raw screen captures. Generated originals were
resampled with sips for the exact upload dimensions. Native captures, originals,
and complete prompts are retained under native/ and design/expansion/.
manifest-expansion.json records dimensions, profiles, hashes and references.

Conversations and projects are fictional demo content. Scheduled chats need
their computer awake with Mindwire running, as stated in the shown UI.
Original and premium iPhone assets verified unchanged. Nothing uploaded to
App Store Connect.

Apple specifications referenced:
https://developer.apple.com/help/app-store-connect/reference/app-information/screenshot-specifications
https://developer.apple.com/help/app-store-connect/reference/app-information/creative-assets-specifications
'''
(ROOT / 'README.txt').write_text(readme)

before = DESIGN / 'AppStorePreviewScene-before.swift'
current = ROOT.parents[1] / 'Mindwire/Preview/AppStorePreviewScene.swift'
(DESIGN / 'native-preview-capture.patch').write_text(''.join(difflib.unified_diff(
    before.read_text().splitlines(True), current.read_text().splitlines(True),
    fromfile='AppStorePreviewScene-before.swift', tofile='AppStorePreviewScene.swift')))
print(json.dumps(dict(assets=26, ipad=8, mac=8, creatives=2, iphone_preserved=8,
                     original_files_verified=len(original_hashes), opaque_rgb=True, profile='sRGB',
                     archives=list(archives.values())+['Mindwire-Premium-All-Platforms.zip'])))
