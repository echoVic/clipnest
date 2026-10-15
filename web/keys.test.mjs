import test from 'node:test';
import assert from 'node:assert/strict';
import { keyAction } from './keys.js';
const key = (k, mods = {}) => ({ key: k, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, repeat: false, isComposing: false, keyCode: 0, ...mods });
const ctx = (over = {}) => ({ demo: false, dialogOpen: false, busy: false, typing: false, atList: true, view: 'history', hasQuery: false, hasSelection: true, count: 10, ...over });
const type = (k, mods, over) => keyAction(key(k, mods), ctx(over))?.type ?? null;
test('existing keys behave the same on both pages', () => {
 for (const view of ['history', 'phrases']) {
  assert.deepEqual(keyAction(key('ArrowDown'), ctx({ view })), { type: 'select', delta: 1 });
  assert.deepEqual(keyAction(key('ArrowUp'), ctx({ view, typing: true })), { type: 'select', delta: -1 });
  assert.deepEqual(keyAction(key('3'), ctx({ view })), { type: 'number', index: 2 });
  assert.equal(type('3', {}, { view, typing: true }), null); assert.equal(type('3', { repeat: true }, { view }), null); assert.equal(type('9', {}, { view, count: 8 }), null);
  assert.equal(type('Enter', {}, { view, typing: true, atList: false }), 'choose'); assert.equal(type('Enter', {}, { view, atList: false }), null);
  assert.equal(type('Escape', {}, { view, hasQuery: true }), 'clear-search'); assert.equal(type('Escape', {}, { view }), 'dismiss');
  assert.equal(type('k', { metaKey: true }, { view, typing: true }), 'search'); assert.equal(type('K', { ctrlKey: true }, { view }), 'search');
  assert.equal(type('Enter', {}, { view }), 'choose');
  assert.equal(type('Escape', {}, { view, dialogOpen: true }), 'cancel-dialog'); assert.equal(type('k', { metaKey: true }, { view, dialogOpen: true }), null);
  assert.equal(type('Escape', {}, { view, busy: true }), 'dismiss'); assert.equal(type('k', { metaKey: true }, { view, busy: true }), 'search');
 }
 assert.equal(type('V', { metaKey: true, shiftKey: true }, { demo: true, dialogOpen: true }), 'reopen'); assert.equal(type('v', { metaKey: true, shiftKey: true }), null);
});
test('left and right switch pages only outside inputs', () => {
 assert.deepEqual(keyAction(key('ArrowRight'), ctx()), { type: 'view', view: 'phrases' });
 assert.deepEqual(keyAction(key('ArrowLeft'), ctx({ view: 'phrases' })), { type: 'view', view: 'history' });
 assert.equal(type('ArrowRight', {}, { typing: true }), null); assert.equal(type('ArrowRight', { shiftKey: true }), null);
});
test('Escape dismisses the innermost UI before the native panel', () => {
 assert.equal(type('Escape', {}, { dialogOpen: true, hasQuery: true, typing: true }), 'cancel-dialog');
 assert.equal(type('Escape', {}, { dialogOpen: true, busy: true }), 'cancel-dialog');
 assert.equal(type('Escape', {}, { hasQuery: true }), 'clear-search');
 assert.equal(type('Escape'), 'dismiss');
 assert.equal(type('Escape', { isComposing: true }, { dialogOpen: true }), null);
 assert.equal(type('Escape', { keyCode: 229 }), null);
});
test('reorder, new and edit belong to the phrases page', () => {
 const phrases = { view: 'phrases' };
 assert.deepEqual(keyAction(key('ArrowUp', { altKey: true }), ctx(phrases)), { type: 'reorder', delta: -1 });
 assert.deepEqual(keyAction(key('ArrowDown', { altKey: true, repeat: true }), ctx(phrases)), { type: 'reorder', delta: 1 });
 assert.equal(type('ArrowDown', { altKey: true }), null); assert.equal(type('ArrowDown', { altKey: true }, { ...phrases, hasQuery: true }), null); assert.equal(type('ArrowDown', { altKey: true }, { ...phrases, typing: true }), null);
 assert.equal(type('n', { metaKey: true }, phrases), 'new'); assert.equal(type('N', { ctrlKey: true }, { ...phrases, typing: true }), 'new'); assert.equal(type('n', { metaKey: true }), null);
 assert.equal(type('e', { metaKey: true }, phrases), 'edit'); assert.equal(type('e', { metaKey: true }, { ...phrases, hasSelection: false }), null); assert.equal(type('e', { metaKey: true }), null);
});
test('new keys reject extra modifiers', () => {
 const phrases = { view: 'phrases' };
 for (const k of ['n', 'e']) for (const mods of [{ shiftKey: true }, { altKey: true }]) assert.equal(type(k, { metaKey: true, ...mods }, phrases), null, `${k} with ${Object.keys(mods)}`);
 assert.equal(type('ArrowDown', { altKey: true, metaKey: true }, phrases), null); assert.equal(type('ArrowDown', { altKey: true, shiftKey: true }, phrases), null);
 assert.equal(type('ArrowRight', { metaKey: true }), null); assert.equal(type('ArrowRight', { altKey: true }), null);
 assert.equal(type('V', { shiftKey: true }, { demo: true }), null);
});
test('busy, open dialogs and IME composition block the keys', () => {
 const phrases = { view: 'phrases' };
 for (const [k, mods] of [['ArrowRight', {}], ['ArrowDown', { altKey: true }], ['n', { metaKey: true }], ['e', { metaKey: true }], ['ArrowDown', {}], ['1', {}], ['Enter', {}]]) {
  assert.equal(type(k, mods, { ...phrases, busy: true }), null, `${k} while busy`);
  assert.equal(type(k, mods, { ...phrases, dialogOpen: true }), null, `${k} with a dialog open`);
  assert.equal(type(k, { ...mods, isComposing: true }, phrases), null, `${k} while composing`);
 }
 assert.equal(type('ArrowLeft', { keyCode: 229 }, phrases), null);
});
