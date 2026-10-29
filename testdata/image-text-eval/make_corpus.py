"""Build the image text evaluation corpus described in docs/image-text-validation.md.

Run on macOS (system fonts and pictures) with Pillow installed:
    python3 testdata/image-text-eval/make_corpus.py <out>

<out>/small    13 small screenshots: Chinese UI, mixed text, Traditional
               Chinese, Go/TS/JSON code, terminal errors, links, numbers
<out>/screens  7 full screens from 1920×1080 to 6016×3384 and 1600×9000, at
               1x and Retina density
<out>/photos   photos without text from macOS's built-in pictures

Each directory gets truth.json: the lines drawn in every image and the search
queries that must be found in its recognized text.
"""
import json
import os
import subprocess
import sys

from PIL import Image, ImageDraw, ImageFont

FONTS = {
    'hiragino': '/System/Library/Fonts/Hiragino Sans GB.ttc',
    'heiti': '/System/Library/Fonts/STHeiti Medium.ttc',
    'songti': '/System/Library/Fonts/Supplemental/Songti.ttc',
    'menlo': '/System/Library/Fonts/Menlo.ttc',
    'sfmono': '/System/Library/Fonts/SFNSMono.ttf',
    'helvetica': '/System/Library/Fonts/Helvetica.ttc',
    'sf': '/System/Library/Fonts/SFNS.ttf',
}
LIGHT = ('white', '#1f1f24')
DARK = ('#1e1e1e', '#d4d4d4')

SMALL = [
    ('go-dark', 'menlo', 26, DARK, [
        'func (s *Store) Save(ctx context.Context) error {',
        '    if err := s.db.PingContext(ctx); err != nil {',
        '        return fmt.Errorf("ping: %w", err)',
        '    }',
        '    return nil',
        '}',
    ], ['func (s *Store)', 'PingContext(ctx)', 'err != nil', 'fmt.Errorf', '"ping: %w"', 'return nil']),
    ('ts-light', 'sfmono', 24, LIGHT, [
        'export async function fetchUser(id: string) {',
        '  const res = await fetch(`/api/users/${id}`);',
        '  if (!res.ok) throw new Error("not found");',
        '  return res.json() as Promise<User>;',
        '}',
    ], ['fetchUser(id: string)', 'await fetch(', '/api/users/', '!res.ok', 'new Error("not found")', 'Promise<User>']),
    ('json-light', 'menlo', 24, LIGHT, [
        '{',
        '  "name": "clipnest",',
        '  "version": "0.1.8",',
        '  "minimumSystemVersion": "13.0",',
        '  "updates": { "github": "echoVic/clipnest" }',
        '}',
    ], ['"name": "clipnest"', '"version": "0.1.8"', 'minimumSystemVersion', 'echoVic/clipnest']),
    ('terminal-dark', 'menlo', 22, DARK, [
        '$ npm run build',
        'Error: ENOENT: no such file or directory, open \'config.yaml\'',
        '    at Object.openSync (node:fs:573:3)',
        '    at /Users/dev/project/src/index.ts:42:13',
        'exit status 1',
    ], ['npm run build', 'ENOENT', 'no such file or directory', 'config.yaml', 'node:fs', 'src/index.ts:42:13', 'exit status 1']),
    ('settings-zh', 'hiragino', 30, LIGHT, [
        '系统设置 > 隐私与安全性 > 辅助功能',
        '允许下面的应用控制您的电脑。',
        '剪贴板记录已暂停，新的复制不会保存。',
        '常用语和历史分开保存。',
    ], ['隐私与安全性', '辅助功能', '控制您的电脑', '已暂停', '新的复制不会保存', '常用语']),
    ('chat-zh', 'heiti', 28, LIGHT, [
        '收到，我今天下班前给你答复。',
        '会议改到周四上午十点，地点在三楼会议室。',
        '记得带上合同原件和身份证复印件。',
    ], ['下班前', '周四上午十点', '三楼会议室', '合同原件', '身份证复印件']),
    ('mixed-tech', 'hiragino', 28, LIGHT, [
        '部署失败：Error 502 Bad Gateway，请检查 nginx 配置',
        '在 Settings 里打开 Dark Mode 后重启 Xcode',
        '数据库连接超时 (timeout=30s)，重试 3 次后放弃',
    ], ['部署失败', '502 Bad Gateway', 'nginx', 'Dark Mode', 'Xcode', 'timeout=30s', '重试']),
    ('english-doc', 'helvetica', 26, LIGHT, [
        'Clipnest keeps your clipboard history on this Mac.',
        'Press Command-Shift-V to open the panel, then pick an item.',
        'Recognized text in screenshots becomes searchable.',
    ], ['clipboard history', 'Command-Shift-V', 'open the panel', 'Recognized text', 'searchable']),
    ('urls', 'sf', 26, LIGHT, [
        'https://github.com/echoVic/clipnest/releases',
        'support@example.com',
        'Server: 192.168.1.10:8080',
        'ssh deploy@prod-01.internal -p 2222',
    ], ['github.com/echoVic/clipnest', 'support@example.com', '192.168.1.10:8080', 'deploy@prod-01', '-p 2222']),
    ('numbers-mixed', 'songti', 28, LIGHT, [
        '订单号 2026102800123  金额 ¥1,299.00',
        'Invoice #INV-2026-0042  Total $42.50',
        '验证码 482913，5 分钟内有效。',
    ], ['2026102800123', '1,299.00', 'INV-2026-0042', '$42.50', '482913', '分钟内有效']),
    ('small-mixed', 'hiragino', 15, LIGHT, [
        '小字号：请在 2026-11-01 前提交季度报告 Q3 Report。',
        'Small print: contact ops-team@example.com for access.',
    ], ['2026-11-01', '季度报告', 'Q3 Report', 'ops-team@example.com', 'for access']),
    ('code-zh-comments', 'hiragino', 24, LIGHT, [
        '// 计算总价，含税',
        'total := price * float64(count)',
        'tax := total * 0.13 // 增值税',
        'log.Printf("合计：%.2f", total+tax)',
    ], ['计算总价', 'price * float64(count)', 'total * 0.13', '增值税', 'log.Printf']),
    ('hant-ui', 'heiti', 28, LIGHT, [
        '系統設定 > 隱私權與安全性',
        '剪貼簿紀錄已暫停，新的複製不會儲存。',
        '請在週五前回覆這封郵件。',
    ], ['隱私權與安全性', '剪貼簿紀錄', '不會儲存', '週五前回覆']),
]

