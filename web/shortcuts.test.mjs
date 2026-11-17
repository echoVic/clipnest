import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { keyAction } from './keys.js';
import { entryActions } from './actions.js';
import { ctrlKeys, shortcutKeys, renderShortcutGroups } from './shortcuts.js';

// The native preferences window and the demo both read this file.
const groups = JSON.parse(readFileSync(new URL('./shortcuts.json', import.meta.url), 'utf8'));
const actionsOf = item => [].concat(item.action ?? []);
const viewsOf = group => group.view ? [group.view] : ['history', 'phrases'];
const names = { '↩': 'Enter', '⌫': 'Backspace', '↑': 'ArrowUp', '↓': 'ArrowDown', '←': 'ArrowLeft', '→': 'ArrowRight', Esc: 'Escape', Delete: 'Delete' };

// "⌘ ↩" is one chord; "1–9" stands for each digit, so its ends are tried.
function events(combo) {
  const parts = combo.split(' '), name = parts.pop();
  const mods = { metaKey: parts.includes('⌘'), altKey: parts.includes('⌥'), shiftKey: parts.includes('⇧') };
  assert.equal(parts.filter(part => !'⌘⌥⇧'.includes(part)).length, 0, `unknown modifier in ${combo}`);
  const keys = name === '1–9' ? ['1', '9'] : [names[name] ?? name.toLowerCase()];
  return keys.map(key => ({ key, ctrlKey: false, repeat: false, isComposing: false, keyCode: 0, defaultPrevented: false, ...mods }));
}

// Every state a key can meet in the panel: focus in the list or the search
// box, with or without a query, inside the action menu or a dialog.
function contexts(view, context) {
  return [false, true].flatMap(typing => [false, true].map(hasQuery => ({
    view, typing, atList: !typing, hasQuery, hasSelection: true, count: 9, busy: false, demo: false,
    menuOpen: context === 'menu', dialogOpen: context === 'dialog',
  })));
}

const triggered = (combo, view, context) =>
  new Set(events(combo).flatMap(event => contexts(view, context).map(ctx => keyAction(event, ctx)?.type).filter(Boolean)));

test('every listed shortcut does what the list says', () => {
  let checked = 0;
  for (const group of groups) for (const item of group.items) {
    const want = actionsOf(item);
    if (!want.length) continue;
    for (const view of viewsOf(group)) {
      const seen = new Set();
      for (const combo of [...item.keys, ...(item.other ?? [])]) {
        const got = triggered(combo, view, group.context);
        assert.ok(got.size, `${combo}（${item.label}）does nothing on ${view}`);
        for (const type of got) assert.ok(want.includes(type), `${combo} on ${view} triggers ${type}, listed as ${want}`);
        got.forEach(type => seen.add(type));
        checked++;
      }
      assert.deepEqual([...seen].sort(), [...want].sort(), `${item.label} on ${view}`);
    }
  }
  assert.ok(checked > 20);
});

test('every action a key can trigger is in the list', () => {
  const keys = [...'abcdefghijklmnopqrstuvwxyz0123456789,./;[]\'`-= ', 'Enter', 'Escape', 'Backspace', 'Delete', 'Tab', 'Home', 'End', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'];
  for (const context of [undefined, 'menu', 'dialog']) for (const view of ['history', 'phrases']) {
    const listed = new Set(groups.filter(group => group.context === context && viewsOf(group).includes(view)).flatMap(group => group.items.flatMap(actionsOf)));
    for (const key of keys) for (let mods = 0; mods < 8; mods++) for (const ctx of contexts(view, context)) {
      const event = { key, metaKey: !!(mods & 1), altKey: !!(mods & 2), shiftKey: !!(mods & 4), ctrlKey: false, repeat: false, isComposing: false, keyCode: 0, defaultPrevented: false };
      const type = keyAction(event, ctx)?.type;
      if (type) assert.ok(listed.has(type), `${JSON.stringify(event)} triggers ${type} on ${view}${context ? ` in ${context}` : ''}, which the list omits`);
    }
  }
});

test('the action menu shows the same keys as the list', () => {
  const items = groups.filter(group => !group.context).flatMap(group => group.items);
  for (const ctrl of [false, true]) {
    const menus = [
      entryActions({ view: 'history', kind: 'link', imageText: false, index: 0, total: 3, hasQuery: false, ctrl }),
      entryActions({ view: 'phrases', kind: 'text', imageText: false, index: 1, total: 3, hasQuery: false, ctrl }),
    ];
    for (const action of menus.flat().filter(action => action.shortcut)) {
      const item = items.find(item => actionsOf(item).includes(action.type));
      assert.ok(item, `${action.label} is missing from the list`);
      assert.ok(shortcutKeys(item, ctrl).includes(action.shortcut), `${action.label}: menu shows ${action.shortcut}, list shows ${shortcutKeys(item, ctrl)}`);
    }
  }
});

test('shortcut keys follow the platform', () => {
  assert.deepEqual(shortcutKeys({ keys: ['⌘ ↩'] }, false), ['⌘ ↩']);
  assert.deepEqual(shortcutKeys({ keys: ['⌘ ↩'] }, true), ['Ctrl ↩']);
  assert.deepEqual(shortcutKeys({ keys: ['⌥ ↑', '⌥ ↓'] }, true), ['Alt ↑', 'Alt ↓']);
  assert.deepEqual(shortcutKeys({ keys: ['⌘ ⌫'], other: ['Delete'] }, false), ['⌘ ⌫']);
  assert.deepEqual(shortcutKeys({ keys: ['⌘ ⌫'], other: ['Delete'] }, true), ['Delete']);
  assert.equal(ctrlKeys('⌘ ⇧ V'), 'Ctrl Shift V');
  assert.equal(ctrlKeys('⌘K'), 'Ctrl K');
});

test('the list renders as text, one row per shortcut', () => {
  const document = {
    createElement: tag => ({
      tagName: tag, children: [], className: '', text: '',
      set textContent(text) { this.text = String(text); this.children = []; },
      get textContent() { return this.text + this.children.map(child => child.textContent).join(' '); },
      set innerHTML(_) { throw Error('shortcut labels must never be parsed as HTML'); },
      append(...children) { this.children.push(...children); },
      replaceChildren(...children) { this.text = ''; this.children = [...children]; },
    }),
  };
  const container = document.createElement('div');
  container.ownerDocument = document;
  renderShortcutGroups(container, [{ group: '历史', view: 'history', items: [{ label: '存为常用语', note: '图片不能存', keys: ['⌘ S'] }, { label: '删除', keys: ['⌘ ⌫'], other: ['Delete'] }] }], true);
  const [section] = container.children;
  assert.equal(section.children[0].textContent, '历史');
  assert.equal(section.children.length, 3);
  assert.match(container.textContent, /存为常用语 图片不能存 Ctrl S/);
  assert.match(container.textContent, /删除 Delete/);
  assert.doesNotMatch(container.textContent, /⌘/);
});
