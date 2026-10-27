import {fileCall,fileSize,initializeFileManager} from './files.js';
import {installFileDrop} from './drop.js';
import {initializeShell} from './shell.js';
import {initializeIOSSettings} from './ios-settings.js';
import {runDeviceTool} from './tools.js';
import {keyPacket, textPacket, touchPacket, scrollPacket, spsCodec, parsePacket} from './protocol.js';

const $ = id => document.getElementById(id);
const canvas = $('screen'), context = canvas.getContext('2d', {alpha:false, desynchronized:true});
let devices = [], selected = '', socket = null, decoder = null, configData = null;
let starting = false, stopping = false, refreshing = false, streamGeneration = 0, touch = null, frameCount = 0, lastStat = performance.now();
let smokeSerial = '', smokeFrames = 0, smokeReported = false;
let fileSerial = '';
let mirrorSerial = '', openDevices = new Set(), opening = false, toolBusy = false;
const shellPanel=initializeShell({notice,onBusy:busy=>{toolBusy=busy;updateControls();}});
const iosSettingsPanel=initializeIOSSettings({notice,onBusy:busy=>{toolBusy=busy;updateControls();}});
let deviceListSignature='';
let fittedOrientation='', noticeTimer=null;
let controlsHideTimer=null;
const controls=document.querySelector('.controls');
let activeControls=null,activeSettings=null,activePlatform='';
let activeCodec='',activeVideoModes=null,switchingVideo=false;
const videoChoices=new Map(),videoModeNames={jpeg:'截图',h264:'USB H.264'};
let pointerIntervalMS=0,lastWheelAt=0;
const buttonControls={3:'home',4:'back',187:'recent',26:'power',25:'volumeDown',24:'volumeUp'};
const heldKeys = new Set();
const auth = new URLSearchParams(location.search).get('token') || sessionStorage.getItem('goscrcpy-token');
if (auth) { sessionStorage.setItem('goscrcpy-token',auth); history.replaceState(null, '', location.pathname); }
const fileDrop=installFileDrop({onHint:message=>{if(document.body.classList.contains('filemanager')){$('files-error').textContent=message;$('files-error').hidden=false;}else notice(message,true);}});
fileDrop.setTarget(uploadFromDrop,()=>{
  const device=devices.find(d=>d.serial===selected);
  return {allowed:!!device&&['ios','android'].includes(device.platform)&&!toolBusy&&!starting&&!stopping,
    title:toolBusy?'请等待当前文件传输完成':!device?'请先选择一台手机':'松开以上传到手机',
    detail:device?`${device.model} · ${device.platform==='ios'?'媒体目录':'/sdcard/Download'}`:'选中手机后，可直接拖入电脑文件。'};
});

async function uploadFromDrop(paths){
  if(toolBusy)return;const serial=selected;toolBusy=true;updateControls();
  let previous='',timer,finished=false,polling=false,cancelling=false;
  const show=progress=>fileDrop.progress({
    title:`${({running:'正在上传',done:'上传完成',cancelled:'已取消',error:'上传失败'})[progress.state]||'正在准备'} · ${progress.name||paths.length+' 个文件'}`,
    detail:progress.error||(cancelling?'正在取消并清理临时文件…':`${fileSize(progress.done||0)} / ${fileSize(progress.total||0)}`),
    percent:progress.state==='done'?100:progress.total?Math.min(100,100*progress.done/progress.total):0,
    running:progress.state==='running',onCancel:async()=>{cancelling=true;await fileCall('CancelFileTransfer');}
  });
  try{
    previous=(await fileCall('GetFileTransfer')).id;show({state:'preparing'});
    timer=setInterval(async()=>{if(polling)return;polling=true;try{const p=await fileCall('GetFileTransfer');if(!finished&&p.id&&p.id!==previous)show(p);}catch{}finally{polling=false;}},150);
    await fileCall('UploadDroppedFiles',serial,'','',paths);show(await fileCall('GetFileTransfer'));
  }catch(error){
    const progress=await fileCall('GetFileTransfer').catch(()=>null);
    show(progress?.id&&progress.id!==previous?progress:{state:'error',error:String(error.message||error)});
  }finally{finished=true;clearInterval(timer);toolBusy=false;updateControls();}
}

