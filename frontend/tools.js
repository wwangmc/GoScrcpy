export async function runDeviceTool(action,serial,call){
  let clipboard='';
  if(action==='store'){
    try{
      clipboard=window.runtime?await window.runtime.ClipboardGetText():await navigator.clipboard.readText();
    }catch{
      throw new Error('无法读取电脑剪贴板，请允许剪贴板访问后重试。');
    }
    if(!clipboard?.trim())throw new Error('电脑剪贴板中没有文字，请先复制 Android 应用包名。');
  }
  return call('DeviceTool',serial,action,clipboard);
}
