import {test} from 'node:test';
import assert from 'node:assert/strict';
import {installFileDrop} from './drop.js';

function fixture(native=true){
  const listeners=new Map(),hints=[],uploads=[];
  const element=()=>({hidden:true,dataset:{},children:new Map(),append(){},remove(){},querySelector(selector){if(!this.children.has(selector))this.children.set(selector,{});return this.children.get(selector);}});
  let callback,registrations=0,active=true;
  const target={addEventListener:(type,fn)=>listeners.set(type,fn),removeEventListener:type=>listeners.delete(type)};
  if(native)target.runtime={OnFileDrop:(fn,useTarget)=>{assert.equal(useTarget,false);callback=fn;registrations++;},OnFileDropOff:()=>{}};
  const doc={createElement:element,body:{append(){}}};
  const controller=installFileDrop({target,doc,onHint:value=>hints.push(value)});
  controller.setTarget(paths=>uploads.push(paths),()=>({allowed:active,title:active?'Upload':'Busy',detail:'/target'}));
  return {controller,hints,uploads,listeners,drop:(...args)=>callback(...args),registrations:()=>registrations,busy:()=>active=false};
}
function event(types=['Files']){return {dataTransfer:{types,dropEffect:''},prevented:false,stopped:false,preventDefault(){this.prevented=true;},stopImmediatePropagation(){this.stopped=true;}};}

test('file drops prevent WebView downloads and deliver native paths once',async()=>{
  const f=fixture(),over=event(),drop=event();
  f.listeners.get('dragover')(over);f.listeners.get('drop')(drop);
  assert.ok(over.prevented&&drop.prevented);assert.equal(over.dataTransfer.dropEffect,'copy');
  assert.equal(drop.stopped,false,'native runtime must still receive File objects');
  f.drop(10,20,['C:\\apps\\测试.exe','C:\\apps\\two.bin']);
  await Promise.resolve();
  assert.deepEqual(f.uploads,[['C:\\apps\\测试.exe','C:\\apps\\two.bin']]);assert.equal(f.registrations(),1);
});
test('busy/unsupported drops never navigate, download or start a second upload',async()=>{
  const f=fixture();f.busy();const e=event();f.listeners.get('drop')(e);f.drop(1,2,['C:\\file.exe']);
  await Promise.resolve();assert.ok(e.prevented);assert.equal(f.uploads.length,0);assert.deepEqual(f.hints,['Busy']);
  const link=event(['text/uri-list']);f.listeners.get('drop')(link);assert.ok(link.prevented&&link.stopped);assert.equal(f.uploads.length,0);
  const browser=fixture(false),external=event();browser.listeners.get('drop')(external);assert.ok(external.prevented);assert.equal(browser.uploads.length,0);assert.ok(browser.hints[0].includes('桌面'));
});
test('missing native file paths are rejected rather than guessed from names',async()=>{
  const f=fixture();f.drop(0,0,['']);await Promise.resolve();assert.equal(f.uploads.length,0);assert.ok(f.hints[0].includes('路径'));
  f.controller.dispose();assert.equal(f.listeners.size,0);
});