async function call(method, serial, options, clipboard='') {
  const binding = window.go?.main?.App;
  if (binding) return binding[method](...(method==='DeviceTool'?[serial,options,clipboard]:['StartMirror','OpenDevice'].includes(method)?[serial,options]:[]));
  const response = await fetch('/api', {method:'POST', headers:{'Content-Type':'application/json',Authorization:`Bearer ${auth}`},body:JSON.stringify({method,serial,options:method==='DeviceTool'?undefined:options,action:method==='DeviceTool'?options:undefined,clipboard:method==='DeviceTool'?clipboard:undefined})});
  const result = await response.json();
  if (!response.ok) throw new Error(result.error || '后端请求失败');
  return result;
}
function reportSmoke(error='') {
  if(!smokeSerial||smokeReported)return;
  smokeReported=true;
  void window.go.main.App.ReportSmoke({codec:activeCodec,frames:smokeFrames,width:canvas.width,height:canvas.height,error});
}
function notice(message, error=false) {
  $('notice').textContent=message; $('notice').classList.toggle('error',error);
  document.body.classList.add('show-notice');clearTimeout(noticeTimer);
  noticeTimer=setTimeout(()=>document.body.classList.remove('show-notice'),3500);
  if(error)reportSmoke(message);
}
function stateLabel(state) { return ({device:'已授权 · 就绪',unauthorized:'等待手机授权',offline:'设备离线',no:'驱动或权限异常',unavailable:'手机服务未就绪'})[state] || state; }
function isLive() { return socket?.readyState === WebSocket.OPEN; }
function updateVideoMode(device){
  const modes=activeVideoModes||device?.videoModes||(device?.platform==='ios'?['jpeg']:device?['h264']:[]);
  const select=$('video-mode');
  for(const option of select.options)option.disabled=!modes.includes(option.value);
  const choice=videoChoices.get(selected);
  select.value=choice||activeCodec||(modes.includes('h264')?'h264':'jpeg');
  // If a previously selected plugin is removed, keep its choice visible but
  // disabled; the user can still explicitly switch back to screenshots.
  select.disabled=!device||starting||stopping||switchingVideo||toolBusy||socket?.readyState===WebSocket.CONNECTING||modes.length<2&&(!choice||modes.includes(choice));
  $('video-mode-current').textContent=isLive()?`当前：${videoModeNames[activeCodec]||activeCodec}`:starting||switchingVideo?'连接中…':'未连接';
  const unavailable=device?.platform==='ios'&&!modes.includes('h264');
  $('video-mode-hint').textContent=!device?'连接设备后检测可用方式':unavailable?'未检测到 PhoneVideoTweak，暂不可使用 USB H.264':modes.length<2?'此设备使用 USB H.264':switchingVideo?'正在切换投屏方式…':'切换后自动重新连接投屏';
  select.title=unavailable?'安装 PhoneVideoTweak 并重启 SpringBoard 后可切换 USB H.264':'';
}
function updateControls() {
  const device = devices.find(d=>d.serial===selected);
  const capabilities=activeControls|| (device?.controls ? new Set(device.controls):null);
  const settings=activeSettings || (device?.settings ? new Set(device.settings):null);
  $('selected-name').textContent = device?.model || '未选择设备';
  $('selected-serial').textContent = device?.serial || '等待设备连接';
  $('transport').textContent = device?.transport || '—';
  $('device-state').textContent = device ? stateLabel(device.state) : '—';
  $('start').disabled = starting || stopping || !!socket || device?.state !== 'device';
  $('start').querySelector('span').textContent = starting ? '正在连接…' : '重新连接';
  $('stop').disabled = starting || stopping || !socket;
  document.querySelectorAll('[data-key]').forEach(b=>{
    const supported=!capabilities||capabilities.has(buttonControls[b.dataset.key]);
    b.hidden=!supported;
    b.disabled=!isLive()||!supported;
    b.dataset.originalTitle??=b.title;
    b.title=supported?b.dataset.originalTitle:'当前设备未提供此操作';
  });
  $('open-files').disabled=toolBusy||!selected||!device||!['android','ios'].includes(device.platform);
  $('open-shell').disabled=toolBusy||!device||!(device.platform==='ios'||device.platform==='android'&&device.state==='device');
  $('ios-one-click').hidden=device?.platform!=='ios';
  $('ios-one-click').disabled=toolBusy||device?.platform!=='ios'||device?.state!=='device';
  $('send-text').disabled=!isLive()||(capabilities&&!capabilities.has('text'));
  $('paste').disabled=$('send-text').disabled;
  $('paste').hidden=!!capabilities&&!capabilities.has('text');
  document.querySelectorAll('[data-tool]').forEach(b=>{b.hidden=device?.platform!=='android';b.disabled=toolBusy||!isLive()||device?.platform!=='android';b.title=b.dataset.originalTitle||b.title;});
  updateDeviceMenu();
  updateVideoMode(device);
  for(const [id,setting] of [['max-size','maxSize'],['max-fps','maxFPS'],['bit-rate','bitRate']]){
    $(id).disabled=!!settings&&!settings.has(setting);
    $(id).title=$(id).disabled?'当前连接不支持此设置':'';
  }
  document.querySelectorAll('.device').forEach(b=>{b.classList.toggle('selected',b.dataset.serial===selected);b.setAttribute('aria-selected',String(b.dataset.serial===selected));});
}
let menuSerial='',menuOrigin=null,devicePreferenceSave=Promise.resolve();
const deviceMenu=$('device-menu');
function selectDevice(serial){
  selected=serial;updateControls();
  devicePreferenceSave=devicePreferenceSave.then(()=>fileCall('RememberDevice',serial)).catch(error=>notice('无法记住所选手机：'+String(error.message||error),true));
}
function closeDeviceMenu(restoreFocus=false){
  deviceMenu.hidden=true;menuSerial='';
  if(restoreFocus&&menuOrigin?.isConnected)menuOrigin.focus({preventScroll:true});
  menuOrigin=null;
}
function updateDeviceMenu(){
  if(deviceMenu.hidden)return;
  const device=devices.find(d=>d.serial===menuSerial);
  if(!device){closeDeviceMenu();return;}
  $('device-menu-connect').disabled=opening||device.state!=='device'||openDevices.has(menuSerial);
  $('device-menu-connect').title=openDevices.has(menuSerial)?'该手机的投屏窗口已打开':'';
  $('device-menu-files').disabled=toolBusy||!['ios','android'].includes(device.platform);
  $('device-menu-shell').disabled=toolBusy||!(device.platform==='ios'||device.platform==='android'&&device.state==='device');
  $('device-menu-ios-settings').hidden=device.platform!=='ios';
  $('device-menu-ios-settings').disabled=toolBusy||device.platform!=='ios'||device.state!=='device';
}
function showDeviceMenu(serial,event){
  if(mirrorSerial||smokeSerial||starting||stopping)return;
  if(socket&&selected!==serial)return;
  selectDevice(serial);menuSerial=serial;menuOrigin=event.currentTarget;
  deviceMenu.hidden=false;updateDeviceMenu();
  const rect=menuOrigin.getBoundingClientRect();
  const x=event.clientX||rect.left+rect.width/2,y=event.clientY||rect.top+rect.height/2;
  deviceMenu.style.left=Math.max(8,Math.min(x,innerWidth-deviceMenu.offsetWidth-8))+'px';
  deviceMenu.style.top=Math.max(8,Math.min(y,innerHeight-deviceMenu.offsetHeight-8))+'px';
  Array.from(deviceMenu.querySelectorAll('button')).find(button=>!button.disabled)?.focus({preventScroll:true});
}
$('device-menu-connect').onclick=()=>{const serial=menuSerial;closeDeviceMenu();void openDevice(serial);};
$('device-menu-files').onclick=()=>{const serial=menuSerial;closeDeviceMenu();void openFiles(serial);};
$('device-menu-shell').onclick=()=>{const serial=menuSerial;closeDeviceMenu();void shellPanel.open(serial);};
$('device-menu-ios-settings').onclick=()=>{const device=devices.find(d=>d.serial===menuSerial);closeDeviceMenu();if(device&&!toolBusy)void iosSettingsPanel.open(device);};
deviceMenu.addEventListener('keydown',event=>{
  if(event.key==='Escape'){event.preventDefault();event.stopPropagation();closeDeviceMenu(true);return;}
  if(event.key==='Tab'){closeDeviceMenu();return;}
  if(!['ArrowUp','ArrowDown','Home','End'].includes(event.key))return;
  event.preventDefault();
  const buttons=Array.from(deviceMenu.querySelectorAll('button')).filter(button=>!button.disabled);
  if(!buttons.length)return;
  const index=buttons.indexOf(document.activeElement),next=event.key==='Home'?0:event.key==='End'?buttons.length-1:(index+(event.key==='ArrowUp'?-1:1)+buttons.length)%buttons.length;
  buttons[next].focus({preventScroll:true});
});
document.addEventListener('pointerdown',event=>{if(!deviceMenu.hidden&&!deviceMenu.contains(event.target))closeDeviceMenu();},true);
document.addEventListener('contextmenu',event=>{if(!event.target.closest('.device')&&!deviceMenu.contains(event.target))closeDeviceMenu();});
window.addEventListener('blur',()=>closeDeviceMenu());window.addEventListener('resize',()=>closeDeviceMenu());
$('devices').addEventListener('scroll',()=>closeDeviceMenu());
async function refresh(manual=false) {
  if (refreshing) return;
  refreshing=true; $('refresh').setAttribute('aria-busy','true');
  try {
    devices = await call('GetDevices');
    if(!mirrorSerial&&!smokeSerial)openDevices=new Set(await call('GetOpenDevices'));
    $('count').textContent=devices.length;
    if(mirrorSerial||smokeSerial)selected=mirrorSerial||smokeSerial;
    else if (!devices.some(d=>d.serial===selected)) selected=devices.find(d=>d.state==='device')?.serial || devices[0]?.serial || '';
    const signature=JSON.stringify([devices,[...openDevices].sort()]);
    if(signature!==deviceListSignature){
    deviceListSignature=signature;
    const container=$('devices'); container.replaceChildren();
    if (!devices.length) { const empty=document.createElement('div'); empty.className='empty-devices'; empty.textContent='尚未发现设备\n连接手机后将自动出现在这里'; container.append(empty); }
    for (const d of devices) {
      const button=document.createElement('button'); button.className='device'; button.dataset.serial=d.serial;button.setAttribute('role','option');button.setAttribute('aria-selected',String(d.serial===selected));
      const icon=document.createElement('span'); icon.className='device-icon'; icon.textContent='▯';
      const text=document.createElement('span'); text.className='device-text';
      const name=document.createElement('strong'); name.textContent=d.model;
      const serial=document.createElement('small'); serial.textContent=d.serial;
      const dot=document.createElement('span'); dot.className='dot '+(d.state==='device'?'':'error'); dot.title=stateLabel(d.state);
      const status=document.createElement('small');status.className='device-state-label';status.textContent=openDevices.has(d.serial)?'投屏窗口已打开':`${d.platform==='ios'?'iOS':'Android'} · ${stateLabel(d.state)}`;
      text.append(name,serial,status); button.append(icon,text,dot);
      button.onclick=()=>{if(starting||stopping)return; if(socket&&selected!==d.serial){notice('请先停止当前投屏，再切换设备');return;} selectDevice(d.serial);};
      button.oncontextmenu=event=>{event.preventDefault();event.stopPropagation();showDeviceMenu(d.serial,event);};
      button.onkeydown=event=>{if(event.key==='ContextMenu'||(event.shiftKey&&event.key==='F10')){event.preventDefault();showDeviceMenu(d.serial,event);}};
      button.ondblclick=()=>{if(!mirrorSerial&&!smokeSerial)void openDevice(d.serial);};
      container.append(button);
    }
    }
    $('adb-dot').className='dot'; $('adb-label').textContent='设备检测已就绪';
    if (!socket && !starting && (manual || !$('notice').classList.contains('error'))) {
      const d=devices.find(d=>d.serial===selected);
      if(d?.state==='unauthorized') notice('请解锁手机，在「允许 USB 调试」弹窗中点击允许。',true);
      else if(d && d.state!=='device') notice(d.error||'设备尚未就绪：'+stateLabel(d.state),true);
      else notice(devices.length ? `已发现 ${devices.length} 台设备，双击打开独立投屏窗口。` : '未发现设备。请检查 USB 数据线、手机授权和设备驱动。');
    }
  } catch(error) { $('adb-dot').className='dot error'; $('adb-label').textContent='设备检测不可用'; notice(String(error.message||error),true); }
  finally { refreshing=false; $('refresh').setAttribute('aria-busy','false'); updateControls(); }
}

