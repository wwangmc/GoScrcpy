import test from 'node:test';
import assert from 'node:assert/strict';
import {runDeviceTool} from './tools.js';

function globals(t,window,navigator={}){
  for(const [key,value] of Object.entries({window,navigator})){
    const previous=Object.getOwnPropertyDescriptor(globalThis,key);
    Object.defineProperty(globalThis,key,{value,configurable:true});
    t.after(()=>{if(previous)Object.defineProperty(globalThis,key,previous);else delete globalThis[key];});
  }
}

test('store reads the desktop clipboard through Wails and sends it to the selected phone',async t=>{
  globals(t,{runtime:{ClipboardGetText:async()=> '包名：com.chaos.drink.sort'}});
  const calls=[];
  await runDeviceTool('store','phone-one',async(...args)=>calls.push(args));
  assert.deepEqual(calls,[['DeviceTool','phone-one','store','包名：com.chaos.drink.sort']]);
});

test('browser store uses clipboard permission during the click',async t=>{
  globals(t,{}, {clipboard:{readText:async()=> 'com.example.app'}});
  const calls=[];
  await runDeviceTool('store','phone-two',async(...args)=>calls.push(args));
  assert.deepEqual(calls,[['DeviceTool','phone-two','store','com.example.app']]);
});

test('empty or denied clipboard never invokes the backend',async t=>{
  globals(t,{runtime:{ClipboardGetText:async()=> '  '}});
  const call=()=>assert.fail('must not invoke backend');
  await assert.rejects(runDeviceTool('store','phone',call),/没有文字/);
  window.runtime.ClipboardGetText=async()=>{throw new Error('denied');};
  await assert.rejects(runDeviceTool('store','phone',call),/无法读取电脑剪贴板/);
});

test('package query does not read clipboard or change the device action',async t=>{
  globals(t,{runtime:{ClipboardGetText:()=>assert.fail('must not read clipboard')}});
  const calls=[];
  assert.equal(await runDeviceTool('package','phone',async(...args)=>{calls.push(args);return 'com.foreground.app';}),'com.foreground.app');
  assert.deepEqual(calls,[['DeviceTool','phone','package','']]);
});
