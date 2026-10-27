const $=id=>document.getElementById(id);
function closeOnBackdrop(dialog,canClose){
  let pressedOutside=false;
  const outside=event=>{
    if(event.target!==dialog)return false;
    const rect=dialog.getBoundingClientRect();
    return event.clientX<rect.left||event.clientX>rect.right||event.clientY<rect.top||event.clientY>rect.bottom;
  };
  dialog.addEventListener('pointerdown',event=>{pressedOutside=outside(event);});
  dialog.addEventListener('pointercancel',()=>{pressedOutside=false;});
  dialog.addEventListener('click',event=>{
    const close=pressedOutside&&outside(event)&&canClose();pressedOutside=false;
    if(close){event.preventDefault();event.stopPropagation();dialog.close();}
  });
}
export async function proxyCall(method,serial,options){
  const binding=window.go?.main?.App;
  if(binding){
    if(method==='LoadProxyLines')return binding[method]();
    return binding[method](...(['ChangeProxy','RememberProxyOptions','OpenIOSCurlStore'].includes(method)?[serial,options]:[serial]));
  }
  const response=await fetch('/api',{method:'POST',headers:{'Content-Type':'application/json',Authorization:`Bearer ${sessionStorage.getItem('goscrcpy-token')}`},body:JSON.stringify({method,serial,...(method==='OpenIOSCurlStore'?{store:options}:{proxyOptions:options})})});
  const result=await response.json();if(!response.ok)throw new Error(result.error||'代理操作失败');return result;
}
export function initializeProxy({notice,onBusy=()=>{}}){
  const dialog=$('proxy-dialog');
  const curlDialog=$('ios-curl-dialog');
  let serial='',platform='',lines=[],busy=false,loading=false,preferred='',save=Promise.resolve();
  closeOnBackdrop(dialog,()=>!busy);
  closeOnBackdrop(curlDialog,()=>!busy);
  const options=()=>({proxyId:$('proxy-line').value,region:$('proxy-region').value.toUpperCase().trim(),appId:$('proxy-appid').value.trim(),name:$('proxy-name').value.trim()});
  function status(message,error=false){$('proxy-status').textContent=message;$('proxy-status').classList.toggle('error',error);}
  function update(){
    for(const id of ['proxy-refresh','proxy-line','proxy-region','proxy-appid','proxy-name','proxy-current-app'])$(id).disabled=busy||loading;
    $('proxy-line').disabled||=!lines.length;
    $('proxy-change').disabled=busy||loading||!lines.length;
    $('proxy-query').disabled=busy||loading;
    $('proxy-close').disabled=busy;
    $('proxy-change').textContent=busy?'正在操作…':platform==='android'?'导入 Clash 配置':'换 IP';
    const line=lines.find(l=>l.id===$('proxy-line').value);
    $('proxy-line-info').textContent=line?`${line.hostname}:${line.port} · SOCKS5`:'选择代理线路，每次换 IP 生成新会话';
  }
  function remember(){
    const o=options();if(!/^[A-Z]{2}$/.test(o.region)||!o.appId||!o.name)return;
    const target=serial;save=save.then(()=>proxyCall('RememberProxyOptions',target,o)).catch(e=>status('无法记住代理设置：'+String(e.message||e),true));
  }
  async function refresh(){
    if(busy||loading)return;loading=true;const previous=$('proxy-line').value||preferred;
    lines=[];$('proxy-line').replaceChildren(new Option('正在加载代理…',''));update();status('正在获取最新代理…');
    try{
      const result=await proxyCall('LoadProxyLines');lines=result.lines;
      $('proxy-line').replaceChildren(...lines.map(l=>new Option(l.name,l.id)));
      if(!lines.length)$('proxy-line').add(new Option('暂无可用代理',''));
      if(lines.some(l=>l.id===previous))$('proxy-line').value=previous;
      status(`已加载 ${lines.length} 条线路${result.skipped?`，忽略 ${result.skipped} 条无效记录`:''}`);
    }catch(e){$('proxy-line').replaceChildren(new Option('加载失败，请刷新',''));status(String(e.message||e),true);}
    finally{loading=false;update();}
  }
  async function perform(method){
    if(busy||loading)return;
    const o=options();
    if(method==='ChangeProxy'&&!/^[A-Z]{2}$/.test(o.region)){status('请输入两位地区代码，例如 US、JP、HK',true);$('proxy-region').focus();return;}
    busy=true;onBusy(true);update();
    status(method==='QueryProxyIP'?'正在手机上查询出口 IP…':platform==='ios'?'正在切换 Shadowrocket 代理并检查 VPN 状态…':'正在打开 Clash 配置导入页…');
    if(method==='ChangeProxy'){$('proxy-ip').hidden=true;$('proxy-last').hidden=true;}
    try{
      await save;
      const result=await proxyCall(method,serial,o);
      if(method==='ChangeProxy'){
        status(result.message);
        $('proxy-last').textContent=`${result.pending?'待手机确认':'上次切换'}：${result.proxyName} / ${result.region} · ${new Date().toLocaleTimeString('zh-CN')}`;
        $('proxy-last').hidden=false;
        if(result.pending)notice('Clash 导入页已打开，关闭此面板后在投屏中保存并启用配置。');
      }else{
        if(platform==='ios'&&result.missingCurl){
          $('proxy-ip').hidden=true;status('手机未安装 curl，请选择商店前往安装。',true);
          $('ios-curl-status').textContent='';curlDialog.showModal();return;
        }
        $('proxy-ip-address').textContent=result.query;
        $('proxy-ip-location').textContent=[result.country,result.regionName,result.city].filter(Boolean).join(' · ');
        $('proxy-ip-detail').textContent=[result.isp,result.org,result.timezone].filter(Boolean).join(' · ');
        $('proxy-ip-time').textContent='查询于 '+new Date().toLocaleTimeString('zh-CN');
        $('proxy-ip').hidden=false;status('已读取手机出口 IP');
      }
    }catch(e){status(String(e.message||e),true);}
    finally{busy=false;onBusy(false);update();}
  }
  $('proxy-refresh').onclick=()=>refresh();
  $('proxy-change').onclick=()=>perform('ChangeProxy');
  $('proxy-query').onclick=()=>perform('QueryProxyIP');
  $('proxy-close').onclick=()=>dialog.close();
  function updateCurlDialog(){for(const id of ['ios-curl-sileo','ios-curl-cydia','ios-curl-close'])$(id).disabled=busy;}
  async function openCurlStore(store){
    if(busy)return;busy=true;onBusy(true);update();updateCurlDialog();
    $('ios-curl-status').textContent='正在打开手机上的 '+(store==='sileo'?'Sileo':'Cydia')+'…';
    try{
      await proxyCall('OpenIOSCurlStore',serial,store);
      curlDialog.close();dialog.close();notice('已发送 curl 安装页跳转。安装完成后，重新点击「查询 IP」。');
    }catch(e){$('ios-curl-status').textContent=String(e.message||e);}
    finally{busy=false;onBusy(false);update();updateCurlDialog();}
  }
  $('ios-curl-sileo').onclick=()=>openCurlStore('sileo');
  $('ios-curl-cydia').onclick=()=>openCurlStore('cydia');
  $('ios-curl-close').onclick=()=>{if(!busy)curlDialog.close();};
  curlDialog.addEventListener('cancel',event=>{if(busy)event.preventDefault();});
  dialog.addEventListener('cancel',event=>{if(busy)event.preventDefault();});
  for(const id of ['proxy-line','proxy-region','proxy-appid','proxy-name'])$(id).addEventListener('change',()=>{remember();update();});
  $('proxy-region').addEventListener('input',()=>{$('proxy-region').value=$('proxy-region').value.toUpperCase();});
  $('proxy-current-app').onclick=async()=>{
    if(busy||loading)return;busy=true;onBusy(true);update();
    try{
      const b=window.go?.main?.App;
      let name;
      if(b)name=await b.DeviceTool(serial,'package');
      else{
        const response=await fetch('/api',{method:'POST',headers:{'Content-Type':'application/json',Authorization:`Bearer ${sessionStorage.getItem('goscrcpy-token')}`},body:JSON.stringify({method:'DeviceTool',serial,action:'package'})});
        name=await response.json();if(!response.ok)throw new Error(name.error);
      }
      $('proxy-appid').value=name;remember();status('已填入当前应用包名');
    }catch(e){status(String(e.message||e),true);}
    finally{busy=false;onBusy(false);update();}
  };
  return {async open(device){
    if(busy||loading)return;
    serial=device.serial;platform=device.platform;preferred='';lines=[];
    $('proxy-platform').textContent=(platform==='ios'?'Shadowrocket':'Clash')+' · '+device.model;
    $('proxy-android').hidden=platform!=='android';
    $('proxy-query-help').textContent=platform==='ios'?'查询在手机上执行，需要手机已安装 curl。':'查询在手机上执行，缺少 curl 时自动推送程序内置版本。';
    $('proxy-help').textContent=platform==='ios'?'每次切换生成新会话，并检查手机 VPN 是否开启。出口分配由代理供应商决定。':'使用 Clash 导入链接下载配置。在手机保存、选择新配置并启用 Clash 后生效。';
    $('proxy-ip').hidden=true;$('proxy-last').hidden=true;
    $('proxy-line').replaceChildren(new Option('正在加载…',''));loading=true;update();dialog.showModal();
    try{
      const saved=await proxyCall('GetProxyOptions',serial);preferred=saved.proxyId;
      $('proxy-region').value=saved.region;$('proxy-appid').value=saved.appId;$('proxy-name').value=saved.name;
    }catch(e){status(String(e.message||e),true);loading=false;update();return;}
    loading=false;await refresh();
  }};
}
