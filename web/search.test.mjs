import test from 'node:test';
import assert from 'node:assert/strict';

const deferred = () => {
  let resolve, reject;
  const promise = new Promise((a, b) => { resolve = a; reject = b; });
  return { promise, resolve, reject };
};
const page = (offset = 0, q = '') => ({
  offset, total: 100,
  entries: Array.from({ length: 50 }, (_, i) => ({
    id: `${q || 'item'}-${offset + i}`, kind: 'text', title: q || 'Item', excerpt: q || 'Item', created: 0,
  })),
});

class Element {
  constructor() {
    this.dataset = {}; this.style = {}; this.attributes = {}; this.listeners = {};
    this.classList = { add() {}, remove() {}, toggle() {} };
    this.value = ''; this.textContent = ''; this.children = []; this.clientHeight = 280; this.scrollTop = 0;
  }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children = nodes; }
  setAttribute(key, value) { this.attributes[key] = value; }
  removeAttribute(key) { delete this.attributes[key]; }
  addEventListener(event, fn) { this.listeners[event] = fn; }
  focus() {}
  matches() { return false; }
  close() { this.open = false; }
  showModal() { this.open = true; }
}

async function app(t) {
  const elements = new Map(), events = new Map(), calls = [], timers = new Map(), frames = new Map();
  let clock = 0, timerID = 0, frameID = 0, session = 1;
  const get = selector => {
    if (!elements.has(selector)) elements.set(selector, new Element());
    return elements.get(selector);
  };
  const filters = ['all', 'text', 'link'].map(kind => Object.assign(new Element(), { dataset: { kind } }));
  const views = ['history', 'phrases'].map(view => Object.assign(new Element(), { dataset: { view } }));
  const state = () => ({ session, loading: false, total: 100, entries: page().entries, accessible: true, platform: 'demo' });
  const bridge = {
    preferences: {},
    on: (event, fn) => events.set(event, fn),
    async call(method, ...args) {
      method = method.replace('Desktop.', '');
      calls.push({ method, args });
      if (bridge.handle) {
        const response = bridge.handle(method, args);
        if (response !== undefined) return response;
      }
      switch (method) {
        case 'State': return state();
        case 'Page': return page(args.at(-2), args[2]);
        case 'Detail': return { id: args[1], kind: 'text', title: 'Detail', text: 'Full detail' };
        case 'Phrases': return [{ id: 'phrase-exact', excerpt: 'Phrase' }];
        case 'PhraseDetail': return { id: args[0], text: 'Full phrase' };
        case 'Choose': case 'InsertPhrase': return { copied: false, pasted: false, message: 'Mock only' };
        default: return null;
      }
    },
  };
  const globals = {
    document: {
      body: new Element(), documentElement: new Element(),
      querySelector: selector => selector === 'dialog[open]' ? null : get(selector),
      querySelectorAll: selector => selector === '.filters button' ? filters : selector === '.views button' ? views : [],
      createElement: () => new Element(), createElementNS: () => new Element(),
      createTextNode: text => text, createDocumentFragment: () => new Element(), addEventListener() {},
    },
    window: { mygo: bridge }, location: { search: '' },
    localStorage: { getItem: () => null, setItem() {} },
    matchMedia: () => ({ matches: false, addEventListener() {} }),
    ResizeObserver: class { observe() {} },
    requestAnimationFrame: fn => { frames.set(++frameID, fn); return frameID; },
    setTimeout: (fn, delay) => { timers.set(++timerID, { fn, at: clock + delay }); return timerID; },
    clearTimeout: id => timers.delete(id),
  };
  const saved = new Map(Object.keys(globals).map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
  for (const [key, value] of Object.entries(globals)) Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
  t.after(() => {
    for (const [key, descriptor] of saved) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else delete globalThis[key];
    }
  });
  const settle = async () => {
    for (let i = 0; i < 12; i++) await Promise.resolve();
    const queued = [...frames.values()]; frames.clear(); queued.forEach(fn => fn());
    for (let i = 0; i < 12; i++) await Promise.resolve();
  };
  await import(`./app.js?search-test=${Math.random()}`);
  await settle();
  return {
    get, calls, bridge, filters, views, settle,
    async input(q) { get('#search').value = q; get('#search').listeners.input(); },
    async advance(ms) {
      clock += ms;
      for (const [id, timer] of [...timers]) if (timer.at <= clock) { timers.delete(id); timer.fn(); }
      await settle();
    },
    async reopen(loading = false) {
      session++; events.get('clipnest:panel')({ ...state(), loading }); await settle();
    },
    async ready() { events.get('clipnest:panel')(state()); await settle(); },
  };
}