CODE = [
    'func (s *Store) Save(ctx context.Context, items []Item) error {',
    '    tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})',
    '    if err != nil { return fmt.Errorf("begin transaction: %w", err) }',
    '    defer tx.Rollback() // keep data consistent on failure',
    '    for _, item := range items { if err := insertItem(tx, item); err != nil { return err } }',
    '    return tx.Commit()',
    '}',
]
CHAT = ['收到，我今天下班前给你答复。', '会议改到周四上午十点，地点在三楼会议室。', '记得带上合同原件和身份证复印件。',
        'Deploy finished: build #4821 passed all 312 checks', '把 staging 的 nginx 超时调到 60s 再观察一下']
SIDEBAR = ['clipnest', 'image_text.go', 'sqlite_history.go', 'history_view.go', 'preferences_view.go', 'web/app.js', 'README.md']
TERMINAL = ['$ go test -race -count=1 ./...', 'ok  \tclipnest\t12.481s', '$ git status --short', 'M  image_text_darwin.go',
            'Error: ENOENT: no such file or directory, open \'config.yaml\'']
ARTICLE = ['剪贴板历史面板支持搜索截图中的文字，识别在本机完成，不上传任何内容。',
           'Recognized text in screenshots becomes searchable and can be copied from the preview.',
           '关闭「图片文字」后，已识别的文字会被删除，索引同步清理。',
           'Large displays produce screenshots wider than five thousand pixels; small interface text must still be found.']

