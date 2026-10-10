const now = Date.now();
const samples = [
  ['text', '好的设计，是让复杂的事轻轻发生。', '好的设计，是让复杂的事轻轻发生。\n\n复制时不必整理，\n需要时总能找到。\n\n把注意力留给正在做的事，\n剩下的，交给 Clipnest。', '备忘录'],
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
export function createDemo() {
  let entries = samples.map(([kind, title, text, source], i) => ({ id: `sample-${i}`, kind, title, text, source, excerpt: text.replaceAll('\n', ' '), created: now - i * 125000 }));
  let session = 1, paused = false;
  const summary = e => ({ ...e, text: undefined, image: undefined });
  const state = () => ({ session, entries: entries.map(summary), paused, accessible: true, target: '备忘录', platform: 'demo', notice: '', shortcut: '⌘ ⇧ V' });
  return { async call(method, ...args) {
    switch (method) {
      case 'State': return state();
      case 'Query': return entries.filter(e => (args[2] === 'all' || e.kind === args[2]) && (e.text + e.title + e.source).toLowerCase().includes(args[1].toLowerCase().trim())).map(summary);
      case 'Detail': return entries.find(e => e.id === args[1]);
      case 'Choose': return { copied: false, pasted: false, message: args[2] ? '演示：内容已选中，未更改系统剪贴板' : '演示：已选择粘贴，未发送系统按键' };
      case 'Pause': paused = args[0]; return state();
      case 'Delete': entries = entries.filter(e => e.id !== args[1]); return state();
      case 'Clear': entries = []; session++; return state();
      case 'Permission': return true;
      case 'OpenPermission': return null;
      case 'Dismiss': return null;
      default: throw new Error('未知操作');
    }
  }};
}
