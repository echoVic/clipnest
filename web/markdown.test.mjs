import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { renderMarkdown } from './markdown.js';

class Element {
  constructor(tag) { this.tag = tag; this.children = []; this.textContent = ''; }
  append(...nodes) { this.children.push(...nodes); }
}

function documentStub() {
  return {
    createElement: tag => new Element(tag),
    createTextNode: text => ({ textContent: text }),
    createDocumentFragment: () => new Element('fragment'),
  };
}

test('incomplete headings and malformed fences cannot stall preview', () => {
  const source = `
    import { renderMarkdown } from ${JSON.stringify(new URL('./markdown.js', import.meta.url).href)};
    ${Element.toString()}
    globalThis.document = (${documentStub.toString()})();
    for (const text of ['# ', '### ', '\\\`\\\`\\\`not a valid fence', '\\\`\\\`\\\`\\\`', '# \\nend']) {
      const out = renderMarkdown(text);
      if (!out.children.length) throw Error('content lost');
    }
  `;
  const result = spawnSync(process.execPath, ['--input-type=module', '-e', source], { timeout: 1500, encoding: 'utf8' });
  assert.equal(result.error, undefined, `preview failed to finish: ${result.error}`);
  assert.equal(result.status, 0, result.stderr);
});

test('supported blocks render without treating raw HTML as markup', () => {
  const old = globalThis.document;
  globalThis.document = documentStub();
  try {
    const out = renderMarkdown('# Title\n\n<script>alert(1)</script>\n\n```js\nconst x = 1\n```');
    assert.deepEqual(out.children.map(n => n.tag), ['h1', 'p', 'pre']);
    assert.equal(out.children[1].children[0].textContent, '<script>alert(1)</script>');
    assert.equal(out.children[2].children[0].textContent, 'const x = 1');
  } finally { globalThis.document = old; }
});
