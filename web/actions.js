// The action menu lists what the selected entry supports. Each item's type is
// the keyAction type its shortcut produces, so app.js runs both the same way.
// Destructive actions come last.
export function entryActions({ view, kind, imageText, index, total, hasQuery, ctrl }) {
  const key = (mac, other) => ctrl ? other : mac;
  const actions = [
    { type: 'choose', label: '粘贴', shortcut: '↩' },
    { type: 'copy', label: '仅复制', shortcut: key('⌘ ↩', 'Ctrl ↩') },
  ];
  if (view === 'phrases') {
    actions.push({ type: 'edit', label: '编辑', shortcut: key('⌘ E', 'Ctrl E') });
    if (!hasQuery && index > 0) actions.push({ type: 'reorder', delta: -1, label: '上移', shortcut: key('⌥ ↑', 'Alt ↑') });
    if (!hasQuery && index < total - 1) actions.push({ type: 'reorder', delta: 1, label: '下移', shortcut: key('⌥ ↓', 'Alt ↓') });
  } else {
    if (kind !== 'image') actions.push({ type: 'save-phrase', label: '存为常用语', shortcut: key('⌘ S', 'Ctrl S') });
    if (kind === 'link') actions.push({ type: 'open-link', label: '打开链接', shortcut: key('⌘ O', 'Ctrl O') });
    if (imageText) actions.push({ type: 'image-text', label: '复制图片文字', shortcut: '' });
  }
  // Deleting a phrase asks for confirmation first; history removal does not.
  actions.push({ type: 'delete', label: view === 'phrases' ? '删除…' : '删除', shortcut: key('⌘ ⌫', 'Delete'), danger: true });
  return actions;
}
