import { moveIndex, numberIndex, WheelSelection } from './selection.js';
import { createDemo } from './demo.js';

const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
const icon = name => {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  const use = document.createElementNS(svg.namespaceURI, 'use');
  use.setAttribute('href', `#i-${name}`); svg.append(use); return svg;
};
const node = (tag, cls, text) => { const n = document.createElement(tag); if (cls) n.className = cls; if (text !== undefined) n.textContent = text; return n; };
const demo = !window.mygo;
const api = demo ? createDemo() : { call: (method, ...args) => window.mygo.call(`Desktop.${method}`, ...args) };
if (demo) document.body.classList.add('demo');
let state = { session: 0, entries: [], paused: false, accessible: false, platform: '', target: '', shortcut: '⌘ ⇧ V' };
let entries = [], selected = -1, kind = 'all', busy = false, queryVersion = 0, detailVersion = 0, queryTimer, toastTimer, queryPending = false;
const wheel = new WheelSelection();
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
  $('#count').textContent = state.entries.length;
  $('#permission-status').textContent = demo ? '演示模式，桌面版可粘贴到原应用。' : state.platform !== 'darwin' ? '此系统使用「仅复制」，自动粘贴支持 macOS。' : state.accessible ? '已允许，选完即可粘贴到原应用。' : '允许辅助功能，选完直接粘贴。';
  $('#permission').hidden = demo || state.platform !== 'darwin' || state.accessible;
}
function applyState(next, reopen = false) {
  state = next; header();
  if (reopen) {
    clearTimeout(queryTimer); ++queryVersion; queryPending = false; $('#search').value = ''; $('#clear-search').hidden = true; kind = 'all';
    $$('.filters button').forEach(b => { b.classList.toggle('active', b.dataset.kind === kind); b.setAttribute('aria-pressed', b.dataset.kind === kind); });
    entries = state.entries; selected = entries.length ? 0 : -1; wheel.reset();
    document.body.classList.remove('dismissed'); $('#panel').classList.remove('dismissed');
    $$('dialog[open]').forEach(d => d.close());
    render(); requestAnimationFrame(() => $('#list').focus({ preventScroll: true }));
  } else refresh();
}
async function refresh() {
  const version = ++queryVersion;
  queryPending = true;
  try {
    const result = await call('Query', state.session, $('#search').value, kind);
    if (version !== queryVersion) return;
    queryPending = false; entries = result || []; selected = entries.length ? 0 : -1; wheel.reset(); render();
  } catch (e) { if (version === queryVersion) toast(message(e)); }
}
function render() {
  const list = $('#list'); const fragment = document.createDocumentFragment();
  entries.forEach((e, i) => {
    const button = node('button', `entry${i === selected ? ' selected' : ''}`);
    button.id = `entry-${i}`; button.type = 'button'; button.tabIndex = -1;
    button.setAttribute('role', 'option'); button.setAttribute('aria-selected', i === selected);
    button.setAttribute('aria-label', `${i + 1}，${e.title}，${labels[e.kind] || '文字'}`);
    const type = node('span', `entry-icon ${e.kind}`); type.append(icon(e.kind));
    const copy = node('span', 'entry-copy'); copy.append(node('div', 'entry-title', e.title));
    const sub = node('span', 'entry-sub'); sub.append(node('span', 'source', e.source || labels[e.kind]), node('i', 'dot'), node('span', '', relativeTime(e.created)));
    copy.append(sub); button.append(type, copy, node('kbd', '', i < 9 ? i + 1 : ''));
    button.addEventListener('click', () => { select(i); choose(false); });
    // Deliberately no mouseenter handler: pointer hover never steals selection.
    fragment.append(button);
  });
  list.replaceChildren(fragment); list.scrollTop = 0;
  $('#empty').hidden = entries.length > 0; list.hidden = entries.length === 0;
  const filtered = $('#search').value.trim() || kind !== 'all';
  $('#list-label').textContent = filtered ? `${entries.length} 条匹配内容` : '最近复制';
  $('#empty h2').textContent = filtered ? '还没有找到这条内容' : '复制一下，好内容就留下了';
  $('#empty p').textContent = filtered ? '试试更短的关键词，或切换到「全部」。' : `复制文字或图片后，在这里找回。按 ${state.shortcut} 随时唤起。`;
  selected = moveIndex(selected, 0, entries.length); selectionChanged();
}
function select(index) { if (busy || index === selected) return; selected = moveIndex(index, 0, entries.length); selectionChanged(); }
function selectionChanged() {
  $$('.entry').forEach((b, i) => { b.classList.toggle('selected', i === selected); b.setAttribute('aria-selected', i === selected); });
  const current = $(`#entry-${selected}`);
  if (current) { $('#list').setAttribute('aria-activedescendant', current.id); current.scrollIntoView({ block: 'nearest', behavior: 'instant' }); }
  else $('#list').removeAttribute('aria-activedescendant');
  updatePreview();
}
async function updatePreview() {
  const version = ++detailVersion;
  const entry = entries[selected];
  $('#paste').disabled = $('#copy-only').disabled = !entry || busy;
  $('#preview-meta').replaceChildren();
  if (!entry) {
    $('#type-label').textContent = '等待灵感';
    const empty = node('div', 'preview-placeholder'); empty.append(icon('copy'), node('span', '', '留住刚才复制的好内容。')); $('#preview-body').replaceChildren(empty); return;
  }
  $('#type-label').textContent = labels[entry.kind] || '文字';
  $('#preview-body').replaceChildren(node('p', 'preview-text', entry.excerpt));
  try {
    const e = await call('Detail', state.session, entry.id);
    if (version !== detailVersion || !e) return;
    let content;
    if (e.kind === 'image') {
      content = node('div', 'image-stage'); const img = new Image(); img.alt = e.title; img.src = `data:image/png;base64,${e.image}`; content.append(img);
    } else if (e.kind === 'link') {
      content = node('div', 'preview-link'); const orb = node('div', 'link-orb'); orb.append(icon('link'));
      let host = '链接'; try { host = new URL(e.text.trim()).hostname; } catch {}
      content.append(orb, node('h2', '', host), node('p', '', e.text));
    } else content = node(e.kind === 'code' ? 'pre' : 'p', e.kind === 'code' ? 'preview-code' : 'preview-text', e.text);
    $('#preview-body').replaceChildren(content); $('#preview-body').scrollTop = 0;
    const info = node('span', '', e.kind === 'image' ? `${e.width} × ${e.height} · PNG` : `${[...(e.text || '')].length} 个字符 · ${labels[e.kind] === '代码' ? '纯文本代码' : '纯文本'}`);
    const remove = node('button', 'icon-button delete-entry'); remove.title = '移除此条记录'; remove.setAttribute('aria-label', '移除此条记录'); remove.append(icon('trash'));
    remove.onclick = async () => { if (busy) return; try { applyState(await call('Delete', state.session, e.id)); toast('已移除这条记录'); } catch (err) { toast(message(err)); } };
    $('#preview-meta').replaceChildren(info, remove);
  } catch (err) { if (version === detailVersion) toast(message(err)); }
}
async function choose(copyOnly) {
  if (queryPending) { clearTimeout(queryTimer); await refresh(); if (queryPending) return; }
  const entry = entries[selected]; if (!entry || busy) return;
  if (!copyOnly && !demo && state.platform === 'darwin' && !state.accessible) {
    state.accessible = await call('Permission'); header();
    if (!state.accessible) { $('#permission-dialog').showModal(); return; }
  }
  busy = true; $('#paste').disabled = $('#copy-only').disabled = true;
  try {
    const result = await call('Choose', state.session, entry.id, copyOnly);
    if (demo && !copyOnly) $('#panel').classList.add('dismissed');
    toast(result.message);
  } catch (err) { toast(message(err)); }
  finally { busy = false; $('#paste').disabled = $('#copy-only').disabled = entries.length === 0; }
}
async function dismiss() { if (busy) return; await call('Dismiss'); if (demo) $('#panel').classList.add('dismissed'); }

