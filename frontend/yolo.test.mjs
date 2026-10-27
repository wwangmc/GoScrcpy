import test from 'node:test';
import assert from 'node:assert/strict';
import {initializeYOLO,projectDetections} from './yolo.js';

const detection={type:'detections',screen:{width:100,height:200,coordinateSystem:'top-left-pixels'},latencyMs:24.6,
  detections:[{label:'person',confidence:.925,box:{x:10,y:20,width:30,height:60}}]};

test('maps raw screen pixels to scaled video, clips boxes and rejects mismatched orientation',()=>{
  assert.deepEqual(projectDetections(detection,50,100),[{x:5,y:10,width:15,height:30,label:'person',confidence:.925}]);
  assert.equal(projectDetections(detection,200,100),null);
  assert.equal(projectDetections({...detection,screen:{...detection.screen,coordinateSystem:'unknown'}},50,100),null);
  assert.deepEqual(projectDetections({...detection,detections:[{confidence:1,box:{x:-10,y:190,width:30,height:40}},
    {confidence:.8,box:{x:NaN,y:0,width:30,height:40}},{confidence:.8,box:{x:200,y:0,width:30,height:40}}]},50,100),
    [{x:0,y:95,width:10,height:5,label:'目标',confidence:1}]);
  assert.deepEqual(projectDetections({...detection,detections:[]},50,100),[]);
});

function harness(getURL=async serial=>'ws://local/'+serial){
  const rectangles=[],labels=[],timers=new Map(),sockets=[];let id=0;
  const context={clearRect(){rectangles.length=0;labels.length=0;},strokeRect(...args){rectangles.push(args);},
    fillRect(){},measureText:text=>({width:text.length*6}),fillText:text=>labels.push(text)};
  const node=()=>({hidden:true,classList:{toggle(){},add(){},remove(){}},setAttribute(){}});
  const canvas={width:100,height:200,hidden:false,getBoundingClientRect:()=>({width:100,height:200})};
  const overlay={...node(),width:100,height:200,getContext:()=>context},button=node(),status=node(),notices=[];
  const controller=initializeYOLO({canvas,overlay,button,status,getURL,notice:(...args)=>notices.push(args),
    createSocket:url=>{const ws={url,closed:false,close(){this.closed=true;}};sockets.push(ws);return ws;},
    setTimer:(fn,delay)=>{timers.set(++id,{fn,delay});return id;},clearTimer:id=>timers.delete(id)});
  const update=(serial='one',platform='ios',live=true)=>controller.update({serial,platform},live);
  const click=async()=>{button.onclick();await Promise.resolve();};
  const receive=data=>sockets.at(-1).onmessage({data:JSON.stringify(data)});
  const expire=delay=>{for(const [id,t] of timers)if(t.delay===delay){timers.delete(id);t.fn();break;}};
  return {controller,canvas,overlay,button,status,rectangles,labels,sockets,timers,notices,update,click,receive,expire};
}

test('live results paint labels, empty frames and stale timeout erase previous boxes',async()=>{
  const h=harness();h.update();await h.click();h.receive(detection);
  assert.deepEqual(h.rectangles,[[10,20,30,60]]);assert.equal(h.labels[0],'person 93%');
  assert.match(h.status.textContent,/1 个目标 · 25 ms/);assert.equal(h.overlay.hidden,false);
  h.receive({...detection,detections:[]});assert.equal(h.rectangles.length,0);assert.match(h.status.textContent,/0 个目标/);
  h.receive(detection);h.expire(2000);assert.equal(h.overlay.hidden,true);assert.equal(h.rectangles.length,0);
  h.receive(detection);h.canvas.width=50;h.canvas.height=100;h.controller.draw();assert.deepEqual(h.rectangles,[[5,10,15,30]]);
  await h.click();assert.equal(h.sockets[0].closed,true);assert.equal(h.status.hidden,true);assert.equal(h.timers.size,0);
});

test('late setup, previous connection callbacks and other devices cannot paint',async()=>{
  let resolve;const h=harness(()=>new Promise(r=>resolve=r));h.update();await h.click();await h.click();resolve('ws://local');await Promise.resolve();
  assert.equal(h.sockets.length,0);
  const g=harness();g.update();await g.click();const old=g.sockets[0].onmessage;
  g.receive(detection);g.update('two');assert.equal(g.sockets[0].closed,true);old({data:JSON.stringify(detection)});
  assert.equal(g.overlay.hidden,true);assert.equal(g.rectangles.length,0);
  g.update('two','android');assert.equal(g.button.hidden,true);assert.equal(g.button.disabled,true);
});

test('socket disconnect reconnects only while enabled; fatal and device errors clear boxes',async()=>{
  const h=harness();h.update();await h.click();h.receive(detection);h.sockets[0].onclose();
  assert.equal(h.overlay.hidden,true);h.expire(3000);await Promise.resolve();assert.equal(h.sockets.length,2);
  h.receive(detection);h.receive({type:'error',message:'模型加载失败',retryInSeconds:5});assert.equal(h.overlay.hidden,true);
  h.receive(detection);assert.equal(h.overlay.hidden,false);
  h.receive({type:'error',message:'请安装 PhoneYOLO',fatal:true});assert.equal(h.sockets[1].closed,true);
  assert.equal(h.overlay.hidden,true);assert.equal(h.notices.length,1);assert.equal(h.timers.size,0);
  await h.click();h.update('one','ios',false);assert.equal(h.button.disabled,true);assert.equal(h.status.hidden,true);
});