function send(packet) { if(isLive()) socket.send(packet); }
function key(code) {
  if(activeControls&&buttonControls[code]&&!activeControls.has(buttonControls[code])){notice('当前设备未提供此操作');return;}
  send(keyPacket(code)); send(keyPacket(code,1));
}
function resetUI(message='● 尚未开始投屏') {
  document.body.classList.remove('streaming');
  touch=null; heldKeys.clear(); canvas.hidden=true; $('placeholder').hidden=false; $('live-tag').hidden=true;
  $('mirror-dot').className='dot dim'; $('mirror-title').textContent='屏幕预览'; $('dimensions').textContent='等待连接'; $('stream-status').textContent=message;
  $('placeholder-title').textContent='投屏尚未连接'; $('placeholder-detail').textContent='确认手机连接后，点击右侧「重新连接」';
}
function destroyStream() {
  ++streamGeneration;
  if(socket) { socket.onclose=null; socket.onerror=null; socket.onmessage=null; socket.close(); socket=null; }
  if(decoder && decoder.state!=='closed') decoder.close();
  decoder=null; configData=null; resetUI(); updateControls();
  activeControls=null;activeSettings=null;activePlatform='';activeCodec='';activeVideoModes=null;updateControls();
}
async function stop(message='投屏已停止。',error=false) {
  if(stopping)return;
  stopping=true; destroyStream(); updateControls();
  try { await call('StopMirror'); } catch(e) { message=String(e.message||e);error=true; }
  finally { stopping=false; updateControls(); notice(message,error); }
}
function configure(data, generation) {
  configData=data.slice();
  if(decoder && decoder.state!=='closed') decoder.close();
  decoder=new VideoDecoder({
    output(frame) {
      if(generation!==streamGeneration) {frame.close();return;}
      try {
        paint(frame,frame.displayWidth,frame.displayHeight);
      } finally {frame.close();}
    },
    error(error) {if(generation===streamGeneration) void stop('视频解码失败：'+error.message,true);}
  });
  decoder.configure({codec:spsCodec(data),optimizeForLatency:true,hardwareAcceleration:'prefer-hardware'});
}
async function fitMirrorWindow(width,height){
  const runtime=window.runtime;
  if(!runtime)return;
  try{
    if(await runtime.WindowIsMaximised()||await runtime.WindowIsFullscreen())return;
    const size=await runtime.WindowGetSize();
    const borderWidth=Math.max(0,size.w-innerWidth),borderHeight=Math.max(0,size.h-innerHeight);
    const availableWidth=Math.max(240,window.screen.availWidth-80-borderWidth);
    const availableHeight=Math.max(320,window.screen.availHeight-80-borderHeight);
    const scale=Math.min(840/Math.max(width,height),availableWidth/width,availableHeight/height);
    runtime.WindowSetSize(Math.round(width*scale+borderWidth),Math.round(height*scale+borderHeight));
    runtime.WindowCenter();
  }catch(error){console.warn('无法自动适配投屏窗口',error);}
}
function showControls(){
  clearTimeout(controlsHideTimer);controlsHideTimer=null;
  controls.inert=false;controls.setAttribute('aria-hidden','false');
  document.body.classList.add('controls-visible');
}
function hideControls(){
  clearTimeout(controlsHideTimer);controlsHideTimer=null;
  document.body.classList.remove('controls-visible');
  document.body.classList.add('settings-collapsed');
  $('settings-toggle').classList.remove('active');$('settings-toggle').setAttribute('aria-expanded','false');
  controls.inert=true;controls.setAttribute('aria-hidden','true');
}
function scheduleControlsHide(){
  if(controls.contains(document.activeElement)&&document.activeElement.matches('input,select,textarea'))return;
  if(controlsHideTimer)return;
  controlsHideTimer=setTimeout(()=>{controlsHideTimer=null;hideControls();},350);
}
window.addEventListener('pointermove',event=>{
  if(!document.body.classList.contains('mirroring')||event.pointerType==='touch'||event.buttons||touch)return;
  if(event.clientX>=innerWidth-6){showControls();return;}
  if(!document.body.classList.contains('controls-visible'))return;
  if(event.target.closest('.tool-rail,.settings-panel')){clearTimeout(controlsHideTimer);controlsHideTimer=null;return;}
  scheduleControlsHide();
});
$('mirror-window').addEventListener('pointerleave',scheduleControlsHide);
controls.addEventListener('focusin',()=>{clearTimeout(controlsHideTimer);controlsHideTimer=null;});
controls.addEventListener('focusout',scheduleControlsHide);
window.addEventListener('blur',hideControls);
window.addEventListener('keydown',event=>{
  if(event.key==='Escape'&&!$('tool-dialog').open&&document.body.classList.contains('controls-visible')){hideControls();event.preventDefault();event.stopImmediatePropagation();}
},true);
function paint(frame,width,height){
  if(canvas.width!==width||canvas.height!==height){releaseInput();canvas.width=width;canvas.height=height;$('dimensions').textContent=`${width} × ${height}`;}
  context.drawImage(frame,0,0,canvas.width,canvas.height);
  document.body.classList.add('streaming');
  const orientation=width>height?'landscape':'portrait';
  if(fittedOrientation!==orientation){fittedOrientation=orientation;void fitMirrorWindow(width,height);}
  frameCount++;canvas.hidden=false;$('placeholder').hidden=true;$('live-tag').hidden=false;
  $('stream-status').textContent='● 正在投屏 · 键鼠控制已开启';
  const now=performance.now();if(now-lastStat>=1000){$('fps').textContent=`${Math.round(frameCount*1000/(now-lastStat))} FPS`;frameCount=0;lastStat=now;}
  if(smokeSerial){
    smokeFrames++;
    if(smokeFrames===1){key(3);setTimeout(()=>{if(!smokeReported&&isLive())reportSmoke();},500);}
    if(smokeFrames>=5)reportSmoke();
  }
}
function enqueueImage(queue,data,generation){
  queue.pending=data;if(queue.busy)return;queue.busy=true;
  void(async()=>{
    try{
      while(queue.pending&&generation===streamGeneration){
        const data=queue.pending;queue.pending=null;
        const frame=await createImageBitmap(new Blob([data]));
        try{if(generation===streamGeneration)paint(frame,frame.width,frame.height);}finally{frame.close();}
      }
    }catch(error){if(generation===streamGeneration)void stop('无法显示画面：'+error.message,true);}
    finally{queue.busy=false;}
  })();
}
async function start() {
  if(starting||socket)return false;
  const device=devices.find(d=>d.serial===selected);
  const expectedCodec=videoChoices.get(selected)||device?.codec||'h264';
  if(expectedCodec==='h264'&&!('VideoDecoder' in window)) {notice('当前 WebView2 不支持视频解码，请安装或更新 Microsoft Edge WebView2 Runtime。',true);return false;}
  starting=true;updateControls();notice('正在推送服务并连接手机…');
  $('placeholder-title').textContent='正在建立投屏连接';$('placeholder-detail').textContent='首次连接可能需要几秒钟';
  try {
    const result=await call('StartMirror',selected,mirrorOptions());
    if(result.codec==='h264'&&!('VideoDecoder' in window)){
      await call('StopMirror');throw new Error('当前 WebView2 不支持视频解码，请安装或更新 Microsoft Edge WebView2 Runtime。');
    }
    const generation=++streamGeneration;
    activeControls=result.controls?new Set(result.controls):null;activeSettings=result.settings?new Set(result.settings):null;activePlatform=device?.platform||'';
    activeCodec=result.codec;activeVideoModes=result.videoModes||device?.videoModes||(activePlatform==='ios'?['jpeg']:['h264']);
    if(!activeVideoModes.includes(activeCodec))activeVideoModes=[...activeVideoModes,activeCodec];
    pointerIntervalMS=result.pointerIntervalMS||0;lastWheelAt=0;
    const imageQueue={pending:null,busy:false};
    canvas.width=result.metadata.width;canvas.height=result.metadata.height;
    $('dimensions').textContent=`${canvas.width} × ${canvas.height}`;
    socket=new WebSocket(result.url);socket.binaryType='arraybuffer';
    frameCount=0;lastStat=performance.now();
    socket.onopen=()=>{updateControls();$('mirror-dot').className='dot';$('mirror-title').textContent=result.metadata.name||'实时投屏';document.title=`${result.metadata.name||selected} · GoScrcpy`;window.runtime?.WindowSetTitle(document.title);notice('投屏已连接。点击画面后，可使用鼠标和键盘操作手机。');};
    socket.onmessage=event=>{
      if(generation!==streamGeneration)return;
      try {
        const packet=parsePacket(event.data);
        if(result.codec==='jpeg'){enqueueImage(imageQueue,packet.data,generation);return;}
        if(packet.config) {configure(packet.data,generation);return;}
        if(!decoder||decoder.state!=='configured')throw new Error('未收到 H.264 解码配置');
        let data=packet.data;
        if(packet.key&&configData){data=new Uint8Array(configData.length+packet.data.length);data.set(configData);data.set(packet.data,configData.length);}
        decoder.decode(new EncodedVideoChunk({type:packet.key?'key':'delta',timestamp:packet.timestamp,data}));
      } catch(error) {void stop('无法显示画面：'+error.message,true);}
    };
    socket.onerror=()=>{if(generation===streamGeneration)notice('视频连接异常，请检查设备连接。',true);};
    socket.onclose=async()=>{
      if(generation!==streamGeneration)return;
      destroyStream();notice('投屏已断开，请检查手机连接后重新开始。',true);
      try {const state=await call('GetState');$('logs').textContent=state.logs||state.error||'连接已关闭';}catch{}
    };
    return true;
  } catch(error) {resetUI();notice(String(error.message||error),true);return false;}
  finally {starting=false;updateControls();}
}

