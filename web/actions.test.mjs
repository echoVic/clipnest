import test from 'node:test';
import assert from 'node:assert/strict';
import { entryActions, sequenceBanner } from './actions.js';
test('entry pin actions describe the selected history item', () => {
 const action = ctx => entryActions({ view: 'history', kind: 'image', index: 0, total: 1, ...ctx }).find(a => a.type === 'pin-entry');
 assert.equal(action({}).label, '置顶这条');
 assert.equal(action({ entryPinned: true }).label, '取消置顶');
 assert.equal(action({ view: 'phrases' }), undefined);
});
const additionalHistoryActions = ['pin-entry', 'convert-case', 'merge-text'];
const types = ctx => entryActions({ view: 'history', kind: 'text', imageText: false, index: 0, total: 3, hasQuery: false, ctrl: false, ...ctx }).map(a => a.type).filter(type => !additionalHistoryActions.includes(type));
test('text actions respect the history item type and privacy', () => {
 const actions = ctx => entryActions({ view: 'history', kind: 'text', total: 2, ...ctx });
 assert.deepEqual(actions({}).filter(a => a.type === 'convert-case').map(a => a.mode), ['upper', 'lower']);
 assert.ok(actions({ kind: 'link' }).some(a => a.type === 'merge-text'));
 for (const ctx of [{ secret: true }, { kind: 'image' }, { view: 'phrases' }]) {
  assert.ok(!actions(ctx).some(a => a.type === 'convert-case' || a.type === 'merge-text'));
 }
});
test('history actions follow the entry kind', () => {
 assert.deepEqual(types({}), ['choose', 'copy', 'save-phrase', 'delete']);
 assert.deepEqual(types({ kind: 'code' }), ['choose', 'copy', 'save-phrase', 'delete']);
 assert.deepEqual(types({ kind: 'link' }), ['choose', 'copy', 'save-phrase', 'open-link', 'delete']);
 assert.deepEqual(types({ kind: 'image' }), ['choose', 'copy', 'delete']);
 assert.deepEqual(types({ kind: 'image', imageText: true }), ['choose', 'copy', 'image-text', 'delete']);
});
test('a masked secret cannot become a phrase', () => {
 assert.deepEqual(types({ secret: true }), ['choose', 'copy', 'delete']);
 assert.deepEqual(types({ kind: 'link', secret: true }), ['choose', 'copy', 'open-link', 'delete']);
});
test('phrase actions hide moves that cannot happen', () => {
 const phrases = { view: 'phrases' };
 assert.deepEqual(entryActions({ ...phrases, index: 1, total: 3 }).filter(a => a.type === 'reorder').map(a => a.delta), [-1, 1]);
 assert.deepEqual(types({ ...phrases, index: 0 }), ['choose', 'copy', 'edit', 'reorder', 'delete']);
 assert.equal(entryActions({ ...phrases, index: 0, total: 3 }).find(a => a.type === 'reorder').delta, 1);
 assert.equal(entryActions({ ...phrases, index: 2, total: 3 }).find(a => a.type === 'reorder').delta, -1);
 assert.deepEqual(types({ ...phrases, index: 1, hasQuery: true }), ['choose', 'copy', 'edit', 'delete']);
 assert.deepEqual(types({ ...phrases, total: 1 }), ['choose', 'copy', 'edit', 'delete']);
});
test('only delete is destructive and it comes last', () => {
 for (const ctx of [{}, { kind: 'link' }, { view: 'phrases', index: 1 }]) {
  const actions = entryActions({ view: 'history', kind: 'text', index: 0, total: 3, ...ctx });
  assert.deepEqual(actions.filter(a => a.danger).map(a => a.type), ['delete']); assert.equal(actions.at(-1).type, 'delete');
 }
 // Deleting a phrase asks first, so its label says so.
 assert.equal(entryActions({ view: 'phrases', index: 0, total: 1 }).at(-1).label, '删除…');
 assert.equal(entryActions({ view: 'history', kind: 'text', index: 0, total: 1 }).at(-1).label, '删除');
});
test('shortcut hints match the platform', () => {
 const hints = ctrl => Object.fromEntries(entryActions({ view: 'history', kind: 'link', index: 0, total: 1, ctrl }).filter(a => a.type !== 'pin-entry').map(a => [a.type, a.shortcut]));
 assert.deepEqual(hints(false), { choose: '↩', copy: '⌘ ↩', 'save-phrase': '⌘ S', 'open-link': '⌘ O', delete: '⌘ ⌫' });
 assert.deepEqual(hints(true), { choose: '↩', copy: 'Ctrl ↩', 'save-phrase': 'Ctrl S', 'open-link': 'Ctrl O', delete: 'Delete' });
 const phrase = ctrl => Object.fromEntries(entryActions({ view: 'phrases', index: 1, total: 3, ctrl }).map(a => [`${a.type}${a.delta ?? ''}`, a.shortcut]));
 assert.deepEqual(phrase(false), { choose: '↩', copy: '⌘ ↩', edit: '⌘ E', 'reorder-1': '⌥ ↑', reorder1: '⌥ ↓', delete: '⌘ ⌫' });
 assert.deepEqual(phrase(true), { choose: '↩', copy: 'Ctrl ↩', edit: 'Ctrl E', 'reorder-1': 'Alt ↑', reorder1: 'Alt ↓', delete: 'Delete' });
});
test('sequential paste is available even when an older item is pinned above newer entries', () => {
 const sequence = ctx => entryActions({ view: 'history', kind: 'text', index: 2, total: 5, ctrl: false, sequence: 3, ...ctx }).find(a => a.type === 'sequence');
 assert.deepEqual(sequence({}), { type: 'sequence', label: '从这条起顺序粘贴（3 条）', shortcut: '' });
 assert.deepEqual(types({ index: 2, sequence: 3 }), ['choose', 'copy', 'sequence', 'save-phrase', 'delete']);
 assert.equal(sequence({ index: 1, kind: 'image', sequence: 2 }).label, '从这条起顺序粘贴（2 条）', 'images can be queued');
 assert.ok(sequence({ index: 0, entryPinned: true }));
 assert.equal(sequence({ index: 0, total: 1, sequence: 1 }), undefined);
 assert.equal(sequence({ sequence: false }), undefined, 'platforms without auto-paste');
 assert.equal(sequence({ view: 'phrases' }), undefined);
});
test('the sequence banner names the paste keys of the platform', () => {
  assert.equal(sequenceBanner(3, false), '正在顺序粘贴，剩 3 条，每次 ⌘V 粘贴下一条');
  assert.equal(sequenceBanner(1, true), '正在顺序粘贴，剩 1 条，每次 Ctrl+V 粘贴下一条');
  assert.equal(sequenceBanner(0, false), '', 'no banner once the sequence ends');
  assert.equal(sequenceBanner(undefined, false), '', 'an older state without the field shows nothing');
});
