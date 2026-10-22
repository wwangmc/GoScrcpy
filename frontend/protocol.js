const encoder = new TextEncoder();
export function keyPacket(code, action = 0, repeat = 0, meta = 0) {
  const b = new Uint8Array(14), v = new DataView(b.buffer);
  b[0] = 0; b[1] = action; v.setUint32(2, code); v.setUint32(6, repeat); v.setUint32(10, meta); return b;
}
export function textPacket(text, paste = false) {
  const data = encoder.encode(text);
  if (data.length > (paste ? 65000 : 300)) throw new Error('文字过长，请分段发送');
  const offset = paste ? 14 : 5, b = new Uint8Array(offset + data.length), v = new DataView(b.buffer);
  b[0] = paste ? 8 : 1;
  if (paste) b[9] = 1;
  v.setUint32(offset - 4, data.length); b.set(data, offset); return b;
}
function position(v, offset, p) { v.setUint32(offset, p.x); v.setUint32(offset+4, p.y); v.setUint16(offset+8, p.width); v.setUint16(offset+10, p.height); }
export function touchPacket(action, p) {
  const b = new Uint8Array(32), v = new DataView(b.buffer); b[0] = 2; b[1] = action;
  v.setBigUint64(2, 0xfffffffffffffffen); position(v, 10, p); v.setUint16(22, action === 1 || action === 3 ? 0 : 65535); return b;
}
export function scrollPacket(p, dx, dy) {
  const b = new Uint8Array(21), v = new DataView(b.buffer); b[0] = 3; position(v, 1, p);
  const fixed = n => Math.max(-32768, Math.min(32767, Math.round(n * 2048)));
  v.setInt16(13, fixed(dx)); v.setInt16(15, fixed(dy)); return b;
}
export function spsCodec(data) {
  for (let i = 0; i + 7 < data.length; i++) {
    let start = -1;
    if (data[i] === 0 && data[i+1] === 0 && data[i+2] === 1) start = i+3;
    else if (data[i] === 0 && data[i+1] === 0 && data[i+2] === 0 && data[i+3] === 1) start = i+4;
    if (start >= 0 && (data[start] & 31) === 7) return 'avc1.' + [...data.slice(start+1,start+4)].map(n=>n.toString(16).padStart(2,'0')).join('');
  }
  throw new Error('H.264 配置缺少 SPS');
}
export function parsePacket(buffer) {
  const v = new DataView(buffer), b = new Uint8Array(buffer);
  if (b.length < 13 || v.getUint32(8) !== b.length-12) throw new Error('视频包损坏');
  const flags = v.getBigUint64(0);
  return { config: !!(flags & (1n<<63n)), key: !!(flags & (1n<<62n)), timestamp: Number(flags & ((1n<<62n)-1n)), data: b.subarray(12) };
}
