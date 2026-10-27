import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {setImmediate} from 'node:timers/promises';
import {initializeFileManager} from './files.js';

async function fixture(t,{platform='ios',remove=async()=>{},paste=async()=>{}}={}){
  const old={window:globalThis.window,document:globalThis.document};t.after(()=>Object.assign(globalThis,old));
  class Element{
    constructor(){this.value='';this.textContent='';this.hidden=false;this.disabled=false;this.open=false;this.children=[];this.dataset={};this.style={};this.listeners=new Map();this.parts=new Map();this.classList={toggle(){}};}
    append(...children){this.children.push(...children);}
    replaceChildren(...children){this.children=[...children];}
    setAttribute(){}
    addEventListener(event,fn){this.listeners.set(event,fn);}
    querySelector(selector){if(!this.parts.has(selector))this.parts.set(selector,new Element());return this.parts.get(selector);}
    querySelectorAll(){return this.children;}
    showModal(){this.open=true;}
    close(){this.open=false;}
    focus(){document.activeElement=this;}
    select(){}
    contains(target){return target===this||this.children.includes(target);}
  }
  const nodes=new Map([...readFileSync(new URL('./index.html',import.meta.url),'utf8').matchAll(/id="([^"]+)"/g)].map(([,id])=>[id,new Element()]));
  const docListeners=new Map(),windowListeners=new Map(),calls=[];
  globalThis.document={getElementById:id=>{assert.ok(nodes.has(id),id);return nodes.get(id);},createElement:()=>new Element(),querySelectorAll:()=>[],addEventListener:(name,fn)=>docListeners.set(name,fn)};
  const entries=[{name:'first.txt',path:'/Documents/first.txt',kind:'file',size:20},{name:'folder',path:'/Documents/folder',kind:'directory',size:0},{name:'link',path:'/Documents/link',kind:'link',size:0}];
  globalThis.window={innerWidth:900,innerHeight:700,addEventListener:(name,fn)=>windowListeners.set(name,fn),go:{main:{App:{
    GetDevices:async()=>[{serial:'phone-one',platform,model:'Phone'}],
    GetFileLocation:async()=>null,
    BrowseFiles:async(...args)=>{calls.push(['browse',...args]);return {platform,scope:platform==='ios'?'media':'storage',path:args[2]||'/Documents',entries};},
    DeleteFile:async(...args)=>{calls.push(['delete',...args]);await remove(...args);},
    PasteFile:async(...args)=>{calls.push(['paste',...args]);await paste(...args);},
  }}}};
  await initializeFileManager('phone-one');
  const node=id=>nodes.get(id),rows=()=>node('files-entries').children;
  const rightClick=name=>rows().find(row=>row.dataset.path==='/Documents/'+name).oncontextmenu({preventDefault(){},clientX:10,clientY:20});
  const submit=()=>node('files-delete-form').onsubmit({preventDefault(){}});
  const browse=async path=>{node('files-path').value=path;node('files-path-form').onsubmit({preventDefault(){}});await setImmediate();};
  return {node,rows,calls,rightClick,submit,windowListeners,browse};
}

test('right-click targets the clicked item; cancel never deletes',async t=>{
  const f=await fixture(t);f.rows()[1].onclick();f.rightClick('folder');
  assert.equal(f.rows()[0].dataset.path,'/Documents/folder');
  assert.equal(f.node('files-menu-download').disabled,true);
  f.node('files-menu-delete').onclick();
  assert.equal(f.node('files-delete-dialog').open,true);
  assert.equal(f.node('files-delete-path').textContent,'/Documents/folder');
  assert.match(f.node('files-delete-warning').textContent,/所有内容/);
  assert.equal(document.activeElement,f.node('files-delete-cancel'));
  assert.equal(f.calls.filter(c=>c[0]==='delete').length,0);
  f.node('files-delete-cancel').onclick();
  assert.equal(f.node('files-delete-dialog').open,false);
  assert.equal(f.calls.filter(c=>c[0]==='delete').length,0);
});

test('confirmed deletion uses the selected phone and scope, prevents duplicate submits and refreshes',async t=>{
  let finish;const pending=new Promise(resolve=>{finish=resolve;});
  const f=await fixture(t,{platform:'android',remove:()=>pending});
  f.rightClick('folder');f.node('files-menu-delete').onclick();const deletion=f.submit();
  assert.equal(f.node('files-delete-submit').disabled,true);
  assert.equal(f.node('files-delete-cancel').disabled,true);
  f.node('files-delete-close').onclick();assert.equal(f.node('files-delete-dialog').open,true);
  await f.submit();assert.deepEqual(f.calls.filter(c=>c[0]==='delete'),[['delete','phone-one','storage','/Documents/folder']]);
  finish();await deletion;
  assert.equal(f.node('files-delete-dialog').open,false);
  assert.equal(f.calls.filter(c=>c[0]==='browse').length,2);
});

test('failure remains visible without automatic retry and closing refreshes partial deletions',async t=>{
  const f=await fixture(t,{remove:async()=>{throw new Error('permission denied');}});
  f.rightClick('first.txt');f.node('files-menu-delete').onclick();await f.submit();
  assert.match(f.node('files-delete-error').textContent,/permission denied/);
  assert.equal(f.node('files-delete-dialog').open,true);
  assert.equal(f.node('files-delete-submit').disabled,false);
  assert.deepEqual(f.calls.filter(c=>c[0]==='delete'),[['delete','phone-one','media','/Documents/first.txt']]);
  f.node('files-delete-cancel').onclick();
  assert.equal(f.calls.filter(c=>c[0]==='browse').length,2);
});

test('symlink deletion explains target preservation; Escape cancels confirmation',async t=>{
  const f=await fixture(t);f.rightClick('link');f.node('files-menu-delete').onclick();
  assert.match(f.node('files-delete-warning').textContent,/指向的内容保留/);
  f.node('files-delete-dialog').listeners.get('cancel')({preventDefault(){}});
  assert.equal(f.node('files-delete-dialog').open,false);
  assert.equal(f.calls.filter(c=>c[0]==='delete').length,0);
});

test('copy retains source clipboard for repeated pastes; shortcuts do not intercept typing',async t=>{
  const f=await fixture(t);f.rightClick('folder');f.node('files-menu-copy').onclick();
  assert.equal(f.calls.filter(c=>c[0]==='paste').length,0);
  await f.browse('/Target');
  f.node('files-table-wrap').listeners.get('contextmenu')({preventDefault(){},target:{closest:()=>null},clientX:10,clientY:10});
  assert.equal(f.node('files-menu').hidden,false);assert.equal(f.node('files-menu-paste').disabled,false);
  await f.node('files-menu-paste').onclick();
  assert.deepEqual(f.calls.filter(c=>c[0]==='paste'),[['paste','phone-one','media','media','/Documents/folder','/Target','copy']]);
  assert.equal(f.node('files-paste').disabled,false);
  let prevented=false;
  f.windowListeners.get('keydown')({key:'v',ctrlKey:true,target:{tagName:'INPUT'},preventDefault(){prevented=true;}});
  assert.equal(prevented,false);
});

test('cut uses keyboard and clears clipboard only after successful paste',async t=>{
  let fail=true;
  const f=await fixture(t,{paste:async()=>{if(fail)throw new Error('same name');}});
  f.rightClick('first.txt');
  f.windowListeners.get('keydown')({key:'x',ctrlKey:true,target:{tagName:'TR'},preventDefault(){}});
  await f.browse('/Target');await f.node('files-paste').onclick();
  assert.match(f.node('files-error').textContent,/same name/);
  assert.equal(f.node('files-paste').disabled,false);
  fail=false;await f.node('files-paste').onclick();
  assert.equal(f.node('files-paste').disabled,true);
  assert.equal(f.node('files-clipboard').textContent,'');
  assert.equal(f.calls.filter(c=>c[0]==='paste').length,2);
  assert.equal(f.calls.find(c=>c[0]==='paste').at(-1),'move');
});
