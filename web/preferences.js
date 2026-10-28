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
