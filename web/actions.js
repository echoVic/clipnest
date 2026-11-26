// The action menu lists what the selected entry supports. Each item's type is
// the keyAction type its shortcut produces, so app.js runs both the same way.
// Destructive actions come last.
// sequence: the platform can paste on its own, which a paste sequence needs.
export function entryActions({ view, kind, secret, entryPinned, imageText, index, total, hasQuery, ctrl, sequence, phraseShortcuts }) {
  const key = (mac, other) => ctrl ? other : mac;
  const actions = [
    { type: 'choose', label: '粘贴', shortcut: '↩' },
    { type: 'copy', label: '仅复制', shortcut: key('⌘ ↩', 'Ctrl ↩') },
  ];
  if (view === 'phrases') {
    actions.push({ type: 'edit', label: '编辑', shortcut: key('⌘ E', 'Ctrl E') });
    if (phraseShortcuts) actions.push({ type: 'phrase-shortcut', label: '全局快捷键…' });
    if (!hasQuery && index > 0) actions.push({ type: 'reorder', delta: -1, label: '上移', shortcut: key('⌥ ↑', 'Alt ↑') });
    if (!hasQuery && index < total - 1) actions.push({ type: 'reorder', delta: 1, label: '下移', shortcut: key('⌥ ↓', 'Alt ↓') });
  } else {
    if (sequence > 1) actions.push({ type: 'sequence', label: `从这条起顺序粘贴（${sequence} 条）`, shortcut: '' });
    actions.push({ type: 'pin-entry', label: entryPinned ? '取消置顶' : '置顶这条', shortcut: '' });
    // A phrase would keep a masked secret in plain text.
    if (kind !== 'image' && !secret) actions.push({ type: 'save-phrase', label: '存为常用语', shortcut: key('⌘ S', 'Ctrl S') });
    if (!secret) {
      if (kind === 'text' || kind === 'code') {
        actions.push({ type: 'convert-case', mode: 'upper', label: '粘贴为大写' });
        actions.push({ type: 'convert-case', mode: 'lower', label: '粘贴为小写' });
      }
      if (kind !== 'image' && total > 1) actions.push({ type: 'merge-text', label: '合并粘贴…' });
    }
    if (kind === 'link') actions.push({ type: 'open-link', label: '打开链接', shortcut: key('⌘ O', 'Ctrl O') });
    if (imageText) actions.push({ type: 'image-text', label: '复制图片文字', shortcut: '' });
  }
  // Deleting a phrase asks for confirmation first; history removal does not.
  actions.push({ type: 'delete', label: view === 'phrases' ? '删除…' : '删除', shortcut: key('⌘ ⌫', 'Delete'), danger: true });
  return actions;
}

// The banner shown while a paste sequence holds the paste keys.
export function sequenceBanner(remaining, ctrl) {
  return remaining > 0 ? `正在顺序粘贴，剩 ${remaining} 条，每次 ${ctrl ? 'Ctrl+V' : '⌘V'} 粘贴下一条` : '';
}
