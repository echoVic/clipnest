import test from 'node:test';
import assert from 'node:assert/strict';
import * as imageText from './image-text.js';

// A minimal DOM: text is only ever assigned, never parsed as HTML.
class Element {
  constructor(tag) { this.tagName = tag; this.children = []; this.attributes = new Map(); this.className = ''; this.text = ''; }
  set textContent(text) { this.text = String(text); this.children = []; }
  get textContent() { return this.text + this.children.map(child => child.textContent).join(''); }
  set innerHTML(_) { throw Error('recognized text must never be parsed as HTML'); }
  setAttribute(key, value) { this.attributes.set(key, String(value)); }
  getAttribute(key) { return this.attributes.get(key) ?? null; }
  append(...children) { this.children.push(...children); }
  find(className) {
    if (this.className.split(' ').includes(className)) return this;
    for (const child of this.children) { const found = child.find?.(className); if (found) return found; }
    return null;
  }
}
const document = { createElement: tag => new Element(tag) };
const preview = (e, copy = () => {}) => imageText.imageTextPreview(document, e, copy);

test('recognized text is shown as text with a copy-only action beside the preview', () => {
  const text = '<img src=x onerror=alert(1)>\n第二行 config.yaml';
  const copies = [];
  const { section, button } = preview({ kind: 'image', imageTextState: 'ready', imageText: text }, b => copies.push(b));
  assert.equal(section.getAttribute('aria-label'), '图片文字');
  assert.equal(section.find('image-text-body').textContent, text);
  assert.equal(section.find('image-text-copy'), null, 'the action stays visible outside the scrolling text');
  assert.equal(button.tagName, 'button');
  assert.equal(button.type, 'button');
  assert.equal(button.textContent, '复制文字');
  button.onclick();
  assert.deepEqual(copies, [button]);
});

test('every other state explains itself without a copy action', () => {
  const notes = {
    pending: /等待识别/, failed: /未能识别/, disabled: /已关闭/, tooLarge: /5000 万像素/,
    needsSQLite: /迁移到 SQLite/,
  };
  for (const [state, note] of Object.entries(notes)) {
    const { section, button } = preview({ kind: 'image', imageTextState: state, imageText: 'stale text' });
    assert.match(section.find('image-text-note').textContent, note, state);
    assert.equal(button, null, state);
    assert.ok(!section.textContent.includes('stale text'), state);
  }
  const empty = preview({ kind: 'image', imageTextState: 'ready', imageText: '' });
  assert.match(empty.section.find('image-text-note').textContent, /未识别到文字/);
  assert.equal(empty.button, null);
  // Other systems explain the feature once, in preferences, not under every image.
  assert.equal(preview({ kind: 'image', imageTextState: 'unsupported' }), null);
  assert.equal(preview({ kind: 'image' }), null, 'a backend without image text shows nothing');
  assert.equal(preview({ kind: 'text', imageTextState: 'ready', imageText: 'x' }), null);
});

test('the privacy switch needs macOS and SQLite; the browser demo stands in for macOS', () => {
  const sqlite = on => ({ backend: 'sqlite', imageText: on });
  assert.deepEqual(
    [true, false].map(on => imageText.imageTextSetting({ platform: 'darwin', storage: sqlite(on) }, false)).map(s => [s.available, s.checked]),
    [[true, true], [true, false]],
  );
  const json = imageText.imageTextSetting({ platform: 'darwin', storage: { backend: 'json', imageText: false } }, false);
  assert.equal(json.available, false); assert.match(json.note, /迁移到 SQLite/);
  const linux = imageText.imageTextSetting({ platform: 'linux', storage: sqlite(true) }, false);
  assert.equal(linux.available, false); assert.equal(linux.checked, false); assert.match(linux.note, /仅适用于 macOS/);
  const demo = imageText.imageTextSetting({ platform: 'demo', storage: sqlite(true) }, true);
  assert.equal(demo.available, true); assert.match(demo.note, /演示/);
});
