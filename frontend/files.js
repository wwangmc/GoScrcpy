const $=id=>document.getElementById(id);
export async function fileCall(method,...args){
  const binding=window.go?.main?.App;
  if(binding)return binding[method](...args);
  const token=sessionStorage.getItem('goscrcpy-token');
  const response=await fetch('/api',{method:'POST',headers:{'Content-Type':'application/json',Authorization:`Bearer ${token}`},body:JSON.stringify({method,args})});
  const result=await response.json();if(!response.ok)throw new Error(result.error||'文件操作失败');return result;
}
export function fileSize(n){
  if(n<1024)return `${n} B`;
  const units=['KB','MB','GB','TB'];let i=0;n/=1024;
  while(n>=1024&&i<units.length-1){n/=1024;i++;}return `${n.toFixed(n>=100?0:1)} ${units[i]}`;
}
const message=error=>String(error?.message||error).replace(/^Error:\s*/,'');
const label=entry=>({directory:'文件夹',link:'符号链接',file:'文件',other:'其他'})[entry.kind]||'其他';
const symbol=entry=>entry.kind==='directory'?'▰':entry.kind==='link'?'↗':/\.(png|jpe?g|heic|webp|gif)$/i.test(entry.name)?'▧':'▤';
const parentPath=p=>p.slice(0,p.lastIndexOf('/'))||'/';

