const $=id=>document.getElementById(id);
export async function iosSettingsCall(method,serial,region){
  const binding=window.go?.main?.App;
  if(binding)return binding[method](...(method==='GetIOSSettings'?[serial]:[serial,region]));
  const response=await fetch('/api',{method:'POST',headers:{'Content-Type':'application/json',Authorization:`Bearer ${sessionStorage.getItem('goscrcpy-token')}`},body:JSON.stringify({method,serial,region})});
  const result=await response.json();if(!response.ok)throw new Error(result.error||'iOS 设置操作失败');return result;
}
export function initializeIOSSettings({notice,onBusy=()=>{}}){
  const dialog=$('ios-settings-dialog');let serial='',choices=[],filtered=[],selectedRegion='',busy=false,pressedOutside=false,save=Promise.resolve();
  const status=(message,error=false)=>{$('ios-settings-status').textContent=message;$('ios-settings-status').classList.toggle('error',error);};
  function preview(){
    const choice=choices.find(c=>c.region===$('ios-settings-region').value);
    $('ios-settings-language').textContent=choice?.language||'—';$('ios-settings-locale').textContent=choice?.locale||'—';
    $('ios-settings-timezone').textContent=choice?.timeZone||'—';$('ios-settings-carriers').textContent=choice?`${choice.carrierCount} 条`:'—';
    $('ios-settings-apply').disabled=busy||!choice;
  }
  function update(){
    $('ios-settings-close').disabled=busy;$('ios-settings-region').disabled=busy||!filtered.length;
    $('ios-settings-search').disabled=busy||!choices.length;
    $('ios-settings-apply').textContent=busy?'正在处理…':'一键应用';preview();
  }
  function filterRegions(){
    const query=$('ios-settings-search').value.trim().toLowerCase();
    filtered=choices.filter(c=>`${c.region} ${c.name} ${c.locale} ${c.language} ${c.timeZone}`.toLowerCase().includes(query));
    const keep=filtered.some(c=>c.region===selectedRegion);
    const options=filtered.map(c=>new Option(`${c.region} · ${c.name}`,c.region));
    if(!keep)options.unshift(new Option(filtered.length?'请选择匹配地区':'没有匹配的地区',''));
    $('ios-settings-region').replaceChildren(...options);$('ios-settings-region').value=keep?selectedRegion:'';
    $('ios-settings-search-status').textContent=query?`找到 ${filtered.length} / ${choices.length} 个地区`:`共 ${choices.length} 个地区`;
    update();
  }
  $('ios-settings-search').addEventListener('input',filterRegions);
  $('ios-settings-close').onclick=()=>{if(!busy)dialog.close();};
  dialog.addEventListener('cancel',e=>{if(busy)e.preventDefault();});
  const outside=e=>{if(e.target!==dialog)return false;const r=dialog.getBoundingClientRect();return e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom;};
  dialog.addEventListener('pointerdown',e=>{pressedOutside=outside(e);});dialog.addEventListener('pointercancel',()=>{pressedOutside=false;});
  dialog.addEventListener('click',e=>{const close=pressedOutside&&outside(e)&&!busy;pressedOutside=false;if(close){e.preventDefault();e.stopPropagation();dialog.close();}});
  $('ios-settings-region').addEventListener('change',()=>{
    preview();status('将同步设置语言、地区、时区及插件运营商参数。');
    const target=serial,region=$('ios-settings-region').value;
    if(!choices.some(c=>c.region===region))return;
    selectedRegion=region;
    save=save.then(()=>iosSettingsCall('RememberIOSSettingsRegion',target,region)).catch(e=>status('无法记住地区：'+String(e.message||e),true));
  });
  $('ios-settings-apply').onclick=async()=>{
    if(busy||!choices.some(c=>c.region===$('ios-settings-region').value))return;
    busy=true;onBusy(true);update();status('正在向手机提交设置…');
    try{await save;const message=await iosSettingsCall('ApplyIOSSettings',serial,$('ios-settings-region').value);status(message);notice(message);}
    catch(e){status(String(e.message||e),true);}
    finally{busy=false;onBusy(false);update();}
  };
  return {async open(device){
    if(busy||device?.platform!=='ios')return;
    serial=device.serial;choices=[];filtered=[];selectedRegion='';busy=true;onBusy(true);$('ios-settings-device').textContent=device.model||'iPhone';
    $('ios-settings-search').value='';$('ios-settings-search-status').textContent='';
    $('ios-settings-region').replaceChildren(new Option('正在加载地区…',''));update();status('正在读取 iOS 地区配置…');dialog.showModal();
    try{
      const data=await iosSettingsCall('GetIOSSettings',serial);choices=data.choices;
      selectedRegion=choices.some(c=>c.region===data.region)?data.region:choices[0]?.region||'';
      filterRegions();
      status('将同步设置语言、地区、时区及插件运营商参数。');
    }catch(e){status(String(e.message||e),true);}
    finally{busy=false;onBusy(false);update();}
  }};
}
