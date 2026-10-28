// Zero-dependency Markdown renderer for the preview pane.
// Block-level parsing runs first, then inline formatting. Text content is
// assigned via textContent so any HTML in the source is escaped; raw HTML
// tags are not supported. URLs are restricted to safe schemes.

// Heuristics: returns true when the text contains enough Markdown syntax that
// rendering it as Markdown is more useful than showing the raw source.
export function looksLikeMarkdown(text) {
  if (!text) return false;
  const lines = text.split('\n');
  for (const line of lines) {
    if (/^#{1,6}\s/.test(line)) return true;
    if (/^>\s?/.test(line)) return true;
    if (/^[-*+]\s+/.test(line)) return true;
    if (/^\d+\.\s+/.test(line)) return true;
    if (/^(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) return true;
  }
  if (/```/.test(text)) return true;
  if (/\[([^\]]+)\]\(([^)]+)\)/.test(text)) return true;
  if (/!\[([^\]]*)\]\(([^)]+)\)/.test(text)) return true;
  if (/\*\*[^*]+\*\*|__[^_]+__/.test(text)) return true;
  if (/~~[^~]+~~/.test(text)) return true;
  return false;
}

const SAFE_URL = /^(https?:|mailto:|\/|#)/i;

function safeUrl(url) {
  const u = url.trim();
  if (SAFE_URL.test(u)) return u;
  return '';
}

// Inline formatter: returns a DocumentFragment with styled nodes.
// Processes: code spans, images, links, bold, italic, strikethrough.
function renderInline(text, out) {
  let i = 0;
  let buf = '';
  const flush = () => {
    if (buf) { out.append(document.createTextNode(buf)); buf = ''; }
  };
  while (i < text.length) {
    const ch = text[i];

    // Inline code: `code` (no nesting, spans to next backtick).
    if (ch === '`') {
      const end = text.indexOf('`', i + 1);
      if (end !== -1) {
        flush();
        const code = node('code', '', text.slice(i + 1, end));
        out.append(code);
        i = end + 1;
        continue;
      }
    }

    // Image: ![alt](url)
    if (ch === '!' && text[i + 1] === '[') {
      const m = /^!\[([^\]]*)\]\(([^)]+)\)/.exec(text.slice(i));
      if (m) {
        flush();
        const src = safeUrl(m[2]);
        if (src) {
          const img = document.createElement('img');
          img.alt = m[1];
          img.src = src;
          out.append(img);
        } else {
          out.append(document.createTextNode(m[1]));
        }
        i += m[0].length;
        continue;
      }
    }

    // Link: [text](url)
    if (ch === '[') {
      const m = /^\[([^\]]+)\]\(([^)]+)\)/.exec(text.slice(i));
      if (m) {
        flush();
        const href = safeUrl(m[2]);
        if (href) {
          const a = document.createElement('a');
          a.href = href;
          a.target = '_blank';
          a.rel = 'noopener noreferrer';
          a.textContent = m[1];
          out.append(a);
        } else {
          out.append(document.createTextNode(m[1]));
        }
        i += m[0].length;
        continue;
      }
    }

    // Bold: **text** or __text__
    if ((ch === '*' && text[i + 1] === '*') || (ch === '_' && text[i + 1] === '_')) {
      const marker = ch;
      const rest = text.slice(i + 2);
      const close = rest.indexOf(marker + marker);
      if (close !== -1) {
        flush();
        const strong = document.createElement('strong');
        renderInline(rest.slice(0, close), strong);
        out.append(strong);
        i += 2 + close + 2;
        continue;
      }
    }

    // Italic: *text* or _text_
    if ((ch === '*' || ch === '_') && text[i + 1] !== ch) {
      const marker = ch;
      const rest = text.slice(i + 1);
      const close = rest.indexOf(marker);
      if (close > 0) {
        flush();
        const em = document.createElement('em');
        renderInline(rest.slice(0, close), em);
        out.append(em);
        i += 1 + close + 1;
        continue;
      }
    }

    // Strikethrough: ~~text~~
    if (ch === '~' && text[i + 1] === '~') {
      const rest = text.slice(i + 2);
      const close = rest.indexOf('~~');
      if (close !== -1) {
        flush();
        const s = document.createElement('s');
        renderInline(rest.slice(0, close), s);
        out.append(s);
        i += 2 + close + 2;
        continue;
      }
    }

    buf += ch;
    i++;
  }
  flush();
  return out;
}

function node(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}

// Parse the source into block elements and append them to the container.
export function renderMarkdown(text, container) {
  const out = container || document.createDocumentFragment();
  const lines = text.replace(/\r\n?/g, '\n').split('\n');
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    // Blank line: skip.
    if (line.trim() === '') { i++; continue; }

    // Fenced code block.
    const fence = /^```(\w*)\s*$/.exec(line);
    if (fence) {
      const lang = fence[1];
      const codeLines = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i])) {
        codeLines.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++; // skip closing fence
      const pre = node('pre', 'md-code');
      const code = node('code', lang ? `lang-${lang}` : '');
      code.textContent = codeLines.join('\n');
      pre.append(code);
      out.append(pre);
      continue;
    }

    // Heading.
    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      const level = heading[1].length;
      const h = node(`h${level}`, '');
      renderInline(heading[2], h);
      out.append(h);
      i++;
      continue;
    }

    // Horizontal rule.
    if (/^(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
      out.append(node('hr'));
      i++;
      continue;
    }

    // Blockquote: collect consecutive > lines.
    if (/^>\s?/.test(line)) {
      const quoteLines = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) {
        quoteLines.push(lines[i].replace(/^>\s?/, ''));
        i++;
      }
      const bq = node('blockquote');
      renderMarkdown(quoteLines.join('\n'), bq);
      out.append(bq);
      continue;
    }

    // Unordered list.
    if (/^[-*+]\s+/.test(line)) {
      const items = [];
      while (i < lines.length && /^[-*+]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^[-*+]\s+/, ''));
        i++;
      }
      const ul = node('ul');
      for (const item of items) {
        const li = node('li');
        renderInline(item, li);
        ul.append(li);
      }
      out.append(ul);
      continue;
    }

    // Ordered list.
    if (/^\d+\.\s+/.test(line)) {
      const items = [];
      while (i < lines.length && /^\d+\.\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\d+\.\s+/, ''));
        i++;
      }
      const ol = node('ol');
      for (const item of items) {
        const li = node('li');
        renderInline(item, li);
        ol.append(li);
      }
      out.append(ol);
      continue;
    }

    // Paragraph: collect consecutive non-empty, non-special lines.
    const paraLines = [];
    while (i < lines.length && lines[i].trim() !== '' &&
           !/^```/.test(lines[i]) && !/^#{1,6}\s/.test(lines[i]) &&
           !/^>\s?/.test(lines[i]) && !/^[-*+]\s+/.test(lines[i]) &&
           !/^\d+\.\s+/.test(lines[i]) &&
           !/^(-{3,}|\*{3,}|_{3,})\s*$/.test(lines[i])) {
      paraLines.push(lines[i]);
      i++;
    }
    // A line resembling a block can fail its stricter opener (e.g. a
    // malformed fence). Render it as text and guarantee forward progress.
    if (paraLines.length === 0) { paraLines.push(lines[i]); i++; }
    const p = node('p');
    renderInline(paraLines.join('\n'), p);
    out.append(p);
  }

  return out;
}
