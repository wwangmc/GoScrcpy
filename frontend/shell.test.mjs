import {test} from 'node:test';
import assert from 'node:assert/strict';
import {initializeShell,shellCall} from './shell.js';

function fixture(t,{result={missingOpenSSH:true},storeError=false}={}){
  const old={document:globalThis.document,window:globalThis.window};t.after(()=>Object.assign(globalThis,old));
  const nodes=new Map();
  for(const id of ['openssh-dialog','openssh-sileo','openssh-cydia','openssh-close','openssh-status'])nodes.set(id,{open:false,disabled:false,textContent:'',addEventListener(){},showModal(){this.open=true;},close(){this.open=false;}});
  globalThis.document={getElementById:id=>nodes.get(id)};
  const calls=[],notices=[];
  globalThis.window={go:{main:{App:{OpenShell:async serial=>{calls.push(['open',serial]);return result;},OpenOpenSSHStore:async(serial,store)=>{calls.push(['store',serial,store]);if(storeError)throw new Error('store unavailable');}}}}};
  return {nodes,calls,notices,panel:initializeShell({notice:(...args)=>notices.push(args)})};
}
test('missing OpenSSH offers installation, close sends no phone command',async t=>{
  const f=fixture(t);await f.panel.open('iphone-one');assert.equal(f.nodes.get('openssh-dialog').open,true);
  f.nodes.get('openssh-close').onclick();assert.equal(f.nodes.get('openssh-dialog').open,false);assert.deepEqual(f.calls,[['open','iphone-one']]);
});
test('both OpenSSH stores use the selected phone',async t=>{
  const f=fixture(t);
  for(const store of ['sileo','cydia']){
    await f.panel.open('iphone-two');await f.nodes.get(`openssh-${store}`).onclick();
    assert.deepEqual(f.calls.at(-1),['store','iphone-two',store]);assert.equal(f.nodes.get('openssh-dialog').open,false);
  }
});
test('installed but stopped SSH remains distinct from missing SSH',async t=>{
  const f=fixture(t,{result:{missingOpenSSH:false,warning:'SSH installed, port stopped'}});await f.panel.open('iphone-one');
  assert.equal(f.nodes.get('openssh-dialog').open,false);assert.equal(f.notices.at(-1)[0],'SSH installed, port stopped');
});
test('store failure keeps retry and close available',async t=>{
  const f=fixture(t,{storeError:true});await f.panel.open('iphone-one');await f.nodes.get('openssh-sileo').onclick();
  assert.equal(f.nodes.get('openssh-dialog').open,true);assert.equal(f.nodes.get('openssh-close').disabled,false);assert.match(f.nodes.get('openssh-status').textContent,/store unavailable/);
});
test('web shell calls carry only the target serial and explicit store',async t=>{
  const old={window:globalThis.window,fetch:globalThis.fetch,sessionStorage:globalThis.sessionStorage};t.after(()=>Object.assign(globalThis,old));
  globalThis.window={};globalThis.sessionStorage={getItem:()=> 'test-token'};
  let body;globalThis.fetch=async(_,args)=>{body=JSON.parse(args.body);return {ok:true,json:async()=>({})};};
  await shellCall('OpenOpenSSHStore','iphone-one','cydia');assert.deepEqual(body,{method:'OpenOpenSSHStore',serial:'iphone-one',store:'cydia'});
});