# Blocks are (x, y, font, size, line step, lines).
SCREENS = [
    ('qhd-1x-ide', (2560, 1440), [
        (20, 60, 'sf', 13, 24, SIDEBAR), (300, 60, 'menlo', 13, 22, CODE), (300, 1100, 'menlo', 12, 20, TERMINAL),
        (1900, 60, 'hiragino', 13, 26, CHAT)]),
    ('fhd-1x', (1920, 1080), [
        (20, 50, 'sf', 12, 22, SIDEBAR), (260, 50, 'hiragino', 14, 28, ARTICLE), (260, 400, 'menlo', 12, 20, CODE)]),
    ('mbp16-2x', (3456, 2234), [
        (40, 120, 'sf', 26, 48, SIDEBAR), (560, 120, 'hiragino', 32, 60, ARTICLE), (560, 600, 'menlo', 24, 42, CODE),
        (560, 1300, 'menlo', 22, 40, TERMINAL)]),
    ('studio-2x', (5120, 2880), [
        (40, 120, 'sf', 26, 50, SIDEBAR), (520, 120, 'menlo', 26, 46, CODE), (520, 700, 'hiragino', 28, 54, ARTICLE),
        (3500, 120, 'hiragino', 28, 56, CHAT), (520, 2200, 'menlo', 24, 44, TERMINAL)]),
    ('xdr-2x', (6016, 3384), [
        (60, 140, 'sf', 26, 50, SIDEBAR), (620, 140, 'hiragino', 30, 58, ARTICLE), (620, 700, 'menlo', 26, 46, CODE),
        (4300, 140, 'hiragino', 28, 56, CHAT), (620, 2600, 'menlo', 24, 44, TERMINAL)]),
    ('tall-scroll-2x', (1600, 9000), [
        (40, 100, 'hiragino', 28, 60, ARTICLE), (40, 2600, 'menlo', 24, 44, CODE), (40, 6000, 'hiragino', 28, 56, CHAT)]),
    ('mba13-2x', (2560, 1664), [
        (40, 100, 'sf', 24, 44, SIDEBAR), (480, 100, 'menlo', 22, 40, CODE), (480, 600, 'hiragino', 26, 50, CHAT)]),
]
SCREEN_QUERIES = ['BeginTx(ctx', 'sql.LevelSerializable', 'begin transaction', 'keep data consistent', 'insertItem(tx, item)',
                  'tx.Commit()', '下班前', '三楼会议室', '身份证复印件', 'build #4821', 'nginx', 'image_text.go',
                  'preferences_view.go', 'go test -race', 'ENOENT', 'config.yaml', '不上传任何内容', 'can be copied',
                  '索引同步清理', 'five thousand pixels', 'sqlite_history.go', '12.481s']

PHOTOS = [f'/Library/User Pictures/Animals/{n}.heic' for n in ['Eagle', 'Owl', 'Parrot', 'Penguin', 'Zebra']]
PHOTOS += [f'/Library/User Pictures/Flowers/{n}.heic' for n in
           ['Dahlia', 'Dandelion', 'Flower', 'Lotus', 'Poppy', 'Red Rose', 'Sunflower', 'Whiterose', 'Yellow Daisy']]
PHOTOS += [f'/System/Library/Desktop Pictures/{n}.heic' for n in ['iMac Blue', 'Mac Purple']]


def small(out):
    truth = []
    for name, font, size, (bg, fg), lines, queries in SMALL:
        f = ImageFont.truetype(FONTS[font], size)
        step = int(size * 1.7)
        im = Image.new('RGB', (max(int(f.getlength(l)) for l in lines) + 80, step * len(lines) + 60), bg)
        d = ImageDraw.Draw(im)
        for i, line in enumerate(lines):
            d.text((40, 30 + i * step), line, font=f, fill=fg)
        im.save(os.path.join(out, name + '.png'))
        truth.append({'name': name, 'lines': lines, 'queries': queries})
    return truth


def screens(out):
    truth = []
    for name, size, blocks in SCREENS:
        im = Image.new('RGB', size, (250, 250, 252))
        d = ImageDraw.Draw(im)
        lines = []
        for x, y, font, px, step, items in blocks:
            f = ImageFont.truetype(FONTS[font], px)
            for i, text in enumerate(items):
                d.text((x, y + i * step), text, font=f, fill=(25, 25, 32))
                lines.append(text)
        im.save(os.path.join(out, name + '.png'))
        text = '\n'.join(lines)
        truth.append({'name': name, 'lines': lines, 'queries': [q for q in SCREEN_QUERIES if q in text]})
    return truth


def photos(out):
    truth = []
    for path in PHOTOS:
        if not os.path.exists(path):
            print('skip missing', path)
            continue
        name = os.path.basename(path)[:-5].replace(' ', '-')
        subprocess.run(['sips', '-s', 'format', 'png', '-Z', '1600', path, '--out', os.path.join(out, name + '.png')],
                       check=True, capture_output=True)
        truth.append({'name': name, 'lines': [], 'queries': []})
    return truth


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    for name, build in [('small', small), ('screens', screens), ('photos', photos)]:
        out = os.path.join(sys.argv[1], name)
        os.makedirs(out, exist_ok=True)
        truth = build(out)
        with open(os.path.join(out, 'truth.json'), 'w') as f:
            json.dump(truth, f, ensure_ascii=False, indent=1)
        print(f'{out}: {len(truth)} images, {sum(len(t["queries"]) for t in truth)} queries')


main()
