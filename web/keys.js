import { numberIndex } from './selection.js';

// Maps a keydown to the panel action it triggers, or null. app.js only performs
// the action, so the rules for both pages stay in one tested place.
export function keyAction(event, ctx) {
  if (event.isComposing || event.keyCode === 229) return null;
  const key = event.key, lower = key.toLowerCase(), command = event.metaKey || event.ctrlKey, phrases = ctx.view === 'phrases';
  if (lower === 'v' && command && event.shiftKey && ctx.demo) return { type: 'reopen' };
  if (ctx.dialogOpen) return key === 'Escape' ? { type: 'cancel-dialog' } : null;
  if (key === 'Escape') return { type: ctx.hasQuery ? 'clear-search' : 'dismiss' };
  if (command && lower === 'k') return { type: 'search' };
  if (ctx.busy) return null;
  if (command && !event.altKey && !event.shiftKey && phrases) {
    if (lower === 'n') return { type: 'new' };
    if (lower === 'e' && ctx.hasSelection) return { type: 'edit' };
  }
  if (event.altKey && !command && !event.shiftKey && (key === 'ArrowUp' || key === 'ArrowDown')) {
    return phrases && !ctx.hasQuery && !ctx.typing ? { type: 'reorder', delta: key === 'ArrowDown' ? 1 : -1 } : null;
  }
  if (command || event.altKey || event.shiftKey) return null;
  if (key === 'ArrowDown' || key === 'ArrowUp') return { type: 'select', delta: key === 'ArrowDown' ? 1 : -1 };
  if ((key === 'ArrowLeft' || key === 'ArrowRight') && !ctx.typing) return { type: 'view', view: key === 'ArrowLeft' ? 'history' : 'phrases' };
  if (key === 'Enter' && (ctx.typing || ctx.atList)) return { type: 'choose' };
  const index = ctx.typing || event.repeat ? -1 : numberIndex(key, ctx.count);
  return index >= 0 ? { type: 'number', index } : null;
}
