import test from 'node:test';
import assert from 'node:assert/strict';
import * as preferences from './preferences.js';

// Only the browser DOM boundary is stubbed; clicks use the real controller.
class Element {
  constructor(tag = 'div', document) {
    this.tagName = tag; this.ownerDocument = document; this.children = [];
    this.attributes = new Map(); this.listeners = new Map();
    this.disabled = false; this.hidden = false; this.open = false; this.value = '';
  }
  set textContent(text) { this.text = String(text); this.children = []; }
  get textContent() { return (this.text || '') + this.children.map(child => child.textContent).join(''); }
  set innerHTML(_) { throw Error('app metadata must never be parsed as HTML'); }
  setAttribute(key, value) { this.attributes.set(key, String(value)); }
  getAttribute(key) { return this.attributes.get(key) ?? null; }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.text = ''; this.children = []; this.append(...children); }
  querySelectorAll(selector) {
    return this.children.flatMap(child => [...(child.tagName === selector ? [child] : []), ...child.querySelectorAll(selector)]);
  }
  addEventListener(type, listener) { this.listeners.set(type, listener); }
  async click() { if (!this.disabled) return (this.onclick || this.listeners.get('click'))?.({ target: this }); }
  focus() { this.ownerDocument.activeElement = this; }
  showModal() { assert.equal(this.open, false); this.open = true; }
  close() { this.open = false; }
}

function fixture(call, demo = true) {
  const document = { activeElement: null };
  document.createElement = tag => new Element(tag, document);
  const elements = Object.fromEntries([
    'excluded-apps-group', 'excluded-apps-list', 'excluded-apps-empty', 'excluded-apps-error',
    'excluded-apps-note', 'excluded-apps-add', 'excluded-apps-retry', 'excluded-app-picker',
    'excluded-app-sample', 'excluded-app-confirm', 'excluded-app-cancel',
  ].map(id => [id, document.createElement(id === 'excluded-app-sample' ? 'select' : 'div')]));
  document.querySelector = selector => {
    assert.ok(elements[selector.slice(1)], `unexpected access outside exclusions: ${selector}`);
    return elements[selector.slice(1)];
  };
  assert.equal(typeof preferences.preferencesExclusions, 'function', 'exclusion preferences controller is missing');
  const controller = preferences.preferencesExclusions(document, { call, demo });
  return { controller, elements, document };
}
const notes = { bundleID: 'com.example.notes', name: '示例备忘录' };
const rowButtons = elements => elements['excluded-apps-list'].querySelectorAll('button');
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };

test('exclusion preferences render backend names and bundle IDs as text and remove by ID', async () => {
  const calls = [], app = { bundleID: 'com.example.long_identifier', name: '<img src=x onerror=alert(1)>' };
  const { controller, elements, document } = fixture(async (method, ...args) => {
    calls.push([method, ...args]); return method === 'ExcludedApps' ? [app] : [];
  }, false);
  await controller.refresh();
  const list = elements['excluded-apps-list'];
  assert.ok(list.textContent.includes(app.name));
  assert.ok(list.textContent.includes(app.bundleID));
  const remove = rowButtons(elements)[0];
  assert.equal(remove.type, 'button');
  assert.equal(remove.getAttribute('aria-label'), `移除 ${app.name}`);
  await remove.click();
  assert.deepEqual(calls, [['ExcludedApps'], ['RemoveExcludedApp', app.bundleID]]);
  assert.equal(list.children.length, 0);
  assert.equal(elements['excluded-apps-empty'].hidden, false);
  assert.equal(document.activeElement, elements['excluded-apps-add']);
});

test('browser exclusions use a labelled sample selector, cancel without mutation and add only the chosen sample ID', async () => {
  const calls = [];
  const { controller, elements } = fixture(async (method, ...args) => {
    calls.push([method, ...args]); return method === 'AddExcludedApp' ? [notes] : [];
  });
  await controller.refresh();
  assert.match(elements['excluded-apps-note'].textContent, /示例/);
  await elements['excluded-apps-add'].click();
  assert.equal(elements['excluded-app-picker'].open, true);
  assert.ok(elements['excluded-app-sample'].children.some(option => option.value === 'sample-notes'));
  await elements['excluded-app-cancel'].click();
  assert.equal(elements['excluded-app-picker'].open, false);
  assert.deepEqual(calls, [['ExcludedApps']]);
  await elements['excluded-apps-add'].click();
  elements['excluded-app-sample'].value = 'sample-notes';
  await elements['excluded-app-confirm'].click();
  assert.equal(elements['excluded-app-picker'].open, false);
  assert.deepEqual(calls, [['ExcludedApps'], ['AddExcludedApp', 'sample-notes']]);
  assert.ok(elements['excluded-apps-list'].textContent.includes(notes.name));
});

