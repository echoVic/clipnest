// shortcuts.json lists the panel keys for the preferences page; the native
// window reads the same file, and shortcuts.test.mjs checks it against keys.js.

const ctrlGlyphs = { '⌘': 'Ctrl', '⌥': 'Alt', '⇧': 'Shift' };

// Windows and Linux spell the macOS modifier glyphs out.
export function ctrlKeys(text) {
  return text.replace(/[⌘⌥⇧]/g, glyph => ` ${ctrlGlyphs[glyph]} `).replace(/\s+/g, ' ').trim();
}

// An item names its macOS keys; `other` replaces them where the convention
// differs, as Delete does for ⌘⌫.
export function shortcutKeys(item, ctrl) {
  if (!ctrl) return item.keys;
  return item.other ?? item.keys.map(ctrlKeys);
}

export function renderShortcutGroups(container, groups, ctrl) {
  const document = container.ownerDocument;
  const element = (tag, className, text) => {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  };
  container.replaceChildren(...groups.map(group => {
    const section = element('section', 'settings-group');
    section.append(element('h3', '', group.group));
    for (const item of group.items) {
      const label = element('div');
      label.append(element('strong', '', item.label));
      if (item.note) label.append(element('p', '', item.note));
      const keys = element('span', 'shortcut-keys');
      keys.append(...shortcutKeys(item, ctrl).map(key => element('kbd', '', key)));
      const row = element('div', 'setting-row');
      row.append(label, keys);
      section.append(row);
    }
    return section;
  }));
}
