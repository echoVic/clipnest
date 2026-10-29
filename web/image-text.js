// Recognized text is rendered only through textContent: it comes from a
// screenshot and may look like markup.
const notes = {
  pending: '等待识别，图片文字会在后台识别。',
  failed: '未能识别这张图片的文字。',
  disabled: '图片文字识别已关闭，可在「偏好设置 → 隐私」开启。',
  tooLarge: '图片超过 5000 万像素，不识别文字。',
  needsSQLite: '迁移到 SQLite 后可识别图片文字。',
};

// imageTextPreview returns the section shown below an image and, when there is
// text, a copy button for the preview's fixed actions, so the action stays in
// view while long text scrolls. It returns null when there is nothing to say:
// other systems explain the feature in preferences. copy receives the button.
export function imageTextPreview(document, e, copy) {
  if (e.kind !== 'image' || !e.imageTextState || e.imageTextState === 'unsupported') return null;
  const node = (tag, className, text) => {
    const element = document.createElement(tag);
    element.className = className;
    if (text !== undefined) element.textContent = text;
    return element;
  };
  const section = node('section', 'image-text');
  section.setAttribute('aria-label', '图片文字');
  section.append(node('span', 'eyebrow', '图片文字'));
  if (e.imageTextState !== 'ready' || !e.imageText) {
    section.append(node('p', 'image-text-note', e.imageTextState === 'ready' ? '未识别到文字。' : notes[e.imageTextState] || '无法确认识别状态。'));
    return { section, button: null };
  }
  section.append(node('p', 'image-text-body', e.imageText));
  const button = node('button', 'secondary image-text-copy', '复制文字');
  button.type = 'button';
  button.onclick = () => copy(button);
  return { section, button };
}

// imageTextSetting describes the privacy switch for a panel state.
export function imageTextSetting(state, demo) {
  const storage = state.storage || {};
  if (!demo && state.platform !== 'darwin') return { available: false, checked: false, note: '图片文字识别仅适用于 macOS。' };
  if (storage.backend !== 'sqlite') return { available: false, checked: false, note: '迁移到 SQLite 后可识别图片文字。' };
  return {
    available: true, checked: !!storage.imageText,
    note: demo ? '演示仅切换示例截图的文字，不调用系统识别。' : '在本机识别截图中的文字，不联网，可搜索和复制。关闭后删除已识别的文字。',
  };
}
