import { PAGE_SIZE } from './responsiveness.js';

export function entryDialogs({ call, context, currentEntry, blocked, choose, toast, message, selected }) {
  const $ = selector => document.querySelector(selector);
  const shortcut = $('#phrase-shortcut-dialog');
  const shortcutSlot = $('#phrase-shortcut-slot');
  const shortcutError = $('#phrase-shortcut-error');
  const merge = $('#merge-text-dialog');
  const candidates = $('#merge-candidates');
  const previous = $('#merge-prev');
  const next = $('#merge-next');
  const mergeSubmit = $('#merge-submit');
  let phraseID = '', selection = null;

  async function editShortcut() {
    const entry = currentEntry();
    if (context().view !== 'phrases' || blocked() || !entry || shortcut.open) return;
    try {
      const slots = await call('PhraseShortcuts');
      if (context().view !== 'phrases' || blocked() || currentEntry()?.id !== entry.id || shortcut.open) return;
      shortcutSlot.replaceChildren(new Option('无', '0'));
      let assignedSlot = '0';
      for (const slot of slots) {
        const assigned = slot.phraseID === entry.id;
        const occupied = !!slot.phraseID && !assigned;
        const suffix = occupied ? '（已绑定）' : assigned && !slot.active ? '（未生效）' : '';
        const option = new Option(slot.label + suffix, slot.slot);
        option.disabled = occupied;
        shortcutSlot.add(option);
        if (assigned) assignedSlot = option.value;
      }
      shortcutSlot.value = assignedSlot;
      phraseID = entry.id;
      shortcutError.hidden = true;
      shortcut.showModal();
    } catch (err) { toast(message(err)); }
  }

  $('#phrase-shortcut-save').onclick = async event => {
    const button = event.currentTarget;
    button.disabled = shortcutSlot.disabled = true;
    shortcutError.hidden = true;
    const slot = Number(shortcutSlot.value);
    try {
      await call('SetPhraseShortcut', phraseID, slot);
      if (shortcut.open) shortcut.close();
      toast(slot ? '已保存全局快捷键' : '已移除快捷键');
    } catch (err) { shortcutError.textContent = message(err); shortcutError.hidden = false; }
    finally { button.disabled = shortcutSlot.disabled = false; }
  };

  function updateMergeSelection() {
    const ids = [...selection.ids];
    for (const label of candidates.querySelectorAll('label')) {
      const position = ids.indexOf(label.dataset.entryId);
      label.querySelector('input').checked = position >= 0;
      label.querySelector('.merge-position').textContent = position >= 0 ? `${position + 1}. ` : '';
    }
    $('#merge-status').textContent = `已选 ${ids.length} 条`;
    mergeSubmit.disabled = ids.length < 2;
  }

  async function loadMergePage(offset) {
    const draft = selection;
    if (!draft || draft.loading) return;
    draft.loading = true;
    previous.disabled = next.disabled = true;
    candidates.textContent = '正在加载…';
    try {
      const page = await call('Page', draft.session, draft.revision, draft.q, draft.kind, offset, PAGE_SIZE);
      if (draft !== selection) return;
      draft.offset = page.offset;
      candidates.replaceChildren();
      for (const entry of page.entries) {
        if (entry.kind === 'image' || entry.secret) continue;
        const label = document.createElement('label');
        const check = document.createElement('input');
        const position = document.createElement('span');
        const title = document.createElement('span');
        label.dataset.entryId = entry.id;
        check.type = 'checkbox';
        position.className = 'merge-position';
        title.className = 'merge-label';
        title.textContent = title.title = entry.title;
        check.onchange = () => {
          if (check.checked && draft.ids.size === 20) { updateMergeSelection(); toast('最多选择 20 条'); return; }
          if (check.checked) draft.ids.add(entry.id); else draft.ids.delete(entry.id);
          updateMergeSelection();
        };
        label.append(check, position, title);
        candidates.append(label);
      }
      candidates.scrollTop = 0;
      if (!candidates.childElementCount) candidates.textContent = '这一页没有可合并的文字';
      $('#merge-page').textContent = `${1 + page.offset / PAGE_SIZE} / ${Math.max(1, Math.ceil(page.total / PAGE_SIZE))}`;
      previous.disabled = offset === 0;
      next.disabled = offset + PAGE_SIZE >= page.total;
      updateMergeSelection();
    } catch (err) { if (draft === selection) candidates.textContent = message(err); }
    finally { draft.loading = false; }
  }

  function openMerge() {
    const entry = currentEntry();
    if (context().view !== 'history' || blocked() || !entry || entry.kind === 'image' || entry.secret || merge.open) return;
    selection = { ...context(), ids: new Set([entry.id]), offset: 0 };
    $('#merge-separator').value = 'newline';
    $('#merge-page').textContent = '';
    merge.showModal();
    updateMergeSelection();
    loadMergePage(Math.floor(selected() / PAGE_SIZE) * PAGE_SIZE);
  }

  merge.addEventListener('close', () => { if (!merge.open) selection = null; });
  previous.onclick = () => loadMergePage(selection.offset - PAGE_SIZE);
  next.onclick = () => loadMergePage(selection.offset + PAGE_SIZE);
  mergeSubmit.onclick = () => {
    const draft = selection;
    merge.close();
    if (draft && draft.session === context().session) choose(false, undefined, { ids: [...draft.ids], separator: $('#merge-separator').value });
  };

  return { editShortcut, openMerge };
}
