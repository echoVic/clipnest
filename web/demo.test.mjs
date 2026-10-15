import test from 'node:test';
import assert from 'node:assert/strict';
import { createDemo } from './demo.js';
test('demo phrases follow the desktop rules', async () => {
 const { call } = createDemo();
 const list = await call('Phrases', '');
 assert.equal(list.length, 4); assert.equal(list[0].text, undefined);
 assert.equal(list[1].excerpt, 'Merged — thanks for the PR! Released in v0.1.4. This will land in the next update, which checks every 4 hours.');
 assert.equal((await call('Phrases', '  REPORT ')).length, 1);
 assert.equal((await call('PhraseDetail', list[1].id)).text, 'Merged — thanks for the PR! Released in v0.1.4.\nThis will land in the next update, which checks every 4 hours.');
 const added = await call('SavePhrase', '', 'new one');
 assert.deepEqual((await call('Phrases', '')).at(-1), { id: added.id, excerpt: 'new one' });
 await call('MovePhrase', added.id, -1); assert.equal((await call('Phrases', '')).at(-2).id, added.id);
 await call('MovePhrase', list[0].id, -1); assert.equal((await call('Phrases', ''))[0].id, list[0].id);
 await assert.rejects(call('MovePhrase', added.id, 2), /无效的移动/);
 await assert.rejects(call('SavePhrase', '', ' \n'), /内容不能为空/);
 await assert.rejects(call('SavePhrase', '', 'x'.repeat(65537)), /单条常用语最多 64 KB/);
 await call('SavePhrase', added.id, 'edited'); assert.equal((await call('PhraseDetail', added.id)).text, 'edited');
 await call('DeletePhrase', added.id);
 await assert.rejects(call('PhraseDetail', added.id), /这条常用语已删除/);
 assert.equal((await call('InsertPhrase', list[0].id, true)).message, '演示：内容已选中，未更改系统剪贴板');
 assert.equal((await call('Phrases', '')).length, 4);
});
test('demo excerpts collapse whitespace and cut at 140 characters', async () => {
 const { call } = createDemo();
 const saved = await call('SavePhrase', '', `  ${'词'.repeat(150)}\n\nend`);
 assert.equal((await call('Phrases', '')).find(p => p.id === saved.id).excerpt, `${'词'.repeat(140)}…`);
});
