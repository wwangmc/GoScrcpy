import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {initializeProxy,proxyCall} from './proxy.js';

function fixture(t,{platform='ios',query=async()=>({missingCurl:true}),store=async()=>{}}={}){
  const previous={document:globalThis.document,window:globalThis.window,Option:globalThis.Option};
  const nodes=new Map();
  for(const [,id] of readFileSync(new URL('./index.html',import.meta.url),'utf8').matchAll(/id="([^"]+)"/g)){
    nodes.set(id,{value:'',textContent:'',hidden:false,disabled:false,open:false,classList:{toggle(){}},addEventListener(){},focus(){},showModal(){this.open=true;},close(){this.open=false;},
      replaceChildren(...children){this.children=children;this.value=children[0]?.value||'';},add(child){this.children.push(child);if(this.children.length===1)this.value=child.value;}});
  }
  globalThis.document={getElementById:id=>{assert.ok(nodes.has(id),`missing markup: ${id}`);return nodes.get(id);}};
  globalThis.Option=class{constructor(text,value){this.text=text;this.value=value;}};
  const calls=[];
  globalThis.window={go:{main:{App:{
    GetProxyOptions:async()=>({proxyId:'line',region:'US',appId:'com.mobile.legends',name:'cs'}),
    LoadProxyLines:async()=>({lines:[{id:'line',name:'test',hostname:'proxy.example',port:9000}],skipped:0}),
    QueryProxyIP:query,
    OpenIOSCurlStore:async(serial,choice)=>{calls.push([serial,choice]);await store();},
  }}}};
  t.after(()=>{Object.assign(globalThis,previous);});
  const panel=initializeProxy({notice:()=>{}});
  return {nodes,calls,open:()=>panel.open({serial:'phone-one',platform,model:'test phone'})};
}

test('iOS missing curl offers stores, close does not command phone',async t=>{
  const f=fixture(t);await f.open();await f.nodes.get('proxy-query').onclick();
  assert.equal(f.nodes.get('ios-curl-dialog').open,true);
  assert.equal(f.nodes.get('proxy-ip').hidden,true);
  f.nodes.get('ios-curl-close').onclick();
  assert.equal(f.nodes.get('ios-curl-dialog').open,false);
  assert.equal(f.nodes.get('proxy-dialog').open,true);
  assert.deepEqual(f.calls,[]);
});

test('each store targets the same phone and returns to mirroring',async t=>{
  const f=fixture(t);
  for(const choice of ['sileo','cydia']){
    await f.open();await f.nodes.get('proxy-query').onclick();
    await f.nodes.get(`ios-curl-${choice}`).onclick();
    assert.deepEqual(f.calls.at(-1),['phone-one',choice]);
    assert.equal(f.nodes.get('ios-curl-dialog').open,false);
    assert.equal(f.nodes.get('proxy-dialog').open,false);
  }
});

test('store errors retain dialog and allow another choice',async t=>{
  const f=fixture(t,{store:async()=>{throw new Error('store unavailable');}});await f.open();await f.nodes.get('proxy-query').onclick();
  await f.nodes.get('ios-curl-sileo').onclick();
  assert.equal(f.nodes.get('ios-curl-dialog').open,true);
  assert.match(f.nodes.get('ios-curl-status').textContent,/store unavailable/);
  assert.equal(f.nodes.get('ios-curl-close').disabled,false);
});

test('ordinary iOS query failures do not open store chooser',async t=>{
  const f=fixture(t,{query:async()=>{throw new Error('network timeout');}});await f.open();await f.nodes.get('proxy-query').onclick();
  assert.equal(f.nodes.get('ios-curl-dialog').open,false);
  assert.match(f.nodes.get('proxy-status').textContent,/network timeout/);
});

test('Android queries keep the curl store chooser hidden',async t=>{
  const f=fixture(t,{platform:'android',query:async()=>({query:'203.0.113.7'})});await f.open();await f.nodes.get('proxy-query').onclick();
  assert.equal(f.nodes.get('ios-curl-dialog').open,false);
  assert.equal(f.nodes.get('proxy-ip-address').textContent,'203.0.113.7');
});

test('browser store request uses a store field instead of proxy options',async t=>{
  const old={window:globalThis.window,fetch:globalThis.fetch,sessionStorage:globalThis.sessionStorage};
  t.after(()=>Object.assign(globalThis,old));globalThis.window={};globalThis.sessionStorage={getItem:()=> 'test-token'};
  let payload;globalThis.fetch=async(_url,args)=>{payload=JSON.parse(args.body);return {ok:true,json:async()=>null};};
  await proxyCall('OpenIOSCurlStore','phone-one','sileo');
  assert.deepEqual(payload,{method:'OpenIOSCurlStore',serial:'phone-one',store:'sileo'});
});
