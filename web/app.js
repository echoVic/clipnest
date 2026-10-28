import { moveIndex, WheelSelection } from './selection.js';
import { keyAction } from './keys.js';
import { createDemo } from './demo.js';
import { DetailCache, PagedList, PAGE_SIZE, virtualRange, scrollForIndex, acceptPanel, panelReadError, updateSelection, characterCount } from './responsiveness.js';
import { looksLikeMarkdown, renderMarkdown } from './markdown.js';

const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
const icon = name => {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  const use = document.createElementNS(svg.namespaceURI, 'use');
  use.setAttribute('href', `#i-${name}`); svg.append(use); return svg;
};
const node = (tag, cls, text) => { const n = document.createElement(tag); if (cls) n.className = cls; if (text !== undefined) n.textContent = text; return n; };
const demo = !window.mygo;
const params = new URLSearchParams(location.search);
const api = demo ? createDemo({ legacyStorage: params.has('migration'), large: params.has('large') }) : { call: (method, ...args) => window.mygo.call(`Desktop.${method}`, ...args), on: (event, fn) => window.mygo.on(event, fn) };
document.body.classList.add(demo ? 'demo' : 'desktop');
let state = { session: 0, total: 0, loading: true, paused: false, accessible: false, platform: '', target: '', shortcut: '⌘ ⇧ V' };
let kind = 'all', view = 'history', draft = null, reordering = false, busy = false, choosing = false, queryVersion = 0, detailVersion = 0, queryTimer, toastTimer, queryPending = false, queryScheduled = false, renderFrame;
const wheel = new WheelSelection();
const details = new DetailCache();
const pages = new PagedList(async (context, offset, limit) => {
  if (context.view === 'history') return call('Page', context.session, context.revision, context.q, context.kind, offset, limit);
  const phrases = await call('Phrases', context.q);
  return { entries: phrases.slice(offset, offset + limit), total: phrases.length, offset };
});
const currentEntry = () => pages.entry(pages.selected);
const context = () => ({ session: state.session, revision: queryVersion, q: $('#search').value, kind, view });
const blocked = () => busy || queryPending || !!pages.error || pages.retrying || (view === 'history' && state.loading);
let rows = Object.create(null), paintedSelection = -1;
let migrating = false, migrationDismissed = false, migrationError = '', savingRetention = false, retentionDirty = false;
const labels = { text: '文字', link: '链接', code: '代码', image: '图片' };
const call = (method, ...args) => api.call(method, ...args);
const message = err => err?.message || String(err);
function toast(text) { $('#toast').textContent = text; $('#toast').hidden = false; clearTimeout(toastTimer); toastTimer = setTimeout(() => $('#toast').hidden = true, 3200); }
function relativeTime(time) {
  const mins = Math.max(0, Math.floor((Date.now() - time) / 60000));
  if (mins < 1) return '刚刚'; if (mins < 60) return `${mins} 分钟前`;
  if (mins < 1440) return `${Math.floor(mins / 60)} 小时前`;
  return new Date(time).toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' });
}
function header() {
  $('#status').classList.toggle('paused', state.paused);
  $('#status').replaceChildren(node('i'), document.createTextNode(state.paused ? '已暂停' : '正在记录'));
  $('#pause').replaceChildren(icon(state.paused ? 'play' : 'pause'));
  $('#pause').title = state.paused ? '恢复记录' : '暂停记录'; $('#pause').setAttribute('aria-label', $('#pause').title);
  $('#destination').textContent = state.target ? `粘贴到 ${state.target}` : '选中后可复制';
  $('#shortcut-label').textContent = state.shortcut;
  $('#notice').hidden = !state.notice; $('#notice').textContent = state.notice;
  $('#count').textContent = state.total;
  $('#permission-status').textContent = demo ? '演示模式，桌面版可粘贴到原应用。' : state.platform !== 'darwin' ? '此系统使用「仅复制」，自动粘贴支持 macOS。' : state.accessible ? '已允许，选完即可粘贴到原应用。' : '允许辅助功能，选完直接粘贴。';
  $('#permission').hidden = demo || state.platform !== 'darwin' || state.accessible;
  storagePrompt();
}
function storagePrompt() {
  const storage = state.storage;
  const available = !!(storage?.migrationAvailable || storage?.cleanupPending);
  $('#storage-upgrade').hidden = !available || migrationDismissed;
  $('#storage-upgrade').setAttribute('aria-busy', migrating);
  $('#migration-title').textContent = storage?.cleanupPending ? '存储已升级，旧文件待清理' : '升级本地存储';
  $('#migration-description').textContent = storage?.cleanupPending
    ? 'SQLite 已启用，可重试清理旧 JSON 文件。'
    : '减少重复写入。校验后替换旧存储，历史和常用语仍仅保存在本机。';
  $('#storage-description').textContent = storage?.backend === 'sqlite' ? 'SQLite · 已启用' : 'JSON · 可一键升级到 SQLite';
  $('#migrate-settings').hidden = !available;
  for (const button of [$('#migrate-storage'), $('#migrate-settings')]) {
    button.disabled = migrating;
    button.textContent = migrating ? '正在迁移…' : storage?.cleanupPending ? '重试清理' : '一键迁移';
  }
  $('#migration-later').disabled = migrating;
  for (const error of [$('#migration-error'), $('#storage-error')]) {
    error.hidden = !migrationError;
    error.textContent = migrationError;
  }
  const enabled = storage?.backend === 'sqlite';
  $('#retention-fields').disabled = !enabled || busy;
  $('#retention-save').disabled = !enabled || busy;
  $('#retention-save').textContent = savingRetention ? '正在清理…' : '保存并清理';
  $('#retention-legacy').hidden = enabled;
  if (!retentionDirty) {
    $('#retention-size').value = storage?.retention?.maxMiB ?? 512;
    $('#retention-days').value = storage?.retention?.keepDays ?? 0;
  }
}
async function saveRetention() {
  if (busy || state.storage?.backend !== 'sqlite') return;
  const inputs = [$('#retention-size'), $('#retention-days')];
  const values = inputs.map(input => input.valueAsNumber);
  if (inputs.some(input => !input.checkValidity()) || values.some(value => !Number.isSafeInteger(value) || value < 0)) {
    $('#retention-error').textContent = '请输入 0 或正整数。';
    $('#retention-error').hidden = false; return;
  }
  busy = savingRetention = true; $('#retention-error').hidden = true; storagePrompt(); actionButtons();
  try {
    const next = await call('SaveRetention', { maxMiB: values[0], keepDays: values[1] });
    retentionDirty = false; applyState(next); toast('已保存并清理历史，常用语不受影响');
  } catch (err) {
    $('#retention-error').textContent = message(err); $('#retention-error').hidden = false;
  } finally { busy = savingRetention = false; storagePrompt(); render(); }
}
async function migrateStorage() {
  if (busy || migrating) return;
  busy = migrating = true;
  migrationError = '';
  const controls = $$('button,input,textarea,select').filter(el => !el.disabled);
  controls.forEach(el => { el.disabled = true; });
  storagePrompt();
  try {
    state.storage = await call('MigrateStorage');
    toast(demo ? '演示：迁移已完成，未修改本地数据' : '存储升级完成');
  } catch (err) {
    migrationError = message(err);
    // A failure after publication (e.g. old-file cleanup) must not be shown
    // as a rollback. Ask the backend which store is now authoritative.
    try { state.storage = await call('StorageStatus'); }
    catch (statusError) { migrationError += `；无法确认存储状态：${message(statusError)}`; }
  } finally {
    busy = migrating = false;
    controls.forEach(el => { el.disabled = false; });
    actionButtons();
    storagePrompt();
    render();
  }
}
function showView() {
  $$('.views button').forEach(b => { b.classList.toggle('active', b.dataset.view === view); b.setAttribute('aria-pressed', b.dataset.view === view); });
  $('#panel').classList.toggle('view-phrases', view === 'phrases');
  $('#search').placeholder = view === 'phrases' ? '搜索常用语…' : '搜索复制过的内容…';
  $('#search').setAttribute('aria-label', view === 'phrases' ? '搜索常用语' : '搜索历史');
  $('#list').setAttribute('aria-label', view === 'phrases' ? '常用语' : '复制历史');
}
// The search text stays, so a query typed on the wrong page carries over.
function switchView(next) { if (busy || next === view) return; view = next; showView(); refresh(); if (view === 'phrases' && draft) openEditor(); }
function invalidateSearch(firstPage) {
  const version = ++queryVersion, next = context();
  queryPending = true; pages.reset(next, firstPage);
  if (state.loading || panelReadError(state)) return version;
  Promise.resolve(call('Search', next.session, next.revision)).catch(error => {
    if (version === queryVersion && next.session === state.session) toast(message(error));
  });
  return version;
}
function applyState(next, reopen = false) {
  if (!acceptPanel(state, next)) return;
  const fresh = next.session !== state.session, completed = state.loading && !next.loading;
  // Do not retain a second copy of the first page after it enters the LRU.
  const { entries: firstEntries = [], ...metadata } = next;
  state = metadata; header();
  if (reopen && fresh) {
    $('#search').value = ''; $('#clear-search').hidden = true; kind = 'all'; view = 'history'; showView();
    $$('.filters button').forEach(b => { b.classList.toggle('active', b.dataset.kind === kind); b.setAttribute('aria-pressed', b.dataset.kind === kind); });
    document.body.classList.remove('dismissed'); $('#panel').classList.remove('dismissed');
    $$('dialog[open]').forEach(d => d.close());
    requestAnimationFrame(() => $('#list').focus({ preventScroll: true }));
  }
  if (fresh || completed) {
    const first = !state.loading && view === 'history' && !$('#search').value.trim() && kind === 'all'
      ? { entries: firstEntries, total: state.total, offset: 0 } : undefined;
    refresh(undefined, first);
  }
}
async function refresh(keepId, firstPage) {
  clearTimeout(queryTimer); queryScheduled = false;
  details.clear();
  const version = invalidateSearch(firstPage);
  wheel.reset(); $('#list').scrollTop = 0;
  updatePreview();
  if (state.loading && view === 'history') { render(); return; }
  if (view === 'history' && panelReadError(state)) {
    queryPending = false; pages.error = Error(panelReadError(state)); render(); return;
  }
  try {
    if (view === 'phrases' && keepId) {
      const result = await call('Phrases', $('#search').value);
      if (version !== queryVersion) return;
      const index = Math.max(0, result.findIndex(e => e.id === keepId));
      const offset = Math.floor(index / PAGE_SIZE) * PAGE_SIZE;
      pages.seed({ entries: result.slice(offset, offset + PAGE_SIZE), total: result.length, offset });
      pages.select(index);
    } else if (!firstPage) await pages.ensure(0);
    if (version !== queryVersion) return;
    queryPending = !pages.pages.size && !pages.error;
    $('#list').scrollTop = scrollForIndex(Math.max(0, pages.selected), 0, $('#list').clientHeight);
    render();
  } catch (e) {
    if (version !== queryVersion) return;
    queryPending = false; pages.error = e; render();
  }
}
function render() {
  if (pages.pages.size || pages.error) queryPending = false;
  const phrases = view === 'phrases', filtered = !!$('#search').value.trim() || (!phrases && kind !== 'all');
  const error = pages.error;
  $('#list').hidden = !pages.total || !!error;
  $('#empty').hidden = !!pages.total && !error;
  $('#list').setAttribute('aria-busy', queryPending);
  $('#list-label').textContent = filtered ? `${pages.total} 条匹配内容` : phrases ? '全部常用语' : '最近复制';
  $('#list-order').textContent = phrases ? '按你的顺序' : '最新在前';
  emptyState(filtered);
  renderWindow();
  updatePreview();
  if (queryPending && !queryScheduled && !(view === 'history' && state.loading) && !error) pages.ensure(0);
}
function scheduleRender() {
  if (!renderFrame) renderFrame = requestAnimationFrame(() => { renderFrame = 0; render(); });
}
pages.onSettled = scheduleRender;
function renderWindow() {
  const list = $('#list'); const fragment = document.createDocumentFragment(); const phrases = view === 'phrases';
  rows = Object.create(null); paintedSelection = -1;
  const range = virtualRange(pages.error ? 0 : pages.total, list.scrollTop, list.clientHeight || 280);
  const spacer = height => { const n = node('div', 'list-spacer'); n.style.height = `${height}px`; n.setAttribute('role', 'presentation'); return n; };
  fragment.append(spacer(range.top));
  const missing = new Set();
  for (let i = range.start; i < range.end; i++) {
    const e = pages.entry(i);
    const button = node('button', `entry${i === pages.selected ? ' selected' : ''}`);
    button.id = `entry-${i}`; button.type = 'button'; button.tabIndex = -1;
    button.setAttribute('role', 'option'); button.setAttribute('aria-selected', i === pages.selected);
    button.setAttribute('aria-posinset', i + 1); button.setAttribute('aria-setsize', pages.total);
    rows[i] = button; fragment.append(button);
    if (!e) {
      button.disabled = true; button.classList.add('entry-loading'); button.textContent = '正在加载…';
      missing.add(Math.floor(i / PAGE_SIZE) * PAGE_SIZE); continue;
    }
    button.setAttribute('aria-label', phrases ? `${i + 1}，${e.excerpt}` : `${i + 1}，${e.title}，${labels[e.kind] || '文字'}`);
    const type = node('span', `entry-icon ${phrases ? 'text' : e.kind}`); type.append(icon(phrases ? 'text' : e.kind));
    const copy = node('span', 'entry-copy');
    if (phrases) copy.append(node('div', 'entry-excerpt', e.excerpt));
    else {
      copy.append(node('div', 'entry-title', e.title));
      const sub = node('span', 'entry-sub'); sub.append(node('span', 'source', e.source || labels[e.kind]), node('i', 'dot'), node('span', '', relativeTime(e.created)));
      copy.append(sub);
    }
    button.append(type, copy, node('kbd', '', i < 9 ? i + 1 : ''));
    button.addEventListener('click', () => { select(i); choose(false, { index: i, id: e.id }); });
    // Deliberately no mouseenter handler: pointer hover never steals selection.
  }
  fragment.append(spacer(range.bottom));
  list.replaceChildren(fragment);
  updateSelection(rows, paintedSelection, pages.selected); paintedSelection = pages.selected;
  const current = rows[pages.selected];
  if (current) list.setAttribute('aria-activedescendant', current.id);
  else list.removeAttribute('aria-activedescendant');
  if (!blocked()) {
    if (pages.selected >= 0 && !currentEntry()) missing.add(Math.floor(pages.selected / PAGE_SIZE) * PAGE_SIZE);
    for (const offset of missing) pages.ensure(offset);
  }
}
function emptyState(filtered) {
  const [title, text] = pages.error ? [view === 'phrases' ? '常用语文件无法读取' : '历史内容无法读取', message(pages.error)]
    : queryPending ? ['正在加载…', '']
    : view === 'phrases' ? (filtered ? ['没有找到这条常用语', '试试更短的关键词。'] : ['把常用的文字存起来，随时插入', ''])
    : filtered ? ['还没有找到这条内容', '试试更短的关键词，或切换到「全部」。'] : ['复制一下，好内容就留下了', `复制文字或图片后，在这里找回。按 ${state.shortcut} 随时唤起。`];
  $('#empty h2').textContent = title; $('#empty p').textContent = text; $('#empty p').hidden = !text;
  $('#empty-new').hidden = view !== 'phrases' || filtered || !!pages.error || queryPending; $('#new-phrase').disabled = !!pages.error || busy;
  $('#list-retry').hidden = !pages.error;
  $('#list-retry').textContent = view === 'history' && panelReadError(state) ? '关闭后重新唤起' : '重试加载';
}
function select(index) { if (blocked() || index === pages.selected) return; pages.select(index); selectionChanged(); }
function selectionChanged() {
  $('#list').scrollTop = scrollForIndex(pages.selected, $('#list').scrollTop, $('#list').clientHeight);
  renderWindow();
  updatePreview();
}
function actionButtons() { $('#paste').disabled = $('#copy-only').disabled = blocked() || !currentEntry() || choosing; }
async function updatePreview() {
  const version = ++detailVersion;
  const entry = blocked() ? undefined : currentEntry();
  actionButtons();
  $('#preview-meta').replaceChildren();
  if (!entry) {
    $('#type-label').textContent = view === 'phrases' ? '常用语' : '等待灵感';
    const empty = node('div', 'preview-placeholder'); empty.append(icon('copy'), node('span', '', view === 'phrases' ? '存好的常用语会显示在这里。' : '留住刚才复制的好内容。')); $('#preview-body').replaceChildren(empty); return;
  }
  if (view === 'phrases') return phrasePreview(entry, version);
  $('#type-label').textContent = labels[entry.kind] || '文字';
  $('#preview-body').replaceChildren(node('p', 'preview-text', entry.excerpt));
  try {
    const session = state.session;
    const e = await details.get(`history:${session}:${entry.id}`, () => call('Detail', session, entry.id));
    if (version !== detailVersion || !e) return;
    let content;
    if (e.kind === 'image') {
      content = node('div', 'image-stage'); const img = new Image(); img.alt = e.title; img.src = `data:image/png;base64,${e.image}`; content.append(img);
    } else if (e.kind === 'link') {
      content = node('div', 'preview-link'); const orb = node('div', 'link-orb'); orb.append(icon('link'));
      let host = '链接'; try { host = new URL(e.text.trim()).hostname; } catch {}
      content.append(orb, node('h2', '', host), node('p', '', e.text));
    } else if (e.kind === 'code') {
      content = node('pre', 'preview-code', e.text);
    } else if (looksLikeMarkdown(e.text)) {
      content = node('div', 'preview-md');
      renderMarkdown(e.text, content);
    } else {
      content = node('p', 'preview-text', e.text);
    }
    $('#preview-body').replaceChildren(content); $('#preview-body').scrollTop = 0;
    const info = node('span', '', e.kind === 'image' ? `${e.width} × ${e.height} · PNG` : `${characterCount(e.text || '')} 个字符 · ${labels[e.kind] === '代码' ? '纯文本代码' : '纯文本'}`);
    const remove = node('button', 'icon-button delete-entry'); remove.title = '移除此条记录'; remove.setAttribute('aria-label', '移除此条记录'); remove.append(icon('trash'));
    remove.onclick = async () => {
      if (blocked() || session !== state.session || currentEntry()?.id !== e.id) return;
      busy = true; actionButtons();
      try { applyState(await call('Delete', session, e.id)); toast('已移除这条记录'); }
      catch (err) { toast(message(err)); }
      finally { busy = false; render(); }
    };
    $('#preview-meta').replaceChildren(info, remove);
  } catch (err) { if (version === detailVersion) toast(message(err)); }
}
async function phrasePreview(entry, version) {
  $('#type-label').textContent = '常用语';
  $('#preview-body').replaceChildren(node('p', 'preview-text', entry.excerpt));
  try {
    const p = await details.get(`phrase:${entry.id}`, () => call('PhraseDetail', entry.id));
    if (version !== detailVersion || !p) return;
    if (looksLikeMarkdown(p.text)) {
      const md = node('div', 'preview-md');
      renderMarkdown(p.text, md);
      $('#preview-body').replaceChildren(md);
    } else {
      $('#preview-body').replaceChildren(node('p', 'preview-text', p.text));
    }
    $('#preview-body').scrollTop = 0;
    const edit = node('button', 'icon-button edit-phrase'); edit.title = '编辑（⌘E）'; edit.setAttribute('aria-label', '编辑这条常用语'); edit.append(icon('edit')); edit.onclick = editPhrase;
    const remove = node('button', 'icon-button delete-entry'); remove.title = '删除这条常用语'; remove.setAttribute('aria-label', '删除这条常用语'); remove.append(icon('trash')); remove.onclick = askDeletePhrase;
    const actions = node('span', 'meta-actions'); actions.append(edit, remove);
    $('#preview-meta').replaceChildren(node('span', '', `${characterCount(p.text)} 个字符`), actions);
  } catch (err) { if (version === detailVersion) toast(message(err)); }
}
async function choose(copyOnly, pick) {
  if (blocked() || choosing) return;
  if (pick) {
    if (pick.index < 0 || pick.index >= pages.total || (pick.id && pages.entry(pick.index)?.id !== pick.id)) return;
    select(pick.index);
  }
  choosing = true; actionButtons();
  let dispatching = false;
  try {
    const ticket = await pages.pick();
    if (!pages.isCurrent(ticket) || blocked()) return;
    if (!copyOnly && !demo && state.platform === 'darwin' && !state.accessible) {
      const accessible = await call('Permission');
      if (!pages.isCurrent(ticket) || blocked()) return;
      state.accessible = accessible; header();
      if (!accessible) { $('#permission-dialog').showModal(); return; }
    }
    if (!pages.isCurrent(ticket) || blocked()) return;
    busy = dispatching = true; actionButtons();
    const result = ticket.view === 'phrases' ? await call('InsertPhrase', ticket.session, ticket.id, copyOnly) : await call('Choose', ticket.session, ticket.id, copyOnly);
    if (demo && !copyOnly) $('#panel').classList.add('dismissed');
    toast(result.message);
  } catch (err) { toast(message(err)); }
  finally { choosing = false; if (dispatching) busy = false; render(); }
}
// The draft outlives the dialog: hiding the panel closes the editor but keeps
// the text, and returning to the phrases page reopens it.
function openEditor(next = draft) {
  draft = next;
  $('#phrase-editor-title').textContent = draft.id ? '编辑常用语' : '新建常用语';
  $('#phrase-text').value = draft.text; $('#phrase-error').hidden = true; editorChanged();
  if (!$('#phrase-editor').open) $('#phrase-editor').showModal();
  $('#phrase-text').focus();
}
function editorChanged() { draft.text = $('#phrase-text').value; $('#phrase-save').disabled = !draft.text.trim(); }
function newPhrase() { if (!busy && !pages.error) openEditor(draft || { id: '', text: '', original: '' }); }
async function editPhrase() {
  if (blocked()) return;
  if (draft) return openEditor();
  const entry = currentEntry(), version = queryVersion; if (!entry) return;
  try { const p = await call('PhraseDetail', entry.id); if (version === queryVersion && currentEntry()?.id === p.id) openEditor({ id: p.id, text: p.text, original: p.text }); }
  catch (err) { toast(message(err)); refresh(entry.id); }
}
async function savePhrase() {
  if (!draft || $('#phrase-save').disabled) return;
  $('#phrase-save').disabled = true;
  try {
    const saved = await call('SavePhrase', draft.id, draft.text);
    draft = null; $('#phrase-editor').close();
    await refresh(saved.id);
    if (currentEntry()?.id !== saved.id) { $('#search').value = ''; $('#clear-search').hidden = true; await refresh(saved.id); }
    $('#list').focus({ preventScroll: true });
    toast('常用语已保存');
  } catch (err) { $('#phrase-error').textContent = message(err); $('#phrase-error').hidden = false; editorChanged(); }
}
function cancelEditor() {
  if (draft && draft.text !== draft.original) { if (!$('#confirm-discard').open) $('#confirm-discard').showModal(); return; }
  draft = null; $('#phrase-editor').close();
}
function askDeletePhrase() { if (!blocked() && currentEntry()) $('#confirm-phrase-delete').showModal(); }
async function deletePhrase() {
  $('#confirm-phrase-delete').close();
  const index = pages.selected, entry = currentEntry(); if (!entry) return;
  const neighbor = pages.entry(index + 1)?.id ?? pages.entry(index - 1)?.id;
  try { await call('DeletePhrase', entry.id); await refresh(neighbor); toast('已删除这条常用语'); }
  catch (err) { toast(message(err)); refresh(entry.id); }
}
async function reorder(delta) {
  const entry = currentEntry(); if (!entry || reordering || blocked()) return;
  reordering = true;
  try { await call('MovePhrase', entry.id, delta); await refresh(entry.id); } catch (err) { toast(message(err)); refresh(entry.id); } finally { reordering = false; }
}
async function dismiss() {
  if (busy) return;
  invalidateSearch(); ++detailVersion; clearTimeout(queryTimer); queryScheduled = true;
  await call('Dismiss'); if (demo) $('#panel').classList.add('dismissed');
}

