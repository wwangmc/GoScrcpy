import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {initializeIOSSettings,iosSettingsCall} from './ios-settings.js';

function fixture(t,{fail=false}={}){
  const old={window:globalThis.window,document:globalThis.document,Option:globalThis.Option};t.after(()=>Object.assign(globalThis,old));
  const nodes=new Map();
  for(const [,id] of readFileSync(new URL('./index.html',import.meta.url),'utf8').matchAll(/id="([^"]+)"/g))nodes.set(id,{value:'',textContent:'',hidden:false,disabled:false,open:false,listeners:new Map(),classList:{toggle(){}},addEventListener(name,fn){this.listeners.set(name,fn);},showModal(){this.open=true;},close(){this.open=false;},replaceChildren(...children){this.value=children[0]?.value||'';}});
  globalThis.document={getElementById:id=>{assert.ok(nodes.has(id));return nodes.get(id);}};
  globalThis.Option=class{constructor(text,value){this.text=text;this.value=value;}};
  const calls=[];
  globalThis.window={go:{main:{App:{
    GetIOSSettings:async serial=>{calls.push(['load',serial]);return {region:'JP',choices:[{region:'US',name:'US',language:'en-US',locale:'en_US',timeZone:'America/New_York'},{region:'JP',name:'Japan',language:'ja-JP',locale:'ja_JP',timeZone:'Asia/Tokyo'}]};},
    RememberIOSSettingsRegion:async(serial,region)=>{calls.push(['remember',serial,region]);},
    ApplyIOSSettings:async(serial,region)=>{calls.push(['apply',serial,region]);if(fail)throw new Error('uncertain response');return 'settings accepted';},
  }}}};
  return {nodes,calls,panel:initializeIOSSettings({notice:()=>{}})};
}
test('one-click settings do not open or call backend for Android',async t=>{
  const f=fixture(t);await f.panel.open({serial:'android',platform:'android'});
  assert.equal(f.nodes.get('ios-settings-dialog').open,false);assert.deepEqual(f.calls,[]);
});
test('remembered iOS region populates preview and full apply uses the selected phone',async t=>{
  const f=fixture(t);await f.panel.open({serial:'iphone-one',platform:'ios',model:'iPhone'});
  assert.equal(f.nodes.get('ios-settings-region').value,'JP');assert.equal(f.nodes.get('ios-settings-language').textContent,'ja-JP');assert.equal(f.nodes.get('ios-settings-timezone').textContent,'Asia/Tokyo');
  f.nodes.get('ios-settings-region').value='US';f.nodes.get('ios-settings-region').listeners.get('change')();
  await f.nodes.get('ios-settings-apply').onclick();
  assert.deepEqual(f.calls,[['load','iphone-one'],['remember','iphone-one','US'],['apply','iphone-one','US']]);
  assert.equal(f.nodes.get('ios-settings-status').textContent,'settings accepted');assert.equal(f.nodes.get('ios-settings-close').disabled,false);
  assert.equal(f.nodes.has('ios-settings-carriers'),false);
});
test('uncertain settings requests show failure and are not retried',async t=>{
  const f=fixture(t,{fail:true});await f.panel.open({serial:'iphone-one',platform:'ios'});await f.nodes.get('ios-settings-apply').onclick();
  assert.equal(f.calls.filter(c=>c[0]==='apply').length,1);assert.match(f.nodes.get('ios-settings-status').textContent,/uncertain response/);assert.equal(f.nodes.get('ios-settings-apply').disabled,false);
});
test('browser request sends region as data',async t=>{
  const old={window:globalThis.window,fetch:globalThis.fetch,sessionStorage:globalThis.sessionStorage};t.after(()=>Object.assign(globalThis,old));
  globalThis.window={};globalThis.sessionStorage={getItem:()=> 'test-token'};let body;
  globalThis.fetch=async(_,args)=>{body=JSON.parse(args.body);return {ok:true,json:async()=> 'ok'};};
  await iosSettingsCall('ApplyIOSSettings','iphone-one','JP');assert.deepEqual(body,{method:'ApplyIOSSettings',serial:'iphone-one',region:'JP'});
});
