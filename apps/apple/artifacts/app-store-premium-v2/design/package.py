"""Package the alternate artwork without modifying the approved original set."""
from pathlib import Path
import hashlib
import html
import json
import struct
import subprocess
import zipfile

root = Path(__file__).resolve().parent.parent
original_root = root.parent / 'app-store'
items = json.loads((root / 'design/campaign.json').read_text())


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def png_info(path):
    data = path.read_bytes()
    assert data[:8] == b'\x89PNG\r\n\x1a\n', path
    width, height, depth, color = struct.unpack('>IIBB', data[16:26])
    return dict(width=width, height=height, bit_depth=depth, color_type=color)


original_hashes = json.loads((root / 'design/original-set-hashes.json').read_text())
for name, expected in original_hashes.items():
    assert digest(original_root / name) == expected, f'Original changed: {name}'

screenshots = []
for item in items:
    n = item['index']
    relative_path = f"iphone/{n:02d}-{item['slug']}.png"
    path = root / relative_path
    info = png_info(path)
    assert (info['width'], info['height'], info['color_type']) == (1284, 2778, 2), (path, info)
    profile = subprocess.check_output(['sips', '-g', 'profile', str(path)], text=True)
    assert 'sRGB' in profile, profile
    native = f"native/iphone/{item['native']}"
    raw = f'design/{n:02d}-generated-original.png'
    screenshots.append(dict(
        index=n, title=item['title'], subtitle=item['sub'], theme=item['theme'],
        path=relative_path, width=1284, height=2778, mode='RGB', profile='sRGB',
        sha256=digest(path), native_reference=native, native_reference_info=png_info(root / native),
        generated_original=raw, generated_original_info=png_info(root / raw),
        prompt=f"design/{n:02d}-{item['slug']}-prompt.txt",
        export='sips resampling and conversion to opaque sRGB PNG at the original App Store canvas size.',
    ))

