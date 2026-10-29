"""Render the screenshots image_text_darwin_test.go recognizes with Vision.

Run from the repository root on macOS (uses system fonts):
    python3 testdata/image-text/make_fixtures.py
"""
import os
from PIL import Image, ImageDraw, ImageFont

OUT = os.path.dirname(os.path.abspath(__file__))
HIRAGINO = '/System/Library/Fonts/Hiragino Sans GB.ttc'
MENLO = '/System/Library/Fonts/Menlo.ttc'


def save(im, name):
    # Palette PNGs keep the committed files small.
    im.convert('P', palette=Image.ADAPTIVE, colors=16).save(os.path.join(OUT, name), optimize=True)


# Chinese interface text, English prose and Go code in one small screenshot.
mixed = [
    (HIRAGINO, '剪贴板记录已暂停，新的复制不会保存。'),
    (HIRAGINO, 'Recognized text in screenshots becomes searchable.'),
    (MENLO, 'if err := tx.Commit(); err != nil {'),
    (MENLO, '    return fmt.Errorf("save: %w", err)'),
]
im = Image.new('RGB', (900, 300), 'white')
d = ImageDraw.Draw(im)
for i, (font, text) in enumerate(mixed):
    d.text((40, 30 + i * 62), text, font=ImageFont.truetype(font, 28), fill='#1f1f24')
save(im, 'mixed.png')

# A 4200 px wide screenshot in three tiles whose cores meet at x=1441 and
# x=2758: one line crosses the second edge, and the 22 px sidebar text is
# too small for whole-image recognition.
im = Image.new('RGB', (4200, 900), '#1e1e1e')
d = ImageDraw.Draw(im)
side = ImageFont.truetype(HIRAGINO, 22)
for i, name in enumerate(['image_text.go', 'sqlite_history.go', 'preferences_view.go']):
    d.text((30, 60 + i * 44), name, font=side, fill='#cccccc')
code = ImageFont.truetype(MENLO, 30)
d.text((2000, 420), 'clipboard history stays on this Mac and becomes searchable offline', font=code, fill='#d4d4d4')
d.text((3400, 700), '本地识别 不联网', font=ImageFont.truetype(HIRAGINO, 30), fill='#d4d4d4')
save(im, 'wide.png')

# A 4000 px long screenshot in three rows of tiles whose cores meet at y=1375
# and y=2625. The second and fourth lines lie where two tiles overlap.
im = Image.new('RGB', (1000, 4000), 'white')
d = ImageDraw.Draw(im)
body = ImageFont.truetype(HIRAGINO, 28)
for y, text in [(100, '第一段：剪贴板历史'), (1400, 'Both tiles above and below see this line'), (2000, '第二段：本地文字识别'),
                (2590, 'Another line seen by two tiles'), (3800, '最后一行 end of screenshot')]:
    d.text((40, y), text, font=body, fill='#1f1f24')
save(im, 'tall.png')

# The browser demo's sample screenshot; web/demo-image.js embeds it as base64
# with the text Vision returns for it.
im = Image.new('RGB', (560, 300), '#f6f6fa')
d = ImageDraw.Draw(im)
d.rounded_rectangle((16, 16, 544, 284), 14, fill='white', outline='#e4e2ec')
d.text((40, 38), '截图里的文字，也能搜到', font=ImageFont.truetype(HIRAGINO, 22), fill='#333440')
body = ImageFont.truetype(HIRAGINO, 17)
for i, text in enumerate(['识别图片文字：已开启', '在本机识别截图中的文字，不联网。', '图片会在后台逐张识别，完成后可搜索和复制。', 'Recognized text becomes searchable.']):
    d.text((40, 96 + i * 40), text, font=body, fill='#5b5c68')
d.rounded_rectangle((470, 98, 506, 120), 11, fill='#7864da')
d.ellipse((486, 100, 504, 118), fill='white')
save(im, 'demo.png')
