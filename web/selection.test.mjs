import test from 'node:test';
import assert from 'node:assert/strict';
import { moveIndex, numberIndex, WheelSelection } from './selection.js';
test('selection clamps at boundaries and empty history', () => {
 assert.equal(moveIndex(0,-1,10),0); assert.equal(moveIndex(9,1,10),9); assert.equal(moveIndex(0,1,0),-1);
 assert.equal(numberIndex('9',9),8); assert.equal(numberIndex('9',8),-1); assert.equal(numberIndex('0',10),-1);
});
test('trackpad accumulation, rate limiting, direction changes and line wheel', () => {
 const wheel=new WheelSelection();
 assert.equal(wheel.consume(15,0,0),0);assert.equal(wheel.consume(30,0,20),1);
 assert.equal(wheel.consume(100,0,25),0);assert.equal(wheel.consume(-44,0,150),-1);
 wheel.reset(); assert.equal(wheel.consume(3,1,1),1); assert.equal(wheel.consume(1,0,500),0);
 assert.equal(wheel.consume(NaN,0,600),0);
});
