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
 assert.deepEqual(after, { backend: 'sqlite', migrationAvailable: false, cleanupPending: false, retention: { maxMiB: 512, keepDays: 0 }, imageText: true });
 // Only the sample screenshot changes: its recognized text becomes the title.
 const migrated = (await readyAfter).entries;
 assert.deepEqual(migrated.map(e => e.id), before.entries.map(e => e.id));
 assert.deepEqual(migrated.filter(e => e.kind !== 'image'), before.entries.filter(e => e.kind !== 'image'));
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

test('demo exclusions add, update the same bundle ID, sort and return independent lists', async () => {
 const { call } = createDemo();
 assert.deepEqual(await call('ExcludedApps'), []);
 assert.deepEqual(await call('AddExcludedApp', 'sample-notes'), [{ bundleID: 'com.example.notes', name: '示例备忘录' }]);
 const apps = await call('AddExcludedApp', 'sample-browser');
 assert.deepEqual(apps.map(app => app.bundleID), ['com.example.browser', 'com.example.notes']);
 apps[0].name = 'changed outside the demo'; apps.push({ bundleID: 'other', name: 'other' });
 assert.deepEqual(await call('AddExcludedApp', 'sample-notes-renamed'), [
  { bundleID: 'com.example.browser', name: '示例浏览器' },
  { bundleID: 'com.example.notes', name: '示例备忘录（新名称）' },
 ]);
 const snapshot = await call('ExcludedApps'); snapshot[1].name = 'another external change';
 assert.equal((await call('ExcludedApps'))[1].name, '示例备忘录（新名称）');
 assert.deepEqual(await call('RemoveExcludedApp', 'com.example.browser'), [
  { bundleID: 'com.example.notes', name: '示例备忘录（新名称）' },
 ]);
 assert.deepEqual(await call('RemoveExcludedApp', 'com.example.notes'), []);
});

test('demo exclusions cancellation and invalid samples leave rules unchanged', async () => {
 const { call } = createDemo();
 const apps = await call('AddExcludedApp', 'sample-notes');
 assert.deepEqual(await call('AddExcludedApp', null), apps);
 assert.deepEqual(await call('AddExcludedApp'), apps);
 await assert.rejects(call('AddExcludedApp', '/Applications/Safari.app'), /示例/);
 assert.deepEqual(await call('ExcludedApps'), apps);
 assert.deepEqual(await call('RemoveExcludedApp', 'not-excluded'), apps);
});

test('demo exclusion changes preserve the active history session, search and phrases', async () => {
 const demo = createDemo({ large: true }), ready = readyState(demo), events = [];
 await demo.call('State'); const state = await ready;
 const phrases = await demo.call('Phrases', '');
 await demo.call('Search', state.session, 17);
 const page = await demo.call('Page', state.session, 17, '设计', 'all', 50, 50);
 const detail = await demo.call('Detail', state.session, page.entries[0].id);
 demo.on('clipnest:panel', event => events.push(event));
 await demo.call('AddExcludedApp', 'sample-notes');
 await demo.call('AddExcludedApp', 'sample-notes-renamed');
 await demo.call('AddExcludedApp', null);
 await demo.call('RemoveExcludedApp', 'com.example.notes');
 assert.deepEqual(events, [], 'exclusion management must not publish or rebuild a history session');
 assert.deepEqual(await demo.call('Page', state.session, 17, '设计', 'all', 50, 50), page);
 assert.deepEqual(await demo.call('Detail', state.session, detail.id), detail);
 await assert.rejects(demo.call('Search', state.session, 16), /搜索已过期/);
 assert.deepEqual(await demo.call('Phrases', ''), phrases);
 assert.deepEqual(await demo.call('StorageStatus'), state.storage);
 assert.equal((await demo.call('Choose', state.session, detail.id, true)).copied, false);
});

test('demo exclusions persist through reopening, migration, retention and clearing, but not a new demo instance', async () => {
 const demo = createDemo({ legacyStorage: true });
 const apps = await demo.call('AddExcludedApp', 'sample-notes');
 for (const [method, args] of [['State', []], ['MigrateStorage', []], ['SaveRetention', [{ maxMiB: 0, keepDays: 0 }]], ['Clear', []]]) {
  const ready = readyState(demo);
  await demo.call(method, ...args); await ready;
  assert.deepEqual(await demo.call('ExcludedApps'), apps, method);
 }
 assert.deepEqual(await createDemo().call('ExcludedApps'), []);
});

test('demo screenshot text is searchable, copyable without new history and follows the switch', async () => {
 const demo = createDemo(), ready = readyState(demo);
 await demo.call('State'); const state = await ready;
 assert.equal(state.storage.imageText, true);
 const image = state.entries.find(e => e.kind === 'image');
 assert.ok(image, 'demo has a sample screenshot');
 assert.equal(image.imageText, undefined, 'summaries omit recognized text');
 assert.equal(image.title, '截图里的文字，也能搜到');
 await demo.call('Search', state.session, 1);
 const found = await demo.call('Page', state.session, 1, '后台逐张识别', 'all', 0, 50);
 assert.deepEqual(found.entries.map(e => e.id), [image.id]);
 const detail = await demo.call('Detail', state.session, image.id);
 assert.equal(detail.imageTextState, 'ready');
 assert.match(detail.imageText, /后台逐张识别/);
 assert.match(detail.image, /^[A-Za-z0-9+/]+=*$/, 'the sample is a base64 PNG');
 const copied = await demo.call('CopyImageText', state.session, image.id);
 assert.equal(copied.copied, false); assert.match(copied.message, /演示/);
 await assert.rejects(demo.call('CopyImageText', state.session, state.entries.find(e => e.kind === 'text').id), /没有可复制的文字/);
 await assert.rejects(demo.call('CopyImageText', 0, image.id), /会话/);
 const offReady = readyState(demo);
 const off = await demo.call('SetImageText', false);
 assert.equal(off.storage.imageText, false);
 const reopened = await offReady;
 assert.equal(reopened.total, state.total, 'switching never adds or removes history');
 assert.equal(reopened.entries.find(e => e.id === image.id).title, '复制的图片');
 await demo.call('Search', reopened.session, 1);
 assert.equal((await demo.call('Page', reopened.session, 1, '后台逐张识别', 'all', 0, 50)).total, 0);
 const hidden = await demo.call('Detail', reopened.session, image.id);
 assert.equal(hidden.imageTextState, 'disabled'); assert.equal(hidden.imageText, undefined);
 await assert.rejects(demo.call('CopyImageText', reopened.session, image.id), /没有可复制的文字/);
 const onReady = readyState(demo);
 await demo.call('SetImageText', true);
 const restored = await onReady;
 assert.equal((await demo.call('Detail', restored.session, image.id)).imageTextState, 'ready');
});

test('legacy demo storage explains that screenshot text needs SQLite', async () => {
 const demo = createDemo({ legacyStorage: true }), ready = readyState(demo);
 await demo.call('State'); const state = await ready;
 const image = state.entries.find(e => e.kind === 'image');
 assert.equal((await demo.call('Detail', state.session, image.id)).imageTextState, 'needsSQLite');
 await assert.rejects(demo.call('SetImageText', true), /迁移/);
});
