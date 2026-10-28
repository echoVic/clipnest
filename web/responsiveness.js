// Promise coalescing and bounded LRU only for the current panel context.
// Reopening invalidates both completed and in-flight entries, even with the
// same ID. Rejections are never retained, and stale completions cannot evict
// a newer request for that key.
export class DetailCache {
  constructor(limit = 12) { this.limit = limit; this.generation = 0; this.items = new Map(); }
  clear() { this.generation++; this.items.clear(); }
  get(key, load) {
    const old = this.items.get(key);
    if (old) { this.items.delete(key); this.items.set(key, old); return old.promise; }
    const generation = this.generation;
    const record = {};
    record.promise = Promise.resolve().then(load).catch(error => {
      if (generation === this.generation && this.items.get(key) === record) this.items.delete(key);
      throw error;
    });
    this.items.set(key, record);
    while (this.items.size > this.limit) this.items.delete(this.items.keys().next().value);
    return record.promise;
  }
}

// Updating the two changed options is independent of history length. No
// pointer hover or query completion can change the keyboard selection.
export function updateSelection(rows, oldIndex, newIndex) {
  const update = (index, active) => {
    const row = rows[index];
    if (row) { row.classList.toggle('selected', active); row.setAttribute('aria-selected', active); }
  };
  if (oldIndex !== newIndex) update(oldIndex, false);
  update(newIndex, true);
}

export function characterCount(text) {
  let count = 0;
  for (const _ of text) count++;
  return count;
}

export const ROW_HEIGHT = 70;
export const PAGE_SIZE = 50;
const MAX_PAGES = 8;

export function virtualRange(total, scrollTop, height) {
  const top = Math.max(0, Math.min(scrollTop, total * ROW_HEIGHT - height));
  const start = Math.max(0, Math.floor(top / ROW_HEIGHT) - 3);
  const end = Math.min(total, Math.ceil((top + height) / ROW_HEIGHT) + 3);
  return { start, end, top: start * ROW_HEIGHT, bottom: (total - end) * ROW_HEIGHT };
}

export function scrollForIndex(index, scrollTop, height) {
  const top = index * ROW_HEIGHT;
  return top < scrollTop ? top : top + ROW_HEIGHT > scrollTop + height ? Math.max(0, top + ROW_HEIGHT - height) : scrollTop;
}

export function acceptPanel(current, next) {
  return next.session >= current.session && !(next.session === current.session && !current.loading && next.loading);
}

export function panelReadError(state) {
  return !state.loading && state.total === 0 && state.notice?.startsWith('\u5386\u53f2\u65e0\u6cd5\u8bfb\u53d6\uff1a') ? state.notice : '';
}

// Metadata and requests are independently bounded. Dispatched RPCs retain
// their slots until they settle, including backend cancellation rejections.
export class PagedList {
  constructor(load) {
    this.load = load;
    this.pages = new Map();
    this.pending = new Map();
    this.generation = 0;
    this.selectionVersion = 0;
    this.reset({});
  }
  reset(context, firstPage) {
    this.generation++;
    this.context = { ...context };
    this.pages.clear();
    this.total = 0;
    this.selected = -1;
    this.error = null;
    this.retrying = false;
    this.failedOffset = 0;
    this.selectionVersion++;
    if (firstPage) this.seed(firstPage);
  }
  seed(page) {
    this.total = page.total;
    this.pages.delete(page.offset);
    this.pages.set(page.offset, { ...page, entries: page.entries.slice(0, PAGE_SIZE) });
    while (this.pages.size > MAX_PAGES) this.pages.delete(this.pages.keys().next().value);
    this.selected = this.total ? Math.max(0, Math.min(this.selected, this.total - 1)) : -1;
  }
  entry(index) {
    return this.pages.get(Math.floor(index / PAGE_SIZE) * PAGE_SIZE)?.entries[index % PAGE_SIZE];
  }
  select(index) {
    const next = this.total ? Math.max(0, Math.min(index, this.total - 1)) : -1;
    if (next !== this.selected) { this.selected = next; this.selectionVersion++; }
  }
  ensure(index) {
    if (index < 0 || this.error) return Promise.resolve(null);
    const offset = Math.floor(index / PAGE_SIZE) * PAGE_SIZE;
    const cached = this.pages.get(offset);
    if (cached) {
      this.pages.delete(offset); this.pages.set(offset, cached);
      return Promise.resolve(cached);
    }
    const generation = this.generation, context = this.context, key = `${generation}:${offset}`;
    if (this.pending.has(key)) return this.pending.get(key);
    if (this.pending.size >= MAX_PAGES) return Promise.resolve(null);
    const promise = Promise.resolve().then(() => {
      if (generation !== this.generation) return null;
      return this.load(context, offset, PAGE_SIZE);
    }).then(page => {
      if (generation !== this.generation) return null;
      if (!page || page.offset !== offset || !Number.isSafeInteger(page.total) || page.total < 0 || !Array.isArray(page.entries) ||
          page.entries.length !== Math.min(PAGE_SIZE, Math.max(0, page.total - offset))) throw Error('Invalid page response');
      this.seed(page);
      return page;
    }).catch(error => {
      if (generation === this.generation) { this.error = error; this.failedOffset = offset; }
      return null;
    }).finally(() => { this.pending.delete(key); this.onSettled?.(); });
    this.pending.set(key, promise);
    return promise;
  }
  async retry() {
    const offset = this.failedOffset, generation = this.generation;
    this.retrying = true;
    this.error = null;
    try { return await this.ensure(offset); }
    finally { if (generation === this.generation) this.retrying = false; }
  }
  async pick() {
    if (this.selected < 0 || this.error || this.retrying) return null;
    const ticket = { ...this.context, generation: this.generation, selectionVersion: this.selectionVersion, index: this.selected };
    await this.ensure(ticket.index);
    const entry = this.entry(ticket.index);
    ticket.id = entry?.id;
    return this.isCurrent(ticket) ? ticket : null;
  }
  isCurrent(ticket) {
    return !!ticket?.id && !this.error && !this.retrying && ticket.generation === this.generation &&
      ticket.selectionVersion === this.selectionVersion && ticket.index === this.selected &&
      ticket.id === this.entry(this.selected)?.id;
  }
}
