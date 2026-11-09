import test from 'node:test';
import assert from 'node:assert/strict';
import { ROW_HEIGHT, ScrollRenderQueue, virtualRange } from './responsiveness.js';

test('scroll rendering coalesces into one frame and skips an unchanged visible window', () => {
  let scrollTop = 0, total = 1000, paints = 0, last;
  const callbacks = [];
  const queue = new ScrollRenderQueue({
    requestFrame: callback => { callbacks.push(callback); return callbacks.length; },
    measure: () => ({ ...virtualRange(total, scrollTop, 300), total }),
    paint: range => { paints++; last = range; }
  });

  queue.schedule(); queue.schedule(); queue.schedule();
  assert.equal(callbacks.length, 1, 'three scroll notifications share one animation frame');
  callbacks.shift()();
  assert.equal(paints, 1);

  queue.schedule();
  callbacks.shift()();
  assert.equal(paints, 1, 'an empty frame does not rebuild rows');

  scrollTop = 10;
  queue.schedule();
  callbacks.shift()();
  assert.equal(paints, 1, 'scrolling inside the painted window does not rebuild rows');

  scrollTop = ROW_HEIGHT * 4;
  queue.schedule();
  callbacks.shift()();
  assert.equal(paints, 2, 'crossing a virtualized edge paints the new window');
  assert.equal(last.start, 1);

  total = 999;
  queue.schedule();
  callbacks.shift()();
  assert.equal(paints, 3, 'a total change invalidates the painted window');
});
