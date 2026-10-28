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
 assert.equal((await call('InsertPhrase', (await call('State')).session, list[0].id, true)).message, '演示：内容已选中，未更改系统剪贴板');
 await assert.rejects(call('InsertPhrase', 0, list[0].id, true), /会话已过期/);
 assert.equal((await call('Phrases', '')).length, 4);
});
test('demo excerpts collapse whitespace and cut at 140 characters', async () => {
 const { call } = createDemo();
 const saved = await call('SavePhrase', '', `  ${'词'.repeat(150)}\n\nend`);
 assert.equal((await call('Phrases', '')).find(p => p.id === saved.id).excerpt, `${'词'.repeat(140)}…`);
});
test('storage upgrade demo is opt-in and preserves sample content', async () => {
 const normal = createDemo();
 assert.equal((await normal.call('State')).storage.migrationAvailable, false);
 const demo = createDemo({ legacyStorage: true }), { call } = demo;
 const readyBefore = readyState(demo);
 await call('State');
 const before = await readyBefore;
 const phrases = await call('Phrases', '');
 assert.equal(before.storage.backend, 'json');
 assert.equal((await call('StorageStatus')).migrationAvailable, true);
 const readyAfter = readyState(demo);
 const after = await call('MigrateStorage');
 assert.deepEqual(after, { backend: 'sqlite', migrationAvailable: false, cleanupPending: false, retention: { maxMiB: 512, keepDays: 0 } });
 assert.deepEqual((await readyAfter).entries, before.entries);
 assert.deepEqual(await call('Phrases', ''), phrases);
 assert.deepEqual(await call('MigrateStorage'), after);
});

const readyState = demo => new Promise(resolve => {
 const off = demo.on('clipnest:panel', state => { if (!state.loading) { off(); resolve(state); } });
});

test('large demo pages metadata, validates sessions and never touches the clipboard', async () => {
 const demo = createDemo({ large: true });
 const ready = readyState(demo);
 const initial = await demo.call('State');
 assert.equal(initial.loading, true);
 const state = await ready;
 assert.equal(state.session, initial.session);
 assert.equal(state.total, 10000);
 assert.equal(state.entries.length, 50);
 assert.ok(state.entries.every(e => e.text === undefined && e.image === undefined));
 const page = await demo.call('Page', state.session, 1, '', 'all', 9950, 50);
 assert.equal(page.offset, 9950); assert.equal(page.total, 10000); assert.equal(page.entries.length, 50);
 assert.notEqual(page.entries[0].id, state.entries[0].id);
 assert.equal((await demo.call('Detail', state.session, page.entries[0].id)).id, page.entries[0].id);
 const choice = await demo.call('Choose', state.session, page.entries[0].id, true);
 assert.equal(choice.copied, false); assert.equal(choice.pasted, false);
 const nextReady = readyState(demo);
 const next = await demo.call('Delete', state.session, page.entries[0].id);
 assert.equal(next.loading, true); assert.notEqual(next.session, state.session);
 assert.equal((await nextReady).total, 9999);
 await assert.rejects(demo.call('Page', state.session, 1, '', 'all', 0, 50), /会话/);
 await assert.rejects(demo.call('Choose', state.session, state.entries[0].id, true), /会话/);
});

test('demo search rejects older revisions while allowing same-revision paging', async () => {
 const demo = createDemo({ large: true }), ready = readyState(demo);
 const { session } = await demo.call('State'); await ready;
 await demo.call('Search', session, 7);
 await assert.rejects(demo.call('Search', session, 6), /搜索已过期/);
 await assert.rejects(demo.call('Page', session, 6, '', 'all', 0, 50), /搜索已过期/);
 for (const offset of [0, 50, 100]) {
  assert.equal((await demo.call('Page', session, 7, '', 'all', offset, 50)).offset, offset);
 }
 const reopened = readyState(demo), next = await demo.call('State'); await reopened;
 await assert.rejects(demo.call('Search', session, 8), /会话/);
 await demo.call('Search', next.session, 9);
 await assert.rejects(demo.call('Page', next.session, 8, '', 'all', 0, 50), /搜索已过期/);
});

