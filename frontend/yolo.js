// PhoneYOLO coordinates are oriented screen pixels; the overlay uses the video
// canvas dimensions and identical object-fit CSS, including its letterboxing.
export function projectDetections(result,width,height){
  const screen=result?.screen;
  if(result?.type!=='detections'||!screen||!Array.isArray(result.detections)||
    ![screen.width,screen.height,width,height].every(n=>Number.isFinite(n)&&n>0)||
    screen.coordinateSystem!=='top-left-pixels')return null;
  // During rotation video and detection streams may briefly disagree.
  if(Math.abs((screen.width/screen.height)/(width/height)-1)>.04)return null;
  const boxes=[];
  for(const item of result.detections.slice(0,300)){
    const b=item?.box,c=item?.confidence;
    if(!b||![b.x,b.y,b.width,b.height,c].every(Number.isFinite)||b.width<=0||b.height<=0||c<0||c>1)continue;
    const x=Math.max(0,b.x),y=Math.max(0,b.y),right=Math.min(screen.width,b.x+b.width),bottom=Math.min(screen.height,b.y+b.height);
    if(right<=x||bottom<=y)continue;
    boxes.push({x:x*width/screen.width,y:y*height/screen.height,width:(right-x)*width/screen.width,height:(bottom-y)*height/screen.height,
      label:String(item.label??item.classId??'目标').slice(0,80),confidence:c});
  }
  return boxes;
}

export function initializeYOLO({canvas,overlay,button,status,getURL,notice,
  createSocket=url=>new WebSocket(url),setTimer=setTimeout,clearTimer=clearTimeout}){
  const context=overlay.getContext('2d');
  let enabled=false,live=false,serial='',socket=null,generation=0,result=null,staleTimer=null,retryTimer=null,lastRender=null;
  function clear(){result=null;lastRender=null;context.clearRect(0,0,overlay.width,overlay.height);overlay.hidden=true;}
  function message(text,error=false){if(status.textContent!==text)status.textContent=text;status.classList.toggle('error',error);status.hidden=false;}
  function release(){
    ++generation;clearTimer(staleTimer);clearTimer(retryTimer);staleTimer=null;retryTimer=null;
    if(socket){socket.onclose=null;socket.onmessage=null;socket.onerror=null;socket.close();socket=null;}
    clear();
  }
  function disable(){enabled=false;release();button.classList.remove('active');button.setAttribute('aria-pressed','false');status.hidden=true;}
  function fatal(text){disable();message(text,true);notice(text,true);}
  function draw(){
    if(!enabled||!result)return;
    const bounds=canvas.getBoundingClientRect(),key=[result,canvas.width,canvas.height,canvas.hidden,bounds.width,bounds.height];
    if(lastRender&&key.every((value,index)=>value===lastRender[index]))return;
    lastRender=key;
    const boxes=projectDetections(result,canvas.width,canvas.height);
    if(overlay.width!==canvas.width)overlay.width=canvas.width;
    if(overlay.height!==canvas.height)overlay.height=canvas.height;
    context.clearRect(0,0,overlay.width,overlay.height);
    overlay.hidden=canvas.hidden||boxes===null;
    if(boxes===null){message('YOLO · 等待画面方向同步');return;}
    const scale=Math.min(bounds.width/canvas.width,bounds.height/canvas.height)||1;
    const font=12/scale,padding=3/scale;
    context.lineWidth=2/scale;context.font=`600 ${font}px system-ui`;context.textBaseline='top';
    for(const b of boxes){
      const text=`${b.label} ${Math.round(b.confidence*100)}%`;
      const labelWidth=Math.min(canvas.width,context.measureText(text).width+padding*2),labelHeight=font+padding*2;
      const x=Math.max(0,Math.min(b.x,canvas.width-labelWidth)),y=Math.max(0,Math.min(b.y-labelHeight,canvas.height-labelHeight));
      context.strokeStyle='#42edb0';context.strokeRect(b.x,b.y,b.width,b.height);
      context.fillStyle='#153d30e8';context.fillRect(x,y,labelWidth,labelHeight);
      context.fillStyle='#8affd2';context.fillText(text,x+padding,y+padding,Math.max(1,labelWidth-padding*2));
    }
    const latency=Number.isFinite(result.latencyMs)?` · ${Math.round(result.latencyMs)} ms`:'';
    message(`YOLO · ${boxes.length} 个目标${latency}`);
  }
  async function connect(){
    release();const epoch=generation;message('YOLO · 正在连接…');
    try{
      const url=await getURL(serial);
      if(epoch!==generation||!enabled||!live)return;
      const ws=createSocket(url);socket=ws;
      ws.onmessage=event=>{
        if(epoch!==generation)return;
        let data;try{data=JSON.parse(event.data);}catch{clear();message('YOLO · 检测数据无效',true);return;}
        if(!data||typeof data!=='object'){clear();message('YOLO · 检测数据无效',true);return;}
        if(data.type==='error'){
          if(data.fatal){fatal(String(data.message||'YOLO 连接失败'));return;}
          clear();message(`YOLO · ${data.message||'检测失败，等待重试'}`,true);return;
        }
        if(data.type!=='detections')return;
        result=data;draw();clearTimer(staleTimer);
        staleTimer=setTimer(()=>{if(epoch===generation&&enabled){clear();message('YOLO · 等待新的检测结果');}},2000);
      };
      ws.onerror=()=>{if(epoch===generation){clear();message('YOLO · 连接异常',true);}};
      ws.onclose=()=>{
        if(epoch!==generation||!enabled)return;
        socket=null;clear();clearTimer(staleTimer);message('YOLO · 连接断开，正在重连…',true);
        retryTimer=setTimer(()=>{if(enabled&&live&&epoch===generation)void connect();},3000);
      };
    }catch(error){if(epoch===generation)fatal(String(error.message||error));}
  }
  button.onclick=()=>{
    if(enabled){disable();return;}
    if(!live)return;
    enabled=true;button.classList.add('active');button.setAttribute('aria-pressed','true');void connect();
  };
  return {
    update(device,isLive){
      const supported=device?.platform==='ios';
      if(enabled&&(!isLive||!supported||device.serial!==serial))disable();
      serial=device?.serial||'';live=!!isLive&&supported;
      button.hidden=!supported;button.disabled=!live;
    },
    draw,stop:disable
  };
}
