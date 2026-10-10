// One source of truth for keyboard, wheel and pointer selection.
export function moveIndex(index, delta, length) {
  return length ? Math.max(0, Math.min(length - 1, index + delta)) : -1;
}
export function numberIndex(key, length) {
  return /^[1-9]$/.test(key) && Number(key) <= length ? Number(key) - 1 : -1;
}
export class WheelSelection {
  constructor() { this.reset(); }
  reset() { this.sum = 0; this.last = -Infinity; this.stepAt = -Infinity; this.direction = 0; }
  consume(delta, mode, now) {
    if (!Number.isFinite(delta) || delta === 0) return 0;
    const direction = Math.sign(delta);
    if (now - this.last > 180 || direction !== this.direction) this.sum = 0;
    this.direction = direction;
    this.last = now;
    this.sum += delta * (mode === 1 ? 24 : mode === 2 ? 100 : 1);
    if (Math.abs(this.sum) < 44 || now - this.stepAt < 110) return 0;
    this.sum = 0;
    this.stepAt = now;
    return direction;
  }
}
