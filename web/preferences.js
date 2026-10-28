import { sampleApps } from './demo.js';

// Automatic activation keeps the visible page and keyboard focus together.
// Panels stay mounted so switching pages preserves unfinished edits.
export function preferencesTabs(root) {
  const tabs = [...root.querySelectorAll('[role="tab"]')];
  const panels = [...root.querySelectorAll('[role="tabpanel"]')];
  if (!tabs.length) return;
  function select(tab, focus = false) {
    for (const item of tabs) {
      const active = item === tab;
      item.setAttribute('aria-selected', String(active));
      item.tabIndex = active ? 0 : -1;
    }
    for (const panel of panels) panel.hidden = panel.id !== tab.getAttribute('aria-controls');
    root.querySelector('#preferences-title').textContent = tab.textContent.trim();
    root.querySelector('.preferences-content').scrollTop = 0;
    if (focus) tab.focus();
  }
  for (const [index, tab] of tabs.entries()) {
    tab.addEventListener('click', () => select(tab));
    tab.addEventListener('keydown', event => {
      const next = { ArrowRight: (index + 1) % tabs.length, ArrowLeft: (index + tabs.length - 1) % tabs.length, Home: 0, End: tabs.length - 1 }[event.key];
      if (next === undefined || event.altKey || event.ctrlKey || event.metaKey) return;
      event.preventDefault();
      select(tabs[next], true);
    });
  }
  select(tabs.find(tab => tab.getAttribute('aria-selected') === 'true') || tabs[0]);
}

// Exclusions have their own request state; they never refresh history or drafts.
export function preferencesExclusions(root, { call, demo }) {
  const $ = selector => root.querySelector(selector);
  const document = root.ownerDocument || root;
  const group = $('#excluded-apps-group'), list = $('#excluded-apps-list');
  const add = $('#excluded-apps-add'), retry = $('#excluded-apps-retry');
  const errorField = $('#excluded-apps-error'), empty = $('#excluded-apps-empty');
  const picker = $('#excluded-app-picker'), sample = $('#excluded-app-sample');
  let apps = [], pending = false, loaded = false, error = '';
  const node = (tag, text) => {
    const element = document.createElement(tag);
    if (text !== undefined) element.textContent = text;
    return element;
  };
  function controls() {
    group.setAttribute('aria-busy', pending);
    add.disabled = pending || !loaded;
    retry.disabled = pending;
    for (const button of list.querySelectorAll('button')) button.disabled = pending || !loaded;
    add.textContent = pending ? '正在更新…' : demo ? '添加示例…' : '添加应用…';
    errorField.textContent = error; errorField.hidden = !error;
    retry.hidden = loaded || !error;
    empty.hidden = !loaded || !!apps.length;
  }
  function render() {
    list.replaceChildren();
    for (const [index, app] of apps.entries()) {
      const row = node('li'); row.className = 'setting-row excluded-app-row';
      const copy = node('div'); copy.className = 'excluded-app-copy';
      const bundleID = node('p', app.bundleID); bundleID.className = 'excluded-app-id';
      copy.append(node('strong', app.name), bundleID);
      const remove = node('button', '移除'); remove.type = 'button'; remove.className = 'secondary';
      remove.setAttribute('aria-label', `移除 ${app.name}`);
      remove.onclick = async () => {
        if (pending || !loaded) return;
        await run('RemoveExcludedApp', app.bundleID);
        const buttons = list.querySelectorAll('button');
        (buttons[Math.min(index, buttons.length - 1)] || add).focus();
      };
      row.append(copy, remove); list.append(row);
    }
    controls();
  }
  const message = err => err?.message || String(err);
  async function run(method, ...args) {
    if (pending) return;
    pending = true; error = ''; controls();
    try {
      apps = await call(method, ...args); loaded = true;
    } catch (err) {
      error = message(err);
      if (method !== 'ExcludedApps') {
        // A save can fail after publication. Read back the authoritative list.
        try { apps = await call('ExcludedApps'); loaded = true; }
        catch (readError) { loaded = false; error += `；无法确认排除应用：${message(readError)}`; }
      }
    } finally { pending = false; render(); }
  }
  $('#excluded-apps-note').textContent = demo
    ? '浏览器演示仅使用示例应用，不读取真实应用或系统剪贴板。规则只在本次演示中保留。'
    : '按检测时的前台应用排除新的记录，不清理已有历史或常用语。后台复制或快速切换应用时，无法保证识别真正来源。';
  empty.textContent = '尚未排除应用。已有历史与常用语不受影响。';
  if (demo) {
    for (const app of sampleApps) {
      const option = node('option', `${app.name} · ${app.bundleID}`);
      option.value = app.id; sample.append(option);
    }
    sample.value = sampleApps[0].id;
  }
  add.onclick = () => {
    if (pending || !loaded) return;
    if (!demo) return run('AddExcludedApp');
    if (!picker.open) picker.showModal();
  };
  $('#excluded-app-confirm').onclick = () => {
    if (!picker.open || pending) return;
    picker.close();
    return run('AddExcludedApp', sample.value);
  };
  $('#excluded-app-cancel').onclick = () => picker.close();
  retry.onclick = () => run('ExcludedApps');
  render();
  return { refresh: () => run('ExcludedApps') };
}
