import test from 'node:test';
import assert from 'node:assert/strict';
import { DeferredPreview } from './responsiveness.js';

function fakeTimers() {
  let next = 1;
  const active = new Map();
  return {
    active,
    setTimer(run, delay) { const id = next++; active.set(id, { run, delay }); return id; },
    clearTimer(id) { active.delete(id); },
    fire(id = active.keys().next().value) { const timer = active.get(id); active.delete(id); timer?.run(); }
  };
}

test('wheel preview waits for the gesture to settle and keeps only the latest selection', () => {
  const timers = fakeTimers(), seen = [];
  const preview = new DeferredPreview({ setTimer: timers.setTimer, clearTimer: timers.clearTimer, delay: 180 });
  preview.schedule(() => seen.push('first'));
  preview.schedule(() => seen.push('second'));
  assert.deepEqual(seen, [], 'moving the selection does not paint the detail preview yet');
  assert.equal(timers.active.size, 1, 'continuous wheel movement keeps one trailing timer');
  timers.fire();
  assert.deepEqual(seen, ['second'], 'only the settled selection is previewed');
  assert.equal(preview.pending, false);
});

test('immediate selection previews now and prevents stale wheel work from winning', () => {
  const timers = fakeTimers(), seen = [];
  const preview = new DeferredPreview({ setTimer: timers.setTimer, clearTimer: timers.clearTimer, delay: 180 });
  preview.schedule(() => seen.push('stale wheel'));
  const stale = [...timers.active.values()][0].run;
  preview.flush(() => seen.push('keyboard'));
  assert.deepEqual(seen, ['keyboard'], 'keyboard and click selections do not wait');
  stale();
  assert.deepEqual(seen, ['keyboard'], 'a cleared wheel timer cannot repaint stale content');
});

test('cancelling pending preview work also drops its trailing render', () => {
  const timers = fakeTimers(), seen = [];
  const preview = new DeferredPreview({ setTimer: timers.setTimer, clearTimer: timers.clearTimer, delay: 180 });
  preview.schedule(() => seen.push('wheel'));
  const stale = [...timers.active.values()][0].run;
  preview.cancel();
  stale();
  assert.deepEqual(seen, []);
  assert.equal(preview.pending, false);
});