test('demo cancellation rejects an older pending page but preserves detail and newer pages', async () => {
 const demo = createDemo({ large: true }), ready = readyState(demo);
 const { session } = await demo.call('State'); const state = await ready;
 const old = demo.call('Page', session, 1, '设计', 'all', 0, 50);
 const detail = demo.call('Detail', session, state.entries[0].id);
 await demo.call('Search', session, 2);
 await assert.rejects(old, /搜索已过期/);
 assert.equal((await detail).id, state.entries[0].id);
 assert.equal((await demo.call('Page', session, 2, '', 'all', 50, 50)).entries[0].id, 'sample-50');
 assert.equal((await demo.call('Choose', session, 'sample-50', true)).copied, false);
});

test('retention is explicit, validates numbers, cleans immediately and exempts phrases', async () => {
 const demo = createDemo({ legacyStorage: true, large: true });
 await assert.rejects(demo.call('SaveRetention', { maxMiB: 0, keepDays: 1 }), /迁移/);
 await demo.call('MigrateStorage');
 for (const policy of [{ maxMiB: -1, keepDays: 0 }, { maxMiB: 1.5, keepDays: 0 }, { maxMiB: 0, keepDays: NaN }]) {
  await assert.rejects(demo.call('SaveRetention', policy));
 }
 const phrases = await demo.call('Phrases', '');
 const ready = readyState(demo);
 const loading = await demo.call('SaveRetention', { maxMiB: 0, keepDays: 1 });
 assert.equal(loading.loading, true);
 const state = await ready;
 assert.equal(state.session, loading.session);
 assert.ok(state.total < 10000 && state.total > 0);
 assert.deepEqual(state.storage.retention, { maxMiB: 0, keepDays: 1 });
 assert.deepEqual(await demo.call('Phrases', ''), phrases);
 const clearReady = readyState(demo);
 assert.equal((await demo.call('Clear')).loading, true);
 assert.equal((await clearReady).total, 0);
});

test('mutation loading states are returned, while migration also publishes loading', async () => {
 const demo = createDemo({ legacyStorage: true }), events = [];
 demo.on('clipnest:panel', state => events.push(state));
 const initialReady = readyState(demo);
 await demo.call('State'); await initialReady; events.length = 0;
 const cleared = readyState(demo), loading = await demo.call('Clear');
 assert.equal(loading.loading, true);
 assert.equal(events.length, 0);
 const ready = await cleared;
 assert.equal(events.length, 1); assert.equal(ready.session, loading.session);
 events.length = 0;
 const migrated = readyState(demo);
 await demo.call('MigrateStorage'); await migrated;
 assert.deepEqual(events.map(state => state.loading), [true, false]);
 assert.equal(events[0].session, events[1].session);
});

test('clipboard permission demo keeps one-time consent separate from automatic recording', async () => {
 const demo = createDemo({ clipboardAccess: 'default' });
 assert.equal(await demo.call('ClipboardPermission'), 'default');
 const ready = readyState(demo); await demo.call('State');
 const before = await ready;
 const after = await demo.call('RequestClipboardPermission');
 assert.equal(after.clipboardAccess, 'ask');
 assert.equal(after.session, before.session);
 assert.deepEqual(after.entries, before.entries);
 assert.equal((await demo.call('RequestClipboardPermission')).clipboardAccess, 'ask');
 assert.equal((await demo.call('Pause', true)).clipboardAccess, 'ask');
 await assert.rejects(demo.call('RequestClipboardPermission'), /恢复记录/);
 const denied = createDemo({ clipboardAccess: 'denied' });
 await assert.rejects(denied.call('RequestClipboardPermission'), /拒绝/);
 assert.equal(await denied.call('ClipboardPermission'), 'denied');
 assert.equal(await denied.call('OpenClipboardPermission'), null);
});