test('native exclusion add invokes the app chooser API without a sample argument', async () => {
  const calls = [];
  const { controller, elements } = fixture(async (method, ...args) => {
    calls.push([method, ...args]); return method === 'AddExcludedApp' ? [notes] : [];
  }, false);
  await controller.refresh();
  await elements['excluded-apps-add'].click();
  assert.equal(elements['excluded-app-picker'].open, false);
  assert.deepEqual(calls, [['ExcludedApps'], ['AddExcludedApp']]);
});

test('exclusion mutation errors refresh authoritative rules, unlock controls and allow retry', async () => {
  const pending = deferred(), calls = [];
  let attempts = 0, saved = [];
  const { controller, elements } = fixture(async (method, ...args) => {
    calls.push([method, ...args]);
    if (method === 'ExcludedApps') return saved;
    if (++attempts === 1) { saved = [notes]; return pending.promise; }
    return saved;
  }, false);
  await controller.refresh();
  const adding = elements['excluded-apps-add'].click();
  assert.equal(elements['excluded-apps-add'].disabled, true);
  assert.equal(elements['excluded-apps-group'].getAttribute('aria-busy'), 'true');
  await elements['excluded-apps-add'].click();
  pending.reject(Error('directory sync failed'));
  await adding;
  assert.match(elements['excluded-apps-error'].textContent, /directory sync failed/);
  assert.equal(elements['excluded-apps-error'].hidden, false);
  assert.ok(elements['excluded-apps-list'].textContent.includes(notes.name), 'published rules must not look rolled back');
  assert.equal(elements['excluded-apps-add'].disabled, false);
  assert.equal(rowButtons(elements)[0].disabled, false);
  assert.equal(elements['excluded-apps-group'].getAttribute('aria-busy'), 'false');
  await elements['excluded-apps-add'].click();
  assert.equal(elements['excluded-apps-error'].hidden, true);
  assert.equal(attempts, 2);
  assert.deepEqual(calls, [['ExcludedApps'], ['AddExcludedApp'], ['ExcludedApps'], ['AddExcludedApp']]);
});

test('failed exclusions loading exposes retry and does not pretend the list is empty', async () => {
  let fail = true;
  const { controller, elements } = fixture(async () => {
    if (fail) throw Error('cannot read rules');
    return [notes];
  });
  await controller.refresh();
  assert.equal(elements['excluded-apps-add'].disabled, true);
  assert.equal(elements['excluded-apps-empty'].hidden, true);
  assert.equal(elements['excluded-apps-retry'].hidden, false);
  assert.match(elements['excluded-apps-error'].textContent, /cannot read rules/);
  fail = false;
  await elements['excluded-apps-retry'].click();
  assert.equal(elements['excluded-apps-add'].disabled, false);
  assert.equal(elements['excluded-apps-error'].hidden, true);
  assert.equal(elements['excluded-apps-retry'].hidden, true);
  assert.ok(elements['excluded-apps-list'].textContent.includes(notes.name));
});

test('remove failures preserve rules and recover busy state without touching any other preferences', async () => {
  let fail = true;
  const { controller, elements } = fixture(async method => {
    if (method === 'ExcludedApps') return [notes];
    if (fail) throw Error('save failed');
    return [];
  });
  await controller.refresh();
  await rowButtons(elements)[0].click();
  assert.match(elements['excluded-apps-error'].textContent, /save failed/);
  assert.equal(elements['excluded-apps-add'].disabled, false);
  assert.equal(rowButtons(elements)[0].disabled, false);
  fail = false;
  await rowButtons(elements)[0].click();
  assert.equal(elements['excluded-apps-list'].children.length, 0);
  assert.equal(elements['excluded-apps-error'].hidden, true);
});