export async function initializeFileManager(serial,fileDrop){
    const state={serial,platform:'',scope:'',path:'',entries:[],selected:'',busy:false,loading:false,sort:'name',descending:false,apps:[],dialog:null,progress:null,cancelling:false};
  const working=()=>state.busy||state.loading;
  const chosen=()=>state.entries.find(e=>e.path===state.selected);
  const deleteDialog=$('files-delete-dialog'),menu=$('files-menu');
  let deleteTarget=null,deleteAttempted=false,clipboard=null;
  const modalOpen=()=>$('files-name-dialog').open||deleteDialog.open;
  function closeMenu(){menu.hidden=true;}
  function openMenu(event,item=null){
    event.preventDefault();closeMenu();if(working()||modalOpen())return;
    state.selected=item?.path||'';update();
    $('files-menu-download').disabled=item?.kind!=='file'||!!item?.error;
    for(const id of ['files-menu-copy','files-menu-cut','files-menu-rename','files-menu-delete'])$(id).disabled=!item;
    $('files-menu-paste').disabled=!clipboard||clipboard.scope!==state.scope||!state.path;
    menu.hidden=false;
    menu.style.left=`${Math.max(0,Math.min(event.clientX,window.innerWidth-menu.offsetWidth-8))}px`;
    menu.style.top=`${Math.max(0,Math.min(event.clientY,window.innerHeight-menu.offsetHeight-8))}px`;
    (item?$('files-menu-copy'):$('files-menu-paste')).focus();
  }
  const error=(value='')=>{$('files-error').textContent=value;$('files-error').hidden=!value;};
  function update(){
    const item=chosen(),disabled=working()||deleteDialog.open,ready=!!state.path;
    $('files-paste').disabled=disabled||!ready||!clipboard||clipboard.scope!==state.scope;
    $('files-clipboard').textContent=clipboard?`${clipboard.mode==='move'?'已剪切':'已复制'}：${clipboard.name}${clipboard.scope!==state.scope?'（请返回原文件位置粘贴）':''}`:'';
    for(const id of ['files-delete-submit','files-delete-close','files-delete-cancel'])$(id).disabled=working();
    $('files-delete-submit').textContent=state.busy&&deleteDialog.open?'正在删除…':'确认删除';
    for(const id of ['files-upload','files-mkdir','files-refresh','files-path','files-search','files-sort-name','files-sort-size'])$(id).disabled=disabled||!ready;
    $('files-path-form').querySelector('[type=submit]').disabled=disabled||!ready;
    $('files-download').disabled=disabled||item?.kind!=='file'||!!item?.error;
    $('files-rename').disabled=disabled||!item;
    $('files-up').disabled=disabled||state.path==='/'||!ready;
    $('files-load-apps').disabled=disabled;
    $('files-name-submit').disabled=disabled;
    $('files-name-input').disabled=disabled;
    $('files-name-close').disabled=disabled;$('files-name-cancel').disabled=disabled;
    document.querySelectorAll('.files-locations button').forEach(button=>button.disabled=disabled);
    $('files-loading').textContent=state.loading?'正在读取…':state.busy?'正在执行文件操作…':'';
    $('files-empty').hidden=state.entries.length!==0;
    $('files-empty').querySelector('strong').textContent=state.loading?'正在读取目录':ready?'此文件夹为空':'文件服务尚未连接';
    $('files-empty').querySelector('p').textContent=ready?'可上传电脑文件，或新建文件夹。':'请检查手机连接，然后选择左侧文件位置。';
    $('files-entry-count').textContent=ready?`${state.entries.length} 项`:'';
    $('files-summary').textContent=item?`${item.name} · ${label(item)}${item.kind==='file'?' · '+fileSize(item.size):''}`:ready?'双击文件夹进入 · 选择文件后下载到电脑':'等待连接';
    for(const row of $('files-entries').children){row.classList.toggle('selected',row.dataset.path===state.selected);row.classList.toggle('files-cut',clipboard?.mode==='move'&&clipboard.scope===state.scope&&row.dataset.path===clipboard.path);row.setAttribute('aria-selected',String(row.dataset.path===state.selected));row.tabIndex=disabled?-1:0;}
  }
  function render(){
    const query=$('files-search').value.toLocaleLowerCase();
    const entries=state.entries.filter(e=>e.name.toLocaleLowerCase().includes(query)).sort((a,b)=>{
      if((a.kind==='directory')!==(b.kind==='directory'))return a.kind==='directory'?-1:1;
      const order=state.sort==='size'?a.size-b.size:a.name.localeCompare(b.name,undefined,{numeric:true});return state.descending?-order:order;
    });
    const body=$('files-entries');body.replaceChildren();
    for(const item of entries){
      const row=document.createElement('tr');row.dataset.path=item.path;row.tabIndex=0;row.setAttribute('aria-selected',String(item.path===state.selected));
      const name=document.createElement('td'),icon=document.createElement('span'),text=document.createElement('span');
      icon.className='files-entry-icon '+item.kind;icon.textContent=symbol(item);text.textContent=item.name;name.append(icon,text);name.title=item.error||item.linkTarget||item.name;
      const kind=document.createElement('td');kind.textContent=item.error||label(item);
      const size=document.createElement('td');size.textContent=item.kind==='file'?fileSize(item.size):'—';row.append(name,kind,size);
      row.onclick=()=>{if(working())return;state.selected=item.path;update();};
      row.ondblclick=()=>{if(!working()&&['directory','link'].includes(item.kind))void browse(item.path);};
      row.oncontextmenu=event=>openMenu(event,item);
      row.addEventListener('keydown',event=>{if(event.key===' '&&!working()){event.preventDefault();state.selected=item.path;update();}});
      body.append(row);
    }
    $('files-sort-arrow').textContent=state.sort==='name'?(state.descending?'↓':'↑'):'';
    update();
    if(!entries.length&&state.entries.length){$('files-empty').hidden=false;$('files-empty').querySelector('strong').textContent='没有匹配的文件';$('files-empty').querySelector('p').textContent='试试其他关键词。';}
  }
  function locationName(){return state.platform==='android'?'共享存储':state.scope==='root'?'根目录':state.scope==='media'?'媒体文件':state.apps.find(a=>'app:'+a.id===state.scope)?.name||'应用文档';}
  function locationButton(container,name,p,scope){
    const button=document.createElement('button');button.type='button';
    const title=document.createElement('span');title.textContent=name;const detail=document.createElement('small');detail.textContent=p||'根目录';button.append(title,detail);
    button.classList.toggle('current',state.scope===scope&&state.path===p);button.onclick=()=>void browse(p,scope);container.append(button);
  }
  function locations(){
    const ios=state.platform==='ios';
    $('files-scopes').replaceChildren();$('files-shortcuts').replaceChildren();$('files-apps').replaceChildren();
    const scopes=ios?[['根目录 · AFC2','/','root'],['媒体文件 · AFC','/','media']]:[['共享存储','/sdcard','storage']];
    const shortcuts=ios?[['用户目录','/var/mobile','root'],['越狱目录','/var/jb','root'],['应用程序','/Applications','root'],['数据目录','/private/var','root']]:[['下载','/sdcard/Download','storage'],['相机照片','/sdcard/DCIM','storage'],['图片','/sdcard/Pictures','storage'],['文档','/sdcard/Documents','storage'],['文件系统','/','storage']];
    scopes.forEach(args=>locationButton($('files-scopes'),...args));shortcuts.forEach(args=>locationButton($('files-shortcuts'),...args));
    state.apps.forEach(app=>locationButton($('files-apps'),app.name,'/Documents','app:'+app.id));
    $('files-apps-section').hidden=!ios;$('files-apps-hint').hidden=state.apps.length>0;
    $('files-location-name').textContent=locationName();
    $('files-scope-description').textContent=ios?(state.scope==='root'?'浏览越狱设备的文件系统':state.scope==='media'?'照片、媒体与共享文件':'应用允许共享的文档'): '浏览手机存储，选择文件与电脑互传';
    update();
  }
  async function browse(p=state.path,scope=state.scope){
    if(working()||deleteDialog.open)return;closeMenu();
    state.loading=true;error();update();
    try{
      const result=await fileCall('BrowseFiles',serial,scope,p);
      state.platform=result.platform;state.scope=result.scope;state.path=result.path;state.entries=result.entries||[];state.selected='';
      $('files-path').value=state.path;$('files-search').value='';
      $('files-hint').textContent=result.hint||'';$('files-hint').hidden=!result.hint;
      locations();render();return true;
    }catch(e){error(message(e));$('files-path').value=state.path;return false;}
    finally{state.loading=false;update();}
  }
  function showProgress(progress){
    state.progress=progress;
    const running=progress.state==='running';
    const status=({running:'正在传输',done:'传输完成',cancelled:'已取消传输',error:'传输失败',choosing:'等待选择文件'})[progress.state]||'准备传输';
    $('files-transfer-title').textContent=progress.name?`${status} · ${progress.name}`:status;
    const direction=progress.direction==='upload'?'电脑 → 手机':'手机 → 电脑';
    $('files-transfer-detail').textContent=progress.error|| (progress.state==='choosing'?'请在系统对话框中选择文件或保存位置。':`${direction} · ${fileSize(progress.done||0)} / ${fileSize(progress.total||0)}${running?' · 文件校验完成后才会显示为传输成功':''}`);
    $('files-cancel').hidden=!running;$('files-cancel').disabled=state.cancelling;
    if(running&&state.cancelling)$('files-transfer-detail').textContent='正在取消并清理临时文件…';
    const meter=$('files-progress');meter.hidden=progress.state==='choosing';meter.value=progress.state==='done'?100:progress.total?Math.min(100,progress.done/progress.total*100):0;
    if(running&&!progress.total)meter.removeAttribute('value');
    $('files-transfer').dataset.state=progress.state;
    $('files-transfer-icon').textContent=progress.state==='done'?'✓':progress.direction==='upload'?'↑':'↓';
  }
  async function transfer(direction,droppedPaths=null){
    if(working()||modalOpen()||!state.path)return;closeMenu();const item=chosen();if(direction==='download'&&item?.kind!=='file')return;
    state.busy=true;state.cancelling=false;error();update();
    let timer,previous='',polling=false,finished=false;
    try{
      previous=(await fileCall('GetFileTransfer')).id;
      showProgress({state:droppedPaths?'preparing':'choosing',direction,name:droppedPaths?`准备上传 ${droppedPaths.length} 个文件`:''});
      timer=setInterval(async()=>{if(polling)return;polling=true;try{const p=await fileCall('GetFileTransfer');if(!finished&&p.id&&p.id!==previous)showProgress(p);}catch{}finally{polling=false;}},150);
      const changed=droppedPaths?await fileCall('UploadDroppedFiles',serial,state.scope,state.path,droppedPaths):await fileCall(direction==='upload'?'UploadFile':'DownloadFile',serial,state.scope,direction==='upload'?state.path:item.path);
      if(changed)showProgress(await fileCall('GetFileTransfer'));
      else{$('files-transfer-title').textContent='已取消文件选择';$('files-transfer-detail').textContent='可以重新选择文件进行传输。';$('files-progress').hidden=true;$('files-cancel').hidden=true;}
    }catch(e){
      error(message(e));
      try{const p=await fileCall('GetFileTransfer');if(p.id&&p.id!==previous)showProgress(p);else showProgress({state:'error',direction,error:message(e)});}catch{}
    }finally{finished=true;clearInterval(timer);state.busy=false;update();}
    if(direction==='upload')await browse();
  }
  function openName(kind){
    if(working()||modalOpen()||!state.path)return;closeMenu();const item=chosen();if(kind==='rename'&&!item)return;
    state.dialog={kind,source:item?.path};$('files-name-title').textContent=kind==='rename'?'重命名':'新建文件夹';$('files-name-input').value=kind==='rename'?item.name:'';$('files-name-error').textContent='';$('files-name-dialog').showModal();$('files-name-input').focus();$('files-name-input').select();
  }
  function closeName(){if(!working()){$('files-name-dialog').close();state.dialog=null;}}
  $('files-name-dialog').addEventListener('cancel',event=>{if(working())event.preventDefault();});
  $('files-name-form').onsubmit=async event=>{
    event.preventDefault();if(working()||!state.dialog)return;
    const name=$('files-name-input').value;
    if(!name||name==='.'||name==='..'||/[\\/\x00]/.test(name)){$('files-name-error').textContent='请输入有效名称，不能含路径分隔符。';return;}
    state.busy=true;update();let success=false;
    try{const d=state.dialog;await fileCall(d.kind==='mkdir'?'CreateFileDirectory':'RenameFile',serial,state.scope,d.kind==='mkdir'?state.path:d.source,name);success=true;}
    catch(e){$('files-name-error').textContent=message(e);}
    finally{state.busy=false;update();}
    if(success){closeName();await browse();}
  };
  $('files-name-close').onclick=closeName;$('files-name-cancel').onclick=closeName;
  function openDelete(){
    closeMenu();const item=chosen();if(working()||modalOpen()||!item)return;
    deleteTarget={path:item.path,scope:state.scope};deleteAttempted=false;
    $('files-delete-title').textContent=item.kind==='directory'?'删除文件夹':'确认删除';
    $('files-delete-warning').textContent=item.kind==='directory'?'将删除此文件夹及其中的所有内容，删除后无法恢复。':item.kind==='link'?'将删除此符号链接，链接指向的内容保留。删除后无法恢复。':'将从手机中删除此文件，删除后无法恢复。';
    $('files-delete-path').textContent=item.path;$('files-delete-error').textContent='';
    deleteDialog.showModal();update();$('files-delete-cancel').focus();
  }
  function closeDelete(){
    if(working())return;deleteDialog.close();deleteTarget=null;update();
    if(deleteAttempted){deleteAttempted=false;void browse();}
  }
  $('files-delete-form').onsubmit=async event=>{
    event.preventDefault();if(working()||!deleteDialog.open||!deleteTarget)return;
    state.busy=true;deleteAttempted=true;error();$('files-delete-error').textContent='';update();let success=false;
    try{await fileCall('DeleteFile',serial,deleteTarget.scope,deleteTarget.path);if(clipboard?.scope===deleteTarget.scope&&clipboard.path===deleteTarget.path)clipboard=null;success=true;}
    catch(e){$('files-delete-error').textContent=`${message(e)}。目录内容可能已发生变化，关闭后会刷新。`;}
    finally{state.busy=false;update();}
    if(success){deleteAttempted=false;closeDelete();await browse();}
  };
  $('files-delete-close').onclick=closeDelete;$('files-delete-cancel').onclick=closeDelete;
  deleteDialog.addEventListener('cancel',event=>{event.preventDefault();closeDelete();});
  let deleteBackdrop=false;
  const outsideDelete=event=>{const r=deleteDialog.getBoundingClientRect();return event.target===deleteDialog&&(event.clientX<r.left||event.clientX>r.right||event.clientY<r.top||event.clientY>r.bottom);};
  deleteDialog.addEventListener('pointerdown',event=>{deleteBackdrop=outsideDelete(event);});
  deleteDialog.addEventListener('click',event=>{if(deleteBackdrop&&outsideDelete(event))closeDelete();deleteBackdrop=false;});
  $('files-menu-delete').onclick=openDelete;
  function copySelection(mode){
    closeMenu();const item=chosen();if(working()||modalOpen()||!item)return;
    clipboard={path:item.path,name:item.name,scope:state.scope,mode};error();update();
  }
  async function paste(){
    closeMenu();if(working()||modalOpen()||!state.path||!clipboard)return;
    if(clipboard.scope!==state.scope){error('请在同一文件位置内粘贴，暂不支持跨文件范围操作');return;}
    const source=clipboard;state.busy=true;error();update();let failure='';
    try{await fileCall('PasteFile',serial,source.scope,state.scope,source.path,state.path,source.mode);if(source.mode==='move')clipboard=null;}
    catch(e){failure=message(e);}
    finally{state.busy=false;update();}
    await browse();if(failure)error(failure);
  }
  $('files-menu-copy').onclick=()=>copySelection('copy');$('files-menu-cut').onclick=()=>copySelection('move');
  $('files-menu-paste').onclick=paste;$('files-paste').onclick=paste;
  $('files-menu-rename').onclick=()=>{closeMenu();openName('rename');};
  $('files-menu-download').onclick=()=>{closeMenu();void transfer('download');};
  document.addEventListener('pointerdown',event=>{if(!menu.contains(event.target))closeMenu();});
  $('files-table-wrap').addEventListener('contextmenu',event=>{if(!event.target.closest('#files-entries tr'))openMenu(event);});
  document.addEventListener('contextmenu',event=>{if(!event.target.closest('#files-table-wrap'))closeMenu();});
  window.addEventListener('resize',closeMenu);
  document.addEventListener('scroll',closeMenu,true);
  menu.addEventListener('keydown',event=>{
    const items=[...menu.querySelectorAll('button')].filter(button=>!button.disabled),index=items.indexOf(document.activeElement);
    if(event.key==='ArrowDown'||event.key==='ArrowUp'){event.preventDefault();items[(index+(event.key==='ArrowDown'?1:items.length-1))%items.length]?.focus();}
  });
  $('files-mkdir').onclick=()=>openName('mkdir');$('files-rename').onclick=()=>openName('rename');
  $('files-upload').onclick=()=>void transfer('upload');$('files-download').onclick=()=>void transfer('download');
  $('files-cancel').onclick=async()=>{state.cancelling=true;$('files-cancel').disabled=true;try{await fileCall('CancelFileTransfer');$('files-transfer-detail').textContent='正在取消并清理临时文件…';}catch(e){state.cancelling=false;error(message(e));$('files-cancel').disabled=false;}};
  $('files-refresh').onclick=()=>void browse();$('files-up').onclick=()=>void browse(parentPath(state.path));
  $('files-path-form').onsubmit=event=>{event.preventDefault();void browse($('files-path').value);};
  $('files-search').oninput=render;
  for(const sort of ['name','size'])$('files-sort-'+sort).onclick=()=>{state.descending=state.sort===sort?!state.descending:false;state.sort=sort;render();};
  $('files-load-apps').onclick=async()=>{
    if(working())return;state.busy=true;error();update();
    try{state.apps=await fileCall('FileSharedApps',serial)||[];locations();$('files-apps-hint').textContent=state.apps.length?'':'没有找到允许共享文档的应用';}
    catch(e){error(message(e));}finally{state.busy=false;update();}
  };
  window.addEventListener('keydown',event=>{
    if(event.key==='Escape')closeMenu();
    const editing=['INPUT','TEXTAREA','SELECT'].includes(event.target.tagName);
    if(event.key==='F5'){event.preventDefault();if(!modalOpen())void browse();return;}
    if(event.ctrlKey&&event.key.toLowerCase()==='l'){event.preventDefault();$('files-path').focus();$('files-path').select();return;}
    if(editing||working()||modalOpen())return;
    if(event.key==='Enter'&&event.target.tagName==='BUTTON')return;
    if(event.ctrlKey&&!event.altKey&&!event.metaKey){
      const key=event.key.toLowerCase();
      if(['c','x','v'].includes(key)){event.preventDefault();if(key==='v')void paste();else copySelection(key==='x'?'move':'copy');return;}
    }
    if(event.key==='Backspace'){event.preventDefault();if(state.path!=='/')void browse(parentPath(state.path));}
    if(event.key==='F2'){event.preventDefault();openName('rename');}
    if(event.key==='Enter'){const item=chosen();if(item&&['directory','link'].includes(item.kind)){event.preventDefault();void browse(item.path);}}
  });
  $('files-device-serial').textContent=serial;
  fileDrop?.setTarget(paths=>transfer('upload',paths),()=>({
    allowed:!!state.path&&!working()&&!modalOpen(),
    title:working()?'请等待当前文件操作完成':$('files-name-dialog').open?'请先关闭名称对话框':state.path?'松开以上传到当前手机目录':'请先打开手机目录',
    detail:state.path?`${locationName()} · ${state.path}`:'选择左侧位置后即可拖入电脑文件。'
  }));
  // Render navigation before connecting, so a missing AFC2 service can still be
  // recovered by choosing Media or shared app documents.
  state.platform='ios';locations();
  try{const devices=await fileCall('GetDevices');const device=devices.find(d=>d.serial===serial);if(device){state.platform=device.platform;$('files-device-name').textContent=device.model;document.title=`${device.model} · 文件管理`;window.runtime?.WindowSetTitle(document.title);}}catch{}
  $('files-device-platform').textContent=state.platform==='ios'?'iOS · USB':'Android · ADB';locations();
  const saved=await fileCall('GetFileLocation',serial).catch(()=>null);
  if(saved?.scope){
    if(saved.scope.startsWith('app:'))state.apps=[{id:saved.scope.slice(4),name:'上次的应用文档'}];
    if(await browse(saved.path,saved.scope))return;
  }
  await browse('','');
  if(saved?.scope){$('files-hint').textContent='上次的目录当前不可访问，已打开默认位置。';$('files-hint').hidden=false;}
}