test('input invalidates Search before debounce and propagates the latest revision to Page', async t => {
  const ui = await app(t);
  ui.calls.length = 0;
  await ui.input('o'); await ui.input('old'); await ui.input('latest');
  const searches = ui.calls.filter(c => c.method === 'Search');
  assert.equal(searches.length, 3);
  assert.ok(searches.every(c => c.args[0] === 1));
  assert.ok(searches[0].args[1] < searches[1].args[1] && searches[1].args[1] < searches[2].args[1]);
  await ui.advance(59);
  assert.equal(ui.calls.filter(c => c.method === 'Page').length, 0);
  await ui.advance(1);
  const read = ui.calls.find(c => c.method === 'Page');
  assert.deepEqual(read.args, [1, ui.calls.filter(c => c.method === 'Search').at(-1).args[1], 'latest', 'all', 0, 50]);
  assert.equal(ui.get('#paste').disabled, false);
});

test('filter, view, refresh, dismiss and reopen use noncolliding revisions without changing frozen IDs', async t => {
  const ui = await app(t);
  assert.ok(ui.calls.some(c => c.method === 'Search'));
  ui.filters[1].onclick(); await ui.settle();
  const filtered = ui.calls.filter(c => c.method === 'Page').at(-1);
  assert.equal(filtered.args[3], 'text');
  ui.views[1].onclick(); await ui.settle();
  await ui.get('#copy-only').onclick(); await ui.settle();
  assert.deepEqual(ui.calls.find(c => c.method === 'InsertPhrase').args, [1, 'phrase-exact', true]);
  ui.get('#clear-search').onclick(); await ui.settle();
  await ui.get('#dismiss').onclick();
  const dismissed = ui.calls.filter(c => c.method === 'Search').at(-1).args[1];
  await ui.reopen();
  const searches = ui.calls.filter(c => c.method === 'Search');
  assert.ok(searches.every((c, i) => !i || c.args[1] > searches[i - 1].args[1]));
  assert.ok(searches.at(-1).args[1] > dismissed);
  assert.equal(searches.at(-1).args[0], 2);
});

test('obsolete Search and Page failures do not block the newer results or detail preview', async t => {
  const ui = await app(t), oldSearch = deferred(), oldPage = deferred();
  ui.bridge.handle = (method, args) => {
    if (method === 'Search' && ui.get('#search').value === 'old') return oldSearch.promise;
    if (method === 'Page' && args[2] === 'old') return oldPage.promise;
  };
  await ui.input('old'); await ui.advance(60);
  assert.ok(ui.calls.some(c => c.method === 'Page' && c.args[2] === 'old'));
  await ui.input('latest'); await ui.advance(60);
  oldSearch.reject(Error('obsolete Search')); oldPage.reject(Error('cancelled old Page'));
  await ui.settle();
  assert.equal(ui.get('#paste').disabled, false);
  assert.equal(ui.get('#list').hidden, false);
  assert.notEqual(ui.get('#toast').textContent, 'obsolete Search');
  assert.equal(ui.get('#preview-body').children[0].textContent, 'Full detail');
  await ui.get('#copy-only').onclick(); await ui.settle();
  assert.deepEqual(ui.calls.find(c => c.method === 'Choose').args, [1, 'latest-0', true]);
});

test('reopen invalidates immediately while State is pending and prevents debounce from dispatching', async t => {
  const ui = await app(t), state = deferred();
  await ui.input('old');
  ui.bridge.handle = method => method === 'State' ? state.promise : undefined;
  const previous = ui.calls.filter(c => c.method === 'Search').at(-1).args[1];
  const reopen = ui.get('#reopen').onclick();
  assert.ok(ui.calls.filter(c => c.method === 'Search').at(-1).args[1] > previous);
  await ui.advance(60);
  assert.equal(ui.calls.filter(c => c.method === 'Page').length, 0);
  state.resolve({ session: 2, loading: false, total: 100, entries: page().entries, accessible: true, platform: 'demo' });
  await reopen; await ui.settle();
  assert.equal(ui.calls.filter(c => c.method === 'Search').at(-1).args[0], 2);
  assert.equal(ui.get('#paste').disabled, false);
});

test('a loading native panel waits for its reader before requesting Search', async t => {
  const ui = await app(t);
  ui.calls.length = 0;
  ui.bridge.handle = method => {
    if (method === 'Search') throw Error('历史正在加载，请稍后再试');
  };
  await ui.reopen(true);
  assert.equal(ui.calls.filter(c => c.method === 'Search').length, 0);
  assert.equal(ui.get('#paste').disabled, true);
  ui.bridge.handle = null;
  await ui.ready();
  assert.equal(ui.calls.filter(c => c.method === 'Search').length, 1);
  assert.equal(ui.get('#paste').disabled, false);
  assert.notEqual(ui.get('#toast').textContent, '历史正在加载，请稍后再试');
});
