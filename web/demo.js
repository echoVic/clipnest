import { demoImagePNG, demoImageSize, demoImageText } from './demo-image.js';

const now = Date.now();
const samples = [
  ['text', '好的设计，是让复杂的事轻轻发生。', '好的设计，是让复杂的事轻轻发生。\n\n复制时不必整理，\n需要时总能找到。\n\n把注意力留给正在做的事，\n剩下的，交给 Clipnest。', '备忘录'],
  ['image', '复制的图片', '', '截图'],
  ['link', 'https://www.are.na/', 'https://www.are.na/', 'Safari'],
  ['code', 'const littleThings = makeLifeBetter()', 'const littleThings = makeLifeBetter()\n\nconst clipboard = {\n  capture: "effortless",\n  find: "instant",\n  paste: "right where you left off"\n}\n\nexport default clipboard', 'Visual Studio Code'],
  ['text', '周末计划：去海边，等一场日落。', '周末计划：去海边，等一场日落。\n带上相机，还有一直没读完的那本书。', '备忘录'],
  ['text', '#B8A7E8 · 一点温柔的紫色', '#B8A7E8', 'Figma'],
  ['link', 'https://www.figma.com/community', 'https://www.figma.com/community', 'Safari'],
  ['text', '少一点寻找，多一点心流。', '少一点寻找，多一点心流。', '备忘录'],
  ['text', '把灵感收好，留给下一次创作。', '把灵感收好，留给下一次创作。', '备忘录'],
  ['text', '明天下午 3 点，设计同步。', '明天下午 3 点，设计同步。', '日历'],
  ['text', '向前一步，也是一种进展。', '向前一步，也是一种进展。', '备忘录'],
];
const phraseSamples = [
  'Thanks for the detailed report! Could you share the steps to reproduce and your macOS version? That will help us track it down faster.',
  'Merged — thanks for the PR! Released in v0.1.4.\nThis will land in the next update, which checks every 4 hours.',
  '收到，我今天下班前给你答复。',
  '辛苦了，有问题随时找我。',
];
export const sampleApps = Object.freeze([
  { id: 'sample-browser', bundleID: 'com.example.browser', name: '示例浏览器' },
  { id: 'sample-notes', bundleID: 'com.example.notes', name: '示例备忘录' },
  { id: 'sample-passwords', bundleID: 'com.example.passwords', name: '示例密码管理器' },
  { id: 'sample-notes-renamed', bundleID: 'com.example.notes', name: '示例备忘录（新名称）' },
].map(Object.freeze));
// Same excerpt and limits as phrases.go, so the demo shows what the app would.
const excerpt = text => { const flat = [...text.trim().split(/\s+/).join(' ')]; return flat.length > 140 ? flat.slice(0, 140).join('') + '…' : flat.join(''); };
export function createDemo({ version = '开发版', accessible = true, legacyStorage = false, large = false, clipboardAccess = 'allowed' } = {}) {
  let entries = Array.from({ length: large ? 10000 : samples.length }, (_, i) => {
    const [kind, title, text, source] = samples[i % samples.length];
    const e = { id: `sample-${i}`, kind, title: large ? `${i + 1} · ${title}` : title, text, source, excerpt: excerpt(text), created: now - i * 125000 };
    if (kind === 'image') Object.assign(e, { image: demoImagePNG, ...demoImageSize, excerpt: `${demoImageSize.width} × ${demoImageSize.height} · PNG` });
    return e;
  });
  let session = 0, revision = 0, paused = false, loading = false, readyTimer;
  const listeners = new Set();
  let storage = { backend: legacyStorage ? 'json' : 'sqlite', migrationAvailable: legacyStorage, cleanupPending: false, retention: { maxMiB: 512, keepDays: 0 }, imageText: !legacyStorage };
  // Like the desktop, recognized text titles and matches an image only while
  // the feature is on and the history is in SQLite.
  const recognized = e => e.kind === 'image' && storage.backend === 'sqlite' && storage.imageText;
  const titled = e => recognized(e) ? { ...e, title: demoImageText.split('\n')[0] } : e;
  const searchable = e => (recognized(e) ? demoImageText : '') + e.text + titled(e).title + e.source;
  const imageDetail = e => {
    if (e.kind !== 'image') return {};
    if (storage.backend !== 'sqlite') return { imageTextState: 'needsSQLite' };
    return storage.imageText ? { imageTextState: 'ready', imageText: demoImageText } : { imageTextState: 'disabled' };
  };
  let excludedApps = [];
  const exclusions = () => excludedApps.map(app => ({ ...app }));
  let phrases = phraseSamples.map((text, i) => ({ id: `phrase-${i}`, text })), nextPhrase = phrases.length;
  const phrase = id => { const p = phrases.find(x => x.id === id); if (!p) throw new Error('这条常用语已删除'); return p; };
  const checkPhrase = text => {
    if (!text.trim()) throw new Error('内容不能为空');
    if (new TextEncoder().encode(text).length > 64 << 10) throw new Error('单条常用语最多 64 KB');
  };
  const summary = e => ({ ...titled(e), text: undefined, image: undefined });
  const status = () => ({ ...storage, retention: { ...storage.retention } });
  const state = () => ({ session, loading, total: entries.length, entries: loading ? [] : entries.slice(0, 50).map(summary), paused, clipboardAccess, accessible, version, target: '备忘录', platform: 'demo', notice: '', shortcut: '⌘ ⇧ V', storage: status() });
  const emit = () => { for (const fn of listeners) fn(state()); };
  const fresh = (publishLoading = false) => {
    session++; revision = 0; loading = true; clearTimeout(readyTimer);
    if (publishLoading) emit();
    readyTimer = setTimeout(() => { loading = false; emit(); }, 10);
    return state();
  };
  const checkSession = id => { if (id !== session) throw Error('会话已过期，请重新打开'); };
  const checkRevision = next => {
    if (!Number.isSafeInteger(next) || next < 0) throw Error('无效的搜索版本');
    if (next < revision) throw Error('搜索已过期');
    revision = next;
  };
  const historyEntry = id => { const e = entries.find(e => e.id === id); if (!e) throw Error('这条记录已删除'); return e; };
  return {
    on(event, fn) { if (event === 'clipnest:panel') listeners.add(fn); return () => listeners.delete(fn); },
    async call(method, ...args) {
    switch (method) {
      case 'State': return fresh(true);
      case 'StorageStatus': return status();
      case 'ExcludedApps': return exclusions();
      case 'AddExcludedApp': {
        if (args[0] == null) return exclusions();
        const app = sampleApps.find(app => app.id === args[0]);
        if (!app) throw Error('请选择示例应用；演示不读取真实应用');
        excludedApps = [...excludedApps.filter(item => item.bundleID !== app.bundleID), { bundleID: app.bundleID, name: app.name }]
          .sort((a, b) => a.bundleID < b.bundleID ? -1 : a.bundleID > b.bundleID ? 1 : 0);
        return exclusions();
      }
      case 'RemoveExcludedApp':
        excludedApps = excludedApps.filter(app => app.bundleID !== args[0]);
        return exclusions();
      case 'MigrateStorage':
        // A new SQLite database starts with image text recognition on.
        storage = { ...storage, backend: 'sqlite', migrationAvailable: false, cleanupPending: false, imageText: storage.backend === 'sqlite' ? storage.imageText : true };
        fresh(true);
        return status();
      case 'SaveRetention': {
        if (storage.backend !== 'sqlite') throw Error('请先迁移到 SQLite');
        const { maxMiB, keepDays } = args[0];
        if (![maxMiB, keepDays].every(n => Number.isSafeInteger(n) && n >= 0)) throw Error('请输入非负整数');
        storage.retention = { maxMiB, keepDays };
        if (keepDays) entries = entries.filter(e => e.created >= Date.now() - keepDays * 86400000);
        if (maxMiB) {
          const encoder = new TextEncoder();
          let size = 0;
          entries = entries.filter(e => { size += encoder.encode(e.text).length; return size <= maxMiB * 1048576; });
        }
        return fresh();
      }
      case 'Search': checkSession(args[0]); checkRevision(args[1]); return null;
      case 'Page': {
        checkSession(args[0]);
        const [id, next, q, kind, offset, limit] = args;
        checkRevision(next);
        if (!Number.isSafeInteger(offset) || offset < 0 || limit !== 50) throw Error('无效的分页');
        await Promise.resolve();
        checkSession(id); checkRevision(next);
        const matches = entries.filter(e => (kind === 'all' || e.kind === kind) && searchable(e).toLowerCase().includes(q.toLowerCase().trim()));
        return { entries: matches.slice(offset, offset + limit).map(summary), total: matches.length, offset };
      }
      case 'Detail': checkSession(args[0]); { const e = historyEntry(args[1]); return { ...titled(e), ...imageDetail(e) }; }
      case 'CopyImageText': {
        checkSession(args[0]);
        if (!imageDetail(historyEntry(args[1])).imageText) throw Error('这张图片没有可复制的文字');
        return { copied: false, pasted: false, message: '演示：已复制图片文字，未更改系统剪贴板' };
      }
      case 'SetImageText':
        if (storage.backend !== 'sqlite') throw Error('迁移到 SQLite 后可识别图片文字');
        storage = { ...storage, imageText: !!args[0] };
        return fresh();
      case 'Choose': checkSession(args[0]); historyEntry(args[1]); return { copied: false, pasted: false, message: args[2] ? '演示：内容已选中，未更改系统剪贴板' : '演示：已选择粘贴，未发送系统按键' };
      case 'Pause': paused = args[0]; return state();
      case 'Delete': checkSession(args[0]); historyEntry(args[1]); entries = entries.filter(e => e.id !== args[1]); return fresh();
      case 'Clear': entries = []; return fresh();
      case 'Permission': return accessible;
      case 'OpenPermission': return null;
      case 'ClipboardPermission': return clipboardAccess;
      case 'OpenClipboardPermission': return null;
      case 'RequestClipboardPermission':
        if (paused) throw Error('请先恢复记录');
        if (clipboardAccess === 'denied') throw Error('系统已拒绝读取');
        if (clipboardAccess === 'default') clipboardAccess = 'ask';
        return state();
      case 'Dismiss': return null;
      case 'Phrases': return phrases.filter(p => p.text.toLowerCase().includes(args[0].toLowerCase().trim())).map(p => ({ id: p.id, excerpt: excerpt(p.text) }));
      case 'PhraseDetail': return { ...phrase(args[0]) };
      case 'SavePhrase': {
        const [id, text] = args; checkPhrase(text);
        if (id) { phrase(id); phrases = phrases.map(p => p.id === id ? { id, text } : p); return { id, text }; }
        if (phrases.length >= 500) throw new Error('常用语最多 500 条');
        const added = { id: `phrase-${nextPhrase++}`, text }; phrases = [...phrases, added]; return { ...added };
      }
      case 'DeletePhrase': phrase(args[0]); phrases = phrases.filter(p => p.id !== args[0]); return null;
      case 'MovePhrase': {
        if (args[1] !== -1 && args[1] !== 1) throw new Error('无效的移动');
        const i = phrases.indexOf(phrase(args[0])), j = i + args[1];
        if (j >= 0 && j < phrases.length) { phrases = [...phrases]; [phrases[i], phrases[j]] = [phrases[j], phrases[i]]; }
        return null;
      }
      case 'InsertPhrase': checkSession(args[0]); phrase(args[1]); return { copied: false, pasted: false, message: args[2] ? '演示：内容已选中，未更改系统剪贴板' : '演示：已选择粘贴，未发送系统按键' };
      default: throw new Error('未知操作');
    }
  }};
}
