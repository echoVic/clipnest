import test from 'node:test';
import assert from 'node:assert/strict';
import { keyAction } from './keys.js';
const key = (k, mods = {}) => ({ key: k, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, repeat: false, isComposing: false, keyCode: 0, ...mods });
const ctx = (over = {}) => ({ demo: false, dialogOpen: false, busy: false, typing: false, atList: true, view: 'history', hasQuery: false, hasSelection: true, menuOpen: false, count: 10, ...over });
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
test('command comma opens preferences from either page', () => {
 for (const view of ['history', 'phrases']) {
  assert.equal(type(',', { metaKey: true }, { view }), 'settings'); assert.equal(type(',', { ctrlKey: true }, { view, typing: true }), 'settings');
  assert.equal(type(',', {}, { view }), null); assert.equal(type(',', { metaKey: true }, { view, dialogOpen: true }), null);
 }
});
test('copy only works on both pages, like Enter', () => {
 for (const view of ['history', 'phrases']) {
  assert.equal(type('Enter', { metaKey: true }, { view }), 'copy'); assert.equal(type('Enter', { ctrlKey: true }, { view, typing: true, atList: false }), 'copy');
  assert.equal(type('Enter', { metaKey: true }, { view, atList: false }), null); assert.equal(type('Enter', { metaKey: true }, { view, hasSelection: false }), null);
 }
});
test('save as phrase and open link belong to the history page', () => {
 for (const [k, action] of [['s', 'save-phrase'], ['o', 'open-link']]) {
  assert.equal(type(k, { metaKey: true }), action); assert.equal(type(k.toUpperCase(), { ctrlKey: true }, { typing: true }), action);
  assert.equal(type(k, { metaKey: true }, { view: 'phrases' }), null); assert.equal(type(k, { metaKey: true }, { hasSelection: false }), null); assert.equal(type(k), null);
 }
});
test('delete needs the list focus and ignores key repeat', () => {
 for (const view of ['history', 'phrases']) {
  assert.equal(type('Backspace', { metaKey: true }, { view }), 'delete'); assert.equal(type('Backspace', { ctrlKey: true }, { view }), 'delete'); assert.equal(type('Delete', {}, { view }), 'delete');
  assert.equal(type('Backspace', {}, { view }), null); assert.equal(type('Delete', { metaKey: true }, { view }), null);
  assert.equal(type('Backspace', { metaKey: true }, { view, typing: true }), null); assert.equal(type('Delete', {}, { view, typing: true }), null);
  assert.equal(type('Backspace', { metaKey: true, repeat: true }, { view }), null); assert.equal(type('Delete', { repeat: true }, { view }), null);
  assert.equal(type('Delete', {}, { view, hasSelection: false }), null);
 }
});
test('command slash toggles the action menu', () => {
 for (const view of ['history', 'phrases']) {
  assert.equal(type('/', { metaKey: true }, { view }), 'menu'); assert.equal(type('/', { ctrlKey: true }, { view, typing: true }), 'menu');
  // German and French layouts type a slash with Shift.
  assert.equal(type('/', { metaKey: true, shiftKey: true }, { view }), 'menu');
  assert.equal(type('/', { metaKey: true, altKey: true }, { view }), null); assert.equal(type('/', {}, { view }), null); assert.equal(type('/', { metaKey: true }, { view, hasSelection: false }), null);
 }
 const open = { dialogOpen: true, menuOpen: true };
 assert.equal(type('/', { metaKey: true }, open), 'menu'); assert.equal(type('Escape', {}, open), 'cancel-dialog');
 assert.deepEqual(keyAction(key('ArrowDown'), ctx(open)), { type: 'menu-move', delta: 1 }); assert.deepEqual(keyAction(key('ArrowUp'), ctx(open)), { type: 'menu-move', delta: -1 });
 // Enter and Space activate the focused item natively; other panel keys wait.
 for (const [k, mods] of [['Enter', {}], [' ', {}], ['s', { metaKey: true }], ['Delete', {}], ['1', {}], ['ArrowDown', { altKey: true }]]) assert.equal(type(k, mods, open), null, `${k} with the menu open`);
 assert.equal(type('/', { metaKey: true }, { dialogOpen: true }), null);
});
test('keys a focused control already handled are left alone', () => {
 // The editor saves on ⌘↩ and may close before the event reaches the panel.
 for (const [k, mods] of [['Enter', { metaKey: true }], ['Escape', {}], ['ArrowDown', {}], ['1', {}], ['/', { metaKey: true }]]) assert.equal(type(k, { ...mods, defaultPrevented: true }), null, k);
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
 for (const k of ['n', 'e', ',']) for (const mods of [{ shiftKey: true }, { altKey: true }]) assert.equal(type(k, { metaKey: true, ...mods }, phrases), null, `${k} with ${Object.keys(mods)}`);
 for (const k of ['s', 'o', 'Enter', 'Backspace']) for (const mods of [{ shiftKey: true }, { altKey: true }]) assert.equal(type(k, { metaKey: true, ...mods }), null, `${k} with ${Object.keys(mods)}`);
 assert.equal(type('Delete', { shiftKey: true }), null); assert.equal(type('Delete', { altKey: true }), null);
 assert.equal(type('ArrowDown', { altKey: true, metaKey: true }, phrases), null); assert.equal(type('ArrowDown', { altKey: true, shiftKey: true }, phrases), null);
 assert.equal(type('ArrowRight', { metaKey: true }), null); assert.equal(type('ArrowRight', { altKey: true }), null);
 assert.equal(type('V', { shiftKey: true }, { demo: true }), null);
});
test('busy, open dialogs and IME composition block the keys', () => {
 const phrases = { view: 'phrases' };
 for (const [k, mods] of [['ArrowRight', {}], ['ArrowDown', { altKey: true }], ['n', { metaKey: true }], ['e', { metaKey: true }], [',', { metaKey: true }], ['ArrowDown', {}], ['1', {}], ['Enter', {}]]) {
  assert.equal(type(k, mods, { ...phrases, busy: true }), null, `${k} while busy`);
  assert.equal(type(k, mods, { ...phrases, dialogOpen: true }), null, `${k} with a dialog open`);
  assert.equal(type(k, { ...mods, isComposing: true }, phrases), null, `${k} while composing`);
 }
 assert.equal(type('ArrowLeft', { keyCode: 229 }, phrases), null);
 for (const [k, mods] of [['s', { metaKey: true }], ['o', { metaKey: true }], ['Enter', { metaKey: true }], ['Backspace', { metaKey: true }], ['Delete', {}], ['/', { metaKey: true }]]) {
  assert.equal(type(k, mods, { busy: true }), null, `${k} while busy`);
  assert.equal(type(k, mods, { dialogOpen: true }), null, `${k} with a dialog open`);
  assert.equal(type(k, { ...mods, isComposing: true }), null, `${k} while composing`);
 }
});
