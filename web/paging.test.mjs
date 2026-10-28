import test from 'node:test';
import assert from 'node:assert/strict';
import * as ui from './responsiveness.js';

const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };
const context = (session = 1, q = '', revision = 1) => ({ session, revision, q, kind: 'all', view: 'history' });
const page = (offset, total = 10000, prefix = 'item') => ({
  offset, total, entries: Array.from({ length: Math.min(50, Math.max(0, total - offset)) }, (_, i) => ({ id: `${prefix}-${offset + i}` })),
});

test('paged metadata uses 50-row reads and an eight-page LRU, never an accumulating array', async () => {
  const calls = [];
  const model = new ui.PagedList(async (ctx, offset, limit) => { calls.push([ctx, offset, limit]); return page(offset); });
  model.reset(context());
  for (let index = 0; index < 10000; index += 50) {
    await model.ensure(index);
    assert.ok(model.pages.size <= 8);
    assert.ok([...model.pages.values()].reduce((n, p) => n + p.entries.length, 0) <= 400);
  }
  assert.equal(calls.length, 200);
  assert.ok(calls.every(([, offset, limit]) => offset % 50 === 0 && limit === 50));
  assert.equal(model.entry(0), undefined);
  await model.ensure(0);
  assert.equal(model.entry(0).id, 'item-0');
  assert.equal(model.pages.size, 8);
});

test('pending reads coalesce and are bounded during rapid scrolling', async () => {
  const pending = [];
  const model = new ui.PagedList((ctx, offset) => { const d = deferred(); pending.push([offset, d]); return d.promise; });
  model.reset(context());
  const first = model.ensure(0);
  assert.equal(model.ensure(1), first);
  const requests = Array.from({ length: 100 }, (_, i) => model.ensure(i * 50));
  await Promise.resolve();
  assert.ok(pending.length <= 8);
  for (const [offset, d] of pending) d.resolve(page(offset));
  await Promise.all([first, ...requests]);
  assert.ok(model.pages.size <= 8);
});

test('obsolete queued reads never dispatch and free the bounded slots for the latest revision', async () => {
  const calls = [];
  const model = new ui.PagedList(async (ctx, offset) => { calls.push(ctx.revision); return page(offset); });
  model.reset(context(1, 'old', 1));
  const old = Array.from({ length: 8 }, (_, i) => model.ensure(i * 50));
  model.reset(context(1, 'latest', 2));
  await Promise.all(old);
  assert.deepEqual(calls, []);
  await model.ensure(0);
  assert.deepEqual(calls, [2]);
  assert.equal((await model.pick()).revision, 2);
  assert.equal(model.pending.size, 0);
});

test('dispatched stale reads keep the eight-slot limit until cancellation frees newer pages', async () => {
  const pending = [], calls = [];
  const model = new ui.PagedList((ctx, offset) => {
    calls.push(ctx.revision);
    if (ctx.revision === 2) return page(offset, 100, 'latest');
    const d = deferred(); pending.push(d); return d.promise;
  });
  model.reset(context(1, 'old', 1));
  const old = Array.from({ length: 8 }, (_, i) => model.ensure(i * 50));
  await Promise.resolve();
  assert.equal(pending.length, 8);
  model.reset(context(1, 'latest', 2));
  assert.equal(await model.ensure(0), null);
  assert.equal(model.pending.size, 8);
  assert.deepEqual(calls, Array(8).fill(1));
  pending.forEach(d => d.reject(Error('cancelled')));
  await Promise.all(old);
  assert.equal(model.error, null);
  await model.ensure(0); await model.ensure(50);
  assert.deepEqual(calls.slice(-2), [2, 2]);
  assert.equal(model.entry(50).id, 'latest-50');
  assert.equal(model.pending.size, 0);
});

test('session and query changes reject stale successful and failed reads', async () => {
  const old = deferred(), current = deferred();
  const model = new ui.PagedList(ctx => ctx.session === 1 ? old.promise : current.promise);
  model.reset(context(1));
  const a = model.ensure(0);
  await Promise.resolve();
  model.reset(context(2, 'new'));
  const b = model.ensure(0);
  old.resolve(page(0, 10000, 'old'));
  assert.equal(await a, null);
  assert.equal(model.entry(0), undefined);
  current.resolve(page(0, 2, 'new'));
  await b;
  assert.equal(model.total, 2);
  assert.equal(model.entry(0).id, 'new-0');
  const failed = deferred();
  model.load = () => failed.promise;
  const c = model.ensure(50);
  await Promise.resolve();
  model.reset(context(2, 'another'));
  failed.reject(Error('stale query'));
  await c;
  assert.equal(model.error, null);
});