assert [s['index'] for s in screenshots] == list(range(1, 9))
manifest = dict(
    status='complete_iphone_set', direction='Premium editorial — varied compositions',
    tool='built-in image_gen', size=[1284, 2778], original_set='../app-store',
    original_set_preserved=True, preservation_check='SHA-256 matches for all eight original PNGs and their ZIP.',
    screenshots=screenshots,
    demo_content='Fictional conversations and offline native app previews supplied as image-generation references.',
    feature_note='Scheduled chats require a compatible Mindwire computer runtime. The app screenshot states that its computer must be awake and Mindwire running.',
    publication='Local artwork only; no App Store Connect assets changed.',
)
(root / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')

archive_name = 'Mindwire-iPhone-Premium-v2-8.zip'
with zipfile.ZipFile(root / archive_name, 'w', zipfile.ZIP_DEFLATED) as archive:
    for screenshot in screenshots:
        archive.write(root / screenshot['path'], Path(screenshot['path']).name)
with zipfile.ZipFile(root / archive_name) as archive:
    assert len(archive.namelist()) == 8 and archive.testzip() is None

original_manifest = json.loads((original_root / 'manifest.json').read_text())
original_screenshots = original_manifest['screenshots']
gallery_data = dict(
    premium=[dict(index=s['index'], title=s['title'], path=s['path']) for s in screenshots],
    original=[dict(index=s['index'], title=s['title'], path='../app-store/' + s['path']) for s in original_screenshots],
)
cards = ''.join(
    f'<a class="card" href="{html.escape(s["path"])}" target="_blank" rel="noopener">'
    f'<img src="{html.escape(s["path"])}" alt="{html.escape(s["title"])}">'
    f'<div class="caption"><span>{s["index"]:02d}</span><strong>{html.escape(s["title"])}</strong></div></a>'
    for s in screenshots
)
page = '''<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Mindwire — Premium App Store artwork</title>
<style>
@font-face{font-family:Gellix;src:url('../../Mindwire/Resources/Fonts/Gellix-Regular.ttf')}
@font-face{font-family:Gellix;src:url('../../Mindwire/Resources/Fonts/Gellix-SemiBold.ttf');font-weight:600}
*{box-sizing:border-box}body{margin:0;padding:32px 40px 28px;background:#eeece7;color:#252622;font-family:Gellix,system-ui,sans-serif}
header{display:flex;align-items:center;justify-content:space-between;gap:24px;margin-bottom:26px}
.identity{display:flex;align-items:center;gap:14px}.mark{width:34px;height:34px;border:3px solid currentColor;border-radius:9px;flex:none}
h1{font-size:28px;line-height:1.15;letter-spacing:-.5px;margin:0 0 6px;font-weight:600}p{margin:0;font-size:16px;color:#64675f}
nav{display:flex;align-items:center;gap:14px}.switch{display:flex;border-radius:99px;padding:4px;background:#dedbd4}
button{font:inherit;border:0;border-radius:99px;padding:10px 18px;background:transparent;color:#64675f;cursor:pointer}
button[aria-pressed=true]{background:#fcfbf9;color:#252622;box-shadow:0 2px 6px #2526220d}
.download{padding:14px 20px;border-radius:99px;background:#252622;color:white;text-decoration:none;font-size:15px;white-space:nowrap}
main{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:22px}
.card{display:block;overflow:hidden;border-radius:14px;background:#fcfbf9;color:inherit;text-decoration:none;box-shadow:0 3px 14px #25262208}
.card img{display:block;width:100%;height:auto;aspect-ratio:1284/2778}.caption{display:flex;gap:10px;align-items:center;padding:13px 14px;font-size:15px;background:#fcfbf9}
.caption span{color:#9e582f;font-size:13px}.caption strong{font-weight:400}footer{margin-top:22px;font-size:14px;color:#64675f}footer a{color:inherit}
a:focus-visible,button:focus-visible{outline:3px solid #9e582f;outline-offset:3px}
@media(max-width:1050px){body{padding:24px}main{grid-template-columns:repeat(2,minmax(0,1fr))}header{align-items:flex-start;flex-direction:column}nav{width:100%;justify-content:space-between}}
@media(max-width:520px){body{padding:18px}main{grid-template-columns:1fr}nav{flex-wrap:wrap}.caption{font-size:16px}}
</style></head><body>
<header><div class="identity"><span class="mark" aria-hidden="true"></span><div><h1>Mindwire</h1><p id="direction">Premium direction · 8 iPhone screens · 1284 × 2778</p></div></div>
<nav aria-label="Compare artwork"><div class="switch"><button type="button" data-set="original" aria-pressed="false">Original</button><button type="button" data-set="premium" aria-pressed="true">Premium</button></div><a class="download" id="download" href="Mindwire-iPhone-Premium-v2-8.zip">Download this set</a></nav></header>
<main>'''+cards+'''</main>
<footer>Click a screen to view full size. Original artwork preserved. <a href="manifest.json">Asset details</a> · <a href="design/campaign.json">Saved prompts</a> · Created with built-in image generation using native app references.</footer>
<script type="application/json" id="sets">'''+json.dumps(gallery_data).replace('</', '<\\/')+'''</script>
<script>
const sets = JSON.parse(document.getElementById('sets').textContent);
const cards = [...document.querySelectorAll('.card')];
document.querySelectorAll('[data-set]').forEach(button => button.addEventListener('click', () => {
  const key = button.dataset.set;
  document.querySelectorAll('[data-set]').forEach(option => option.setAttribute('aria-pressed', String(option === button)));
  sets[key].forEach((item, index) => {
    const card = cards[index];
    card.href = item.path;
    card.querySelector('img').src = item.path;
    card.querySelector('img').alt = item.title;
    card.querySelector('strong').textContent = item.title;
  });
  document.getElementById('direction').textContent = (key === 'premium' ? 'Premium direction' : 'Original direction') + ' · 8 iPhone screens · 1284 × 2778';
  document.getElementById('download').href = key === 'premium' ? 'Mindwire-iPhone-Premium-v2-8.zip' : '../app-store/Mindwire-iPhone-App-Store-8.zip';
}));
</script></body></html>'''
(root / 'preview.html').write_text(page)

readme = 'Mindwire — Premium iPhone App Store artwork (alternate direction)\n\n'
readme += '8 screens, 1284 × 2778, opaque RGB PNG with sRGB profile.\n'
readme += 'The original set remains in ../app-store, unchanged. Open preview.html to switch between the two directions.\n\n'
for screenshot in screenshots:
    readme += f"{screenshot['index']}. {screenshot['title']} — {screenshot['path']}\n"
readme += '\nCreated with built-in image generation. Full prompts, original generations, and native app reference images are saved alongside the artwork. The images are creative marketing treatments of native offline demo screens. No App Store Connect assets have been replaced or uploaded.\n'
(root / 'README.txt').write_text(readme)
print(json.dumps(dict(screens=8, size=[1284,2778], mode='RGB', profile='sRGB', original_files_verified=len(original_hashes), archive=archive_name)))
