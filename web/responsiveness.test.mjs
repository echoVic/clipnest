import test from 'node:test';
import assert from 'node:assert/strict';
import { DetailCache, updateSelection, characterCount } from './responsiveness.js';

const deferred = () => { let resolve, reject; const promise = new Promise((a,b) => { resolve=a; reject=b; }); return {promise,resolve,reject}; };

test('detail requests coalesce, failures retry, and panel generations do not share content', async () => {
  const cache = new DetailCache(2), old = deferred(), current = deferred();
  let calls=0;
  const a=cache.get('same',()=>{calls++;return old.promise;});
  assert.equal(cache.get('same',()=>{throw Error('duplicate');}),a);
  await Promise.resolve(); assert.equal(calls,1);
  cache.clear();
  const b=cache.get('same',()=>current.promise);
  old.reject(Error('old panel')); await assert.rejects(a,/old panel/);
  assert.equal(cache.get('same',()=>{throw Error('stale eviction');}),b);
  current.resolve({text:'current'});assert.deepEqual(await b,{text:'current'});
  assert.deepEqual(await cache.get('same',()=>{throw Error('cache miss');}),{text:'current'});
  const failed=cache.get('bad',()=>Promise.reject(Error('failure')));
  await assert.rejects(failed,/failure/);
  assert.equal(await cache.get('bad',()=>42),42);
});

test('detail cache evicts least recently used entries and remains bounded',async()=>{
 const cache=new DetailCache(2);
 await cache.get('a',()=>1);await cache.get('b',()=>2);await cache.get('a',()=>99);await cache.get('c',()=>3);
 let calls=0;assert.equal(await cache.get('b',()=>{calls++;return 4;}),4);assert.equal(calls,1);assert.equal(cache.items.size,2);
});

test('selection touches only old and new rows with unchanged aria semantics',()=>{
 const trace=[];
 const rows=Array.from({length:500},(_,i)=>({classList:{toggle:(name,on)=>trace.push([i,name,on])},setAttribute:(name,on)=>trace.push([i,name,on])}));
 updateSelection(rows,1,498);
 assert.deepEqual(trace,[[1,'selected',false],[1,'aria-selected',false],[498,'selected',true],[498,'aria-selected',true]]);
 trace.length=0;updateSelection(rows,-1,-1);assert.deepEqual(trace,[]);
});

test('character count matches Unicode code points without a full array',()=>{
 for(const s of ['', 'ASCII','中文','a😀𝄞\u0301','\ud800']) assert.equal(characterCount(s),[...s].length);
});
