// The native runtime resolves real OS paths. Browser File.name/File.path are
// insufficient on Windows and must never be used as upload source paths.
export function installFileDrop({target=window,doc=document,onHint=()=>{}}={}){
  const overlay=doc.createElement('div');overlay.id='file-drop-overlay';overlay.hidden=true;
  overlay.innerHTML='<div><span>↑</span><strong></strong><p></p></div>';doc.body.append(overlay);
  const progressCard=doc.createElement('section');progressCard.id='drop-transfer';progressCard.className='files-transfer';progressCard.hidden=true;
  progressCard.innerHTML='<div class="files-transfer-top"><span class="files-transfer-icon">↑</span><div><strong></strong><p></p></div><button class="secondary" hidden>取消</button></div><progress max="100" value="0" aria-label="拖入文件传输进度"></progress>';doc.body.append(progressCard);
  let handler=null,describe=()=>({allowed:false,title:'请先打开手机文件管理',detail:'在当前手机目录中拖入文件即可上传。'}),depth=0,registered=false;
  const reset=()=>{depth=0;overlay.hidden=true;};
  const isFile=event=>Array.from(event.dataTransfer?.types||[]).includes('Files');
  function display(){const status=describe();overlay.querySelector('strong').textContent=status.title;overlay.querySelector('p').textContent=status.detail||'';overlay.dataset.allowed=String(!!status.allowed);overlay.hidden=false;return status;}
  const enter=event=>{event.preventDefault();if(isFile(event)){depth++;display();}};
  const over=event=>{event.preventDefault();if(isFile(event)){const status=display();if(event.dataTransfer)event.dataTransfer.dropEffect=status.allowed?'copy':'none';}};
  const leave=event=>{if(isFile(event)&&--depth<=0)reset();};
  const drop=event=>{
    // Cancel navigation/download even when there is no device, a transfer is
    // busy, or the user drops a URL rather than a local file. Do not stop
    // propagation: Wails' window listener still needs the native File objects.
    event.preventDefault();reset();
    if(!isFile(event)){event.stopImmediatePropagation();onHint('请拖入电脑上的文件；网页链接不会上传。');return;}
    if(!registered)onHint('拖拽上传请使用 GoScrcpy 桌面窗口。');
  };
  function connect(){
    if(registered||!target.runtime?.OnFileDrop)return;
    target.runtime.OnFileDrop((_x,_y,paths)=>{
      reset();const status=describe();
      if(!status.allowed||!handler){onHint(status.title);return;}
      if(!Array.isArray(paths)||!paths.length||paths.some(path=>!path)){onHint('无法取得文件路径，请改用“上传到手机”。');return;}
      void Promise.resolve().then(()=>handler(paths)).catch(error=>onHint(String(error?.message||error)));
    },false);
    registered=true;
  }
  for(const [name,listener] of [['dragenter',enter],['dragover',over],['dragleave',leave],['drop',drop]])target.addEventListener(name,listener,true);
  target.addEventListener('blur',reset);connect();
  return {
    setTarget(callback,status){handler=callback;describe=status;connect();},
    progress({title,detail,percent=0,running=false,onCancel}){
      progressCard.hidden=false;progressCard.querySelector('strong').textContent=title;progressCard.querySelector('p').textContent=detail;
      progressCard.querySelector('progress').value=percent;
      const button=progressCard.querySelector('button');button.hidden=!running;button.disabled=false;button.textContent=running?'取消':'关闭';
      button.onclick=async()=>{button.disabled=true;button.textContent='正在取消…';try{await onCancel?.();}catch(error){onHint(String(error?.message||error));button.disabled=false;}};
      if(!running){button.hidden=false;button.textContent='关闭';button.onclick=()=>{progressCard.hidden=true;};}
    },
    dispose(){for(const [name,listener] of [['dragenter',enter],['dragover',over],['dragleave',leave],['drop',drop]])target.removeEventListener(name,listener,true);target.removeEventListener('blur',reset);if(registered)target.runtime.OnFileDropOff?.();overlay.remove();progressCard.remove();}
  };
}