test('a failed page blocks even cached choices until explicit retry succeeds', async () => {
  let fail = false;
  const model = new ui.PagedList(async (ctx, offset) => { if (fail) throw Error('read failed'); return page(offset); });
  model.reset(context());
  await model.ensure(0);
  model.select(4);
  assert.equal((await model.pick()).id, 'item-4');
  fail = true;
  await model.ensure(50);
  assert.match(model.error.message, /read failed/);
  assert.equal(await model.pick(), null);
  fail = false;
  await model.retry();
  assert.equal(model.error, null);
  assert.equal((await model.pick()).id, 'item-4');
});

test('cached choices remain blocked while a retry is in flight', async () => {
  const retry = deferred();
  let attempts = 0;
  const model = new ui.PagedList(async (ctx, offset) => {
    if (!offset) return page(offset);
    if (++attempts === 1) throw Error('read failed');
    return retry.promise;
  });
  model.reset(context());
  await model.ensure(0);
  await model.ensure(50);
  const retried = model.retry();
  assert.equal(await model.pick(), null);
  retry.resolve(page(50));
  await retried;
  assert.equal((await model.pick()).id, 'item-0');
});

test('cross-page navigation preserves absolute indexes and chooses the exact loaded ID', async () => {
  const d = deferred();
  const model = new ui.PagedList(async (ctx, offset) => offset === 50 ? d.promise : page(offset));
  model.reset(context());
  await model.ensure(0);
  model.select(49);
  model.select(50);
  const a = model.pick();
  model.select(51);
  const b = model.pick();
  d.resolve(page(50));
  assert.equal(await a, null);
  const chosen = await b;
  assert.equal(model.selected, 51);
  assert.equal(chosen.id, 'item-51');
  assert.equal(chosen.session, 1);
  assert.equal(model.isCurrent(chosen), true);
  model.select(52);
  assert.equal(model.isCurrent(chosen), false);
  model.reset(context(2));
  assert.equal(model.isCurrent(chosen), false);
});

test('a stale query cannot choose and first-page completion does not overwrite navigation', async () => {
  const d = deferred();
  const model = new ui.PagedList(() => d.promise);
  model.reset(context(), page(0));
  model.select(70);
  const pick = model.pick();
  model.reset(context(1, 'changed'));
  d.resolve(page(50));
  assert.equal(await pick, null);
  assert.equal(model.selected, -1);
  model.seed(page(0, 0));
  assert.equal(await model.pick(), null);
});

test('virtual ranges stay bounded at the start, middle, end and after a shrink', () => {
  assert.equal(ui.ROW_HEIGHT, 70);
  for (const total of [0, 1, 50, 10000, 1000000]) {
    for (const top of [0, 69, 3500, 69999999]) {
      const range = ui.virtualRange(total, top, 301);
      assert.ok(range.start >= 0 && range.end <= total && range.end >= range.start);
      assert.ok(range.end - range.start <= 12);
      assert.equal(range.top + (range.end - range.start) * 70 + range.bottom, total * 70);
    }
  }
  assert.equal(ui.scrollForIndex(50, 0, 280), 3290);
  assert.equal(ui.scrollForIndex(49, 3500, 280), 3430);
});

test('panel state guards reject old sessions and late loading responses', () => {
  assert.equal(ui.acceptPanel({ session: 2, loading: false }, { session: 1, loading: false }), false);
  assert.equal(ui.acceptPanel({ session: 2, loading: false }, { session: 2, loading: true }), false);
  assert.equal(ui.acceptPanel({ session: 2, loading: true }, { session: 2, loading: false }), true);
  assert.equal(ui.acceptPanel({ session: 2, loading: false }, { session: 3, loading: true }), true);
});

test('an initial backend view failure requires reopening, not an empty-results message', () => {
  const failed = { session: 1, loading: false, total: 0, notice: '历史无法读取：disk error' };
  assert.equal(ui.panelReadError(failed), failed.notice);
  assert.equal(ui.panelReadError({ ...failed, loading: true }), '');
  assert.equal(ui.panelReadError({ ...failed, total: 50 }), '');
  assert.equal(ui.panelReadError({ ...failed, notice: 'Permission notice' }), '');
});