$('#search').addEventListener('input', () => { ++queryVersion; queryPending = true; $('#clear-search').hidden = !$('#search').value; clearTimeout(queryTimer); queryTimer = setTimeout(refresh, 60); });
$('#clear-search').onclick = () => { $('#search').value = ''; $('#clear-search').hidden = true; refresh(); $('#search').focus(); };
$$('.filters button').forEach(b => b.onclick = () => { if (busy) return; kind = b.dataset.kind; $$('.filters button').forEach(x => { x.classList.toggle('active', x === b); x.setAttribute('aria-pressed', x === b); }); refresh(); });
$('#paste').onclick = () => choose(false); $('#copy-only').onclick = () => choose(true); $('#dismiss').onclick = dismiss;
$('#pause').onclick = async () => { try { const next = await call('Pause', !state.paused); state.paused = next.paused; header(); toast(state.paused ? '已暂停，新的复制不会保存' : '已恢复记录'); } catch (e) { toast(message(e)); } };
$('#settings').onclick = async () => { $('#preferences').showModal(); try { state.accessible = await call('Permission'); header(); } catch (e) { toast(message(e)); } };
$$('.close-dialog').forEach(b => b.onclick = () => b.closest('dialog').close());
$$('dialog').forEach(d => d.addEventListener('click', e => { const r = d.getBoundingClientRect(); if (e.target === d && (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom)) d.close(); }));
$('#clear-history').onclick = () => { $('#preferences').close(); $('#confirm-clear').showModal(); };
$('#confirm-delete').onclick = async () => { try { applyState(await call('Clear'), true); $('#confirm-clear').close(); toast('所有历史已清空'); } catch (e) { toast(message(e)); } };
const openPermission = async () => { try { await call('OpenPermission'); } catch (e) { toast(message(e)); } };
$('#permission').onclick = $('#permission-open').onclick = openPermission;
$('#permission-copy').onclick = () => { $('#permission-dialog').close(); choose(true); };
$('#reopen').onclick = async () => applyState(await call('State'), true);
$('#list').addEventListener('wheel', event => {
  if (busy || event.ctrlKey || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return;
  event.preventDefault(); const step = wheel.consume(event.deltaY, event.deltaMode, performance.now());
  if (step) { select(moveIndex(selected, step, entries.length)); $('#list').focus({ preventScroll: true }); }
}, { passive: false });

document.addEventListener('keydown', event => {
  if (event.isComposing || event.keyCode === 229) return;
  if (event.key.toLowerCase() === 'v' && (event.metaKey || event.ctrlKey) && event.shiftKey && demo) { event.preventDefault(); $('#reopen').click(); return; }
  if ($('dialog[open]')) return;
  if (event.key === 'Escape') {
    event.preventDefault(); if ($('#search').value) { $('#search').value = ''; $('#clear-search').hidden = true; refresh(); $('#list').focus(); } else dismiss(); return;
  }
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); $('#search').focus(); return; }
  if (busy || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
  const typing = event.target.matches('input,textarea,select,[contenteditable=true]');
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); select(moveIndex(selected, event.key === 'ArrowDown' ? 1 : -1, entries.length)); if (!typing) $('#list').focus({ preventScroll: true }); }
  else if (event.key === 'Enter' && (typing || event.target === document.body || event.target === $('#list'))) { event.preventDefault(); choose(false); }
  else if (!typing && !event.repeat) { const index = numberIndex(event.key, entries.length); if (index >= 0) { event.preventDefault(); select(index); choose(false); } }
});
const mq = matchMedia('(prefers-color-scheme:dark)');
function applyTheme() { const theme = $('#theme').value; document.documentElement.dataset.theme = theme === 'system' ? (mq.matches ? 'dark' : 'light') : theme; }
try { $('#theme').value = localStorage.getItem('clipnest-theme') || 'system'; } catch {}
$('#theme').onchange = () => { try { localStorage.setItem('clipnest-theme', $('#theme').value); } catch {} applyTheme(); }; mq.addEventListener('change', applyTheme); applyTheme();
if (!demo) window.mygo.on('clipnest:panel', data => applyState(data, true));
try { applyState(await call('State'), true); } catch (e) { $('#notice').hidden = false; $('#notice').textContent = `无法加载历史：${message(e)}`; }