function point(event) {
  const r=canvas.getBoundingClientRect();
  // object-fit letterboxing is inside the element on some WebView versions.
  const scale=Math.min(r.width/canvas.width,r.height/canvas.height), w=canvas.width*scale,h=canvas.height*scale;
  const left=r.left+(r.width-w)/2, top=r.top+(r.height-h)/2;
  return {x:Math.max(0,Math.min(canvas.width-1,Math.floor((event.clientX-left)/scale))),y:Math.max(0,Math.min(canvas.height-1,Math.floor((event.clientY-top)/scale))),width:canvas.width,height:canvas.height};
}
function releaseInput() {
  if(touch){send(touchPacket(1,touch.point));touch=null;}
  for(const code of heldKeys)send(keyPacket(code,1));heldKeys.clear();
}
canvas.addEventListener('pointerdown',event=>{
  if(event.button!==0||!isLive()||touch)return;
  event.preventDefault();const initialPoint=point(event);canvas.focus({preventScroll:true});canvas.setPointerCapture(event.pointerId);
  touch={id:event.pointerId,point:initialPoint,lastMoveAt:0};send(touchPacket(0,touch.point));
});
canvas.addEventListener('pointermove',event=>{
  if(touch?.id!==event.pointerId)return;
  touch.point=point(event);
  const now=performance.now();if(now-touch.lastMoveAt<pointerIntervalMS)return;
  touch.lastMoveAt=now;send(touchPacket(2,touch.point));
});
for(const type of ['pointerup','pointercancel','lostpointercapture'])canvas.addEventListener(type,event=>{if(touch?.id!==event.pointerId)return;touch.point=point(event);send(touchPacket(1,touch.point));touch=null;});
canvas.addEventListener('contextmenu',event=>{
  event.preventDefault();
  if(!isLive())return;
  releaseInput();
  key(activePlatform==='ios'?3:4);
});
canvas.addEventListener('wheel',event=>{event.preventDefault();const now=performance.now();if(pointerIntervalMS&&now-lastWheelAt<pointerIntervalMS*2)return;lastWheelAt=now;send(scrollPacket(point(event),-Math.sign(event.deltaX),-Math.sign(event.deltaY)));},{passive:false});
const keyCodes={Escape:4,Home:3,Enter:66,NumpadEnter:66,Backspace:67,Delete:112,Tab:61,ArrowUp:19,ArrowDown:20,ArrowLeft:21,ArrowRight:22,PageUp:92,PageDown:93,End:123};
canvas.addEventListener('keydown',event=>{
  if(!isLive())return;
  const code=keyCodes[event.code];
  if(code&&activeControls&&[...activeControls].some(c=>c.startsWith('key')&&c!=='keyboard')&&!activeControls.has('key'+code)){event.preventDefault();notice('当前设备未提供此按键操作');return;}
  if(code){event.preventDefault();send(keyPacket(code,0,event.repeat?1:0));heldKeys.add(code);return;}
  if(event.ctrlKey||event.metaKey||event.altKey||event.isComposing)return;
  if(event.key.length===1){event.preventDefault();try{send(textPacket(event.key));}catch(e){notice(e.message,true);}}
});
canvas.addEventListener('keyup',event=>{const code=keyCodes[event.code];if(code&&heldKeys.has(code)){event.preventDefault();send(keyPacket(code,1));heldKeys.delete(code);}});
canvas.addEventListener('blur',releaseInput);window.addEventListener('blur',releaseInput);
$('text-form').addEventListener('submit',event=>{event.preventDefault();const value=$('text-input').value;if(!value||!isLive())return;try{send(textPacket(value,true));$('text-input').value='';notice('文字已发送，请在手机输入框中确认。');}catch(e){notice(e.message,true);}});
document.querySelectorAll('[data-key]').forEach(button=>button.onclick=()=>key(Number(button.dataset.key)));
function mirrorOptions(){return {maxSize:Number($('max-size').value),maxFPS:Number($('max-fps').value),bitRate:Number($('bit-rate').value),videoMode:videoChoices.get(selected)||'auto'};}
$('video-mode').onchange=async()=>{
  if(starting||stopping||switchingVideo||toolBusy){updateControls();return;}
  const mode=$('video-mode').value,device=devices.find(d=>d.serial===selected);
  const modes=activeVideoModes||device?.videoModes||(device?.platform==='ios'?['jpeg']:['h264']);
  if(!modes.includes(mode)){updateControls();notice('当前设备未提供此投屏方式',true);return;}
  const previous=activeCodec||videoChoices.get(selected)||device?.codec;
  videoChoices.set(selected,mode);
  if(!socket){updateControls();return;}
  switchingVideo=true;releaseInput();updateControls();
  try{
    await stop('正在切换投屏方式…');
    if(!await start()){
      const error=$('notice').textContent;
      if(previous&&previous!==mode){
        videoChoices.set(selected,previous);
        const restored=await start();
        notice(`${error}${restored?'；已恢复'+videoModeNames[previous]:''}`,true);
      }
    }
  }finally{switchingVideo=false;updateControls();}
};
async function openDevice(serial=selected){
  if(opening)return;
  selected=serial;opening=true;updateControls();
  try{await call('OpenDevice',serial,mirrorOptions());openDevices.add(serial);notice('已打开独立投屏窗口，可继续连接其他手机。');}
  catch(error){notice(String(error.message||error),true);}
  finally{opening=false;updateControls();}
}
async function openFiles(serial=selected){if(toolBusy)return;toolBusy=true;updateControls();try{await fileCall('OpenFileManager',serial);}catch(error){notice(String(error.message||error),true);}finally{toolBusy=false;updateControls();}}
$('open-files').onclick=()=>openFiles();
$('open-shell').onclick=()=>{if(selected&&!toolBusy)void shellPanel.open(selected);};
$('ios-one-click').onclick=()=>{const device=devices.find(d=>d.serial===selected);if(device&&!toolBusy)void iosSettingsPanel.open(device);};
$('settings-toggle').onclick=()=>{
  showControls();
  const collapsed=document.body.classList.toggle('settings-collapsed');
  $('settings-toggle').classList.toggle('active',!collapsed);$('settings-toggle').setAttribute('aria-expanded',String(!collapsed));
};
for(const button of document.querySelectorAll('[data-tool]')){
  button.dataset.originalTitle=button.title;
  button.onclick=async()=>{
    if(toolBusy)return;toolBusy=true;updateControls();notice('正在执行设备操作…');
    try{
      const action=button.dataset.tool,result=await runDeviceTool(action,selected,call);
      if(action==='store')notice('已在手机上打开剪贴板包名对应的商店页面。');
      else if(result){$('tool-title').textContent=button.querySelector('small').textContent;$('tool-output').textContent=result;$('tool-dialog').showModal();notice('设备操作已完成。');}
      else notice('已取消文件选择。');
    }catch(error){notice(String(error.message||error),true);}
    finally{toolBusy=false;updateControls();}
  };
}
$('tool-close').onclick=()=>$('tool-dialog').close();
$('tool-copy').onclick=async()=>{try{const value=$('tool-output').textContent;if(window.runtime)await window.runtime.ClipboardSetText(value);else await navigator.clipboard.writeText(value);$('tool-copy').textContent='已复制';setTimeout(()=>$('tool-copy').textContent='复制内容',1500);}catch{notice('无法写入剪贴板，请手动选中内容复制。',true);}};
$('paste').onclick=async()=>{try{const value=window.runtime?await window.runtime.ClipboardGetText():await navigator.clipboard.readText();if(!value){notice('电脑剪贴板中没有文字。');return;}if(!isLive())return;send(textPacket(value,true));notice('剪贴板文字已发送到手机。');}catch(error){if(document.body.classList.contains('settings-collapsed'))$('settings-toggle').click();$('text-input').focus();notice('无法读取剪贴板，请在右侧文字框中粘贴并发送。',true);}};
$('start').onclick=start;$('stop').onclick=()=>stop();$('refresh').onclick=()=>refresh(true);
$('mirror-logs-toggle').onclick=()=>$('logs-toggle').click();
$('logs-toggle').onclick=async()=>{ $('logs').hidden=!$('logs').hidden;if(!$('logs').hidden){try{const state=await call('GetState');$('logs').textContent=`ADB: ${state.adbPath}\nscrcpy-server: ${state.version}\n${state.error}\n${state.logs}`;}catch(e){$('logs').textContent=String(e);}} };
window.addEventListener('beforeunload',()=>{releaseInput();socket?.close();});
resetUI();updateControls();
async function initialize() {
  try {
    const launch=await call('GetLaunchOptions');
    selected=launch.lastDevice||'';
    fileSerial=launch.fileSerial||'';
    if(fileSerial){document.body.className='filemanager';$('connection-window').hidden=true;$('mirror-window').hidden=true;$('file-window').hidden=false;await initializeFileManager(fileSerial,fileDrop);return;}
    smokeSerial=launch.smokeSerial||'';
    mirrorSerial=launch.mirrorSerial||'';
    const mirrorMode=!!(mirrorSerial||smokeSerial);
    document.body.className=mirrorMode?'mirroring settings-collapsed':'connecting';
    $('connection-window').hidden=mirrorMode;$('mirror-window').hidden=!mirrorMode;
    if(launch.settings?.maxSize){$('max-size').value=String(launch.settings.maxSize);$('max-fps').value=String(launch.settings.maxFPS);$('bit-rate').value=String(launch.settings.bitRate);}
    if(['jpeg','h264'].includes(launch.settings?.videoMode)&&mirrorMode)videoChoices.set(mirrorSerial||smokeSerial,launch.settings.videoMode);
    await refresh();
    if(mirrorMode) {
      selected=mirrorSerial||smokeSerial;updateControls();
      if(!devices.some(d=>d.serial===selected&&d.state==='device'))throw new Error('设备未连接或未授权，请检查手机连接后重新连接');
      await start();
    }
  } catch(error) {notice(String(error.message||error),true);}
}
void initialize();setInterval(()=>{if(!fileSerial&&!starting&&!stopping&&!opening&&$('auto-refresh').checked)void refresh();},3000);
