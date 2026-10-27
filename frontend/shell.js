const $=id=>document.getElementById(id);
export async function shellCall(method,serial,store){
  const binding=window.go?.main?.App;
  if(binding)return binding[method](...(method==='OpenOpenSSHStore'?[serial,store]:[serial]));
  const response=await fetch('/api',{method:'POST',headers:{'Content-Type':'application/json',Authorization:`Bearer ${sessionStorage.getItem('goscrcpy-token')}`},body:JSON.stringify({method,serial,store})});
  const result=await response.json();if(!response.ok)throw new Error(result.error||'无法打开手机 Shell');return result;
}
export function initializeShell({notice,onBusy=()=>{}}){
  const dialog=$('openssh-dialog');let serial='',busy=false,pressedOutside=false;
  const update=()=>{for(const id of ['openssh-sileo','openssh-cydia','openssh-close'])$(id).disabled=busy;};
  const outside=e=>{if(e.target!==dialog)return false;const r=dialog.getBoundingClientRect();return e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom;};
  dialog.addEventListener('pointerdown',e=>{pressedOutside=outside(e);});
  dialog.addEventListener('pointercancel',()=>{pressedOutside=false;});
  dialog.addEventListener('click',e=>{const close=pressedOutside&&outside(e)&&!busy;pressedOutside=false;if(close){e.preventDefault();e.stopPropagation();dialog.close();}});
  dialog.addEventListener('cancel',e=>{if(busy)e.preventDefault();});
  $('openssh-close').onclick=()=>{if(!busy)dialog.close();};
  async function install(store){
    if(busy)return;busy=true;onBusy(true);update();$('openssh-status').textContent='正在打开手机上的 '+(store==='sileo'?'Sileo':'Cydia')+'…';
    try{await shellCall('OpenOpenSSHStore',serial,store);dialog.close();notice('已发送 OpenSSH 安装页跳转。安装并启动服务后，再次打开 Shell。');}
    catch(e){$('openssh-status').textContent=String(e.message||e);}
    finally{busy=false;onBusy(false);update();}
  }
  $('openssh-sileo').onclick=()=>install('sileo');$('openssh-cydia').onclick=()=>install('cydia');
  return {async open(target){
    if(busy)return;serial=target;busy=true;onBusy(true);update();notice('正在打开手机 Shell…');
    try{
      const result=await shellCall('OpenShell',serial);
      if(result.missingOpenSSH){$('openssh-status').textContent='';dialog.showModal();}
      else notice(result.warning||'已打开独立 Shell 窗口，可使用手机命令行。');
    }catch(e){notice(String(e.message||e),true);}
    finally{busy=false;onBusy(false);update();}
  }};
}