$('#search').addEventListener('input', () => {
  invalidateSearch(); queryScheduled = true; details.clear();
  $('#clear-search').hidden = !$('#search').value; clearTimeout(queryTimer); queryTimer = setTimeout(refresh, 60); render();
});
$('#list-retry').onclick = async () => {
  if (view === 'history' && panelReadError(state)) { await dismiss(); return; }
  queryPending = true; actionButtons(); await pages.retry(); queryPending = !pages.pages.size && !pages.error; render();
};
$('#clear-search').onclick = () => { $('#search').value = ''; $('#clear-search').hidden = true; refresh(); $('#search').focus(); };
$$('.filters button').forEach(b => b.onclick = () => { if (busy) return; kind = b.dataset.kind; $$('.filters button').forEach(x => { x.classList.toggle('active', x === b); x.setAttribute('aria-pressed', x === b); }); refresh(); });
$$('.views button').forEach(b => b.onclick = () => switchView(b.dataset.view));
$('#paste').onclick = () => choose(false); $('#copy-only').onclick = () => choose(true); $('#dismiss').onclick = dismiss;
$('#pause').onclick = async () => { try { const next = await call('Pause', !state.paused); state.paused = next.paused; header(); toast(state.paused ? '已暂停，新的复制不会保存' : '已恢复记录'); } catch (e) { toast(message(e)); } };
$('#settings').onclick = async () => { $('#preferences').showModal(); try { state.accessible = await call('Permission'); header(); } catch (e) { toast(message(e)); } };
$('#migrate-storage').onclick = $('#migrate-settings').onclick = migrateStorage;
$('#retention-save').onclick = saveRetention;
for (const input of [$('#retention-size'), $('#retention-days')]) input.oninput = () => { retentionDirty = true; $('#retention-error').hidden = true; };
$('#migration-later').onclick = () => { migrationDismissed = true; storagePrompt(); $('#list').focus({ preventScroll: true }); };
$$('.close-dialog').forEach(b => b.onclick = () => b.closest('dialog').close());
$$('dialog').forEach(d => d.addEventListener('click', e => { const r = d.getBoundingClientRect(); if (e.target === d && (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom)) d === $('#phrase-editor') ? cancelEditor() : d.close(); }));
$('#new-phrase').onclick = $('#empty-new').onclick = newPhrase;
$('#phrase-text').addEventListener('input', () => { $('#phrase-error').hidden = true; editorChanged(); });
$('#phrase-text').addEventListener('keydown', e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !e.isComposing) { e.preventDefault(); savePhrase(); } });
$('#phrase-save').onclick = savePhrase; $('#phrase-cancel').onclick = cancelEditor;
$('#phrase-editor').addEventListener('cancel', e => { e.preventDefault(); cancelEditor(); });
$('#keep-editing').onclick = () => { $('#confirm-discard').close(); $('#phrase-text').focus(); };
$('#discard-phrase').onclick = () => { draft = null; $('#confirm-discard').close(); $('#phrase-editor').close(); };
$('#confirm-phrase-delete-button').onclick = deletePhrase;
$('#clear-history').onclick = () => { $('#preferences').close(); $('#confirm-clear').showModal(); };
$('#confirm-delete').onclick = async () => {
  if (busy) return;
  busy = true; actionButtons();
  try { applyState(await call('Clear')); $('#confirm-clear').close(); toast('所有历史已清空'); }
  catch (e) { toast(message(e)); }
  finally { busy = false; render(); }
};
const openPermission = async () => { try { await call('OpenPermission'); } catch (e) { toast(message(e)); } };
$('#permission').onclick = $('#permission-open').onclick = openPermission;
// 更新窗口自己会说明结果；演示版没有桌面能力，不显示这个入口。
$('#check-update-wrap').hidden = demo;
$('#check-update').onclick = () => { if (!demo) api.call('CheckForUpdates').catch(() => {}); };
$('#permission-copy').onclick = () => { $('#permission-dialog').close(); choose(true); };
$('#reopen').onclick = async () => {
  invalidateSearch(); ++detailVersion; clearTimeout(queryTimer); queryScheduled = true;
  applyState(await call('State'), true);
};
$('#list').addEventListener('scroll', () => { renderWindow(); }, { passive: true });
new ResizeObserver(scheduleRender).observe($('#list'));
$('#list').addEventListener('wheel', event => {
  if (busy || event.ctrlKey || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return;
  event.preventDefault(); const step = wheel.consume(event.deltaY, event.deltaMode, performance.now());
  if (step) { select(moveIndex(pages.selected, step, pages.total)); $('#list').focus({ preventScroll: true }); }
}, { passive: false });

document.addEventListener('keydown', event => {
  if (migrating) { event.preventDefault(); return; }
  // The native panel also handles unclaimed Escape. Claim it even when an
  // IME uses the legacy 229 key code, without closing its composition.
  if (event.key === 'Escape' && (event.isComposing || event.keyCode === 229)) { event.preventDefault(); return; }
  const typing = event.target.matches('input,textarea,select,[contenteditable=true]');
  const action = keyAction(event, { demo, dialogOpen: !!$('dialog[open]'), busy, typing, atList: event.target === document.body || event.target === $('#list'), view, hasQuery: !!$('#search').value, hasSelection: pages.selected >= 0, count: pages.total });
  if (!action) return;
  event.preventDefault();
  switch (action.type) {
    case 'cancel-dialog': {
      const dialog = event.target.closest('dialog[open]') || $$('dialog[open]').at(-1);
      if (dialog === $('#phrase-editor')) cancelEditor(); else dialog?.close();
      break;
    }
    case 'reopen': $('#reopen').click(); break;
    case 'clear-search': $('#search').value = ''; $('#clear-search').hidden = true; refresh(); $('#list').focus(); break;
    case 'dismiss': dismiss(); break;
    case 'search': $('#search').focus(); break;
    case 'select': select(moveIndex(pages.selected, action.delta, pages.total)); if (!typing) $('#list').focus({ preventScroll: true }); break;
    case 'view': switchView(action.view); break;
    case 'choose': choose(false); break;
    case 'number': choose(false, { index: action.index }); break;
    case 'new': newPhrase(); break;
    case 'edit': editPhrase(); break;
    case 'reorder': reorder(action.delta); break;
  }
});
const mq = matchMedia('(prefers-color-scheme:dark)');
function applyTheme() { const theme = $('#theme').value; document.documentElement.dataset.theme = theme === 'system' ? (mq.matches ? 'dark' : 'light') : theme; }
try { $('#theme').value = localStorage.getItem('clipnest-theme') || 'system'; } catch {}
$('#theme').onchange = () => { try { localStorage.setItem('clipnest-theme', $('#theme').value); } catch {} applyTheme(); }; mq.addEventListener('change', applyTheme); applyTheme();
api.on('clipnest:panel', data => applyState(data, !busy && data.session !== state.session));
if (!demo) {
  const applyPreferences = preferences => {
    const root = document.documentElement;
    root.classList.toggle('reduce-motion', !!preferences.reduceMotion);
    root.classList.toggle('reduce-transparency', !!preferences.reduceTransparency);
    root.classList.toggle('high-contrast', !!preferences.highContrast);
  };
  applyPreferences(window.mygo.preferences);
  window.mygo.on('mygo:preferences', applyPreferences);
}
try { applyState(await call('State'), true); } catch (e) { $('#notice').hidden = false; $('#notice').textContent = `无法加载历史：${message(e)}`; }
