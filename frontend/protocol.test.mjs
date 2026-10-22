import {test} from 'node:test';
import assert from 'node:assert/strict';
import {keyPacket,textPacket,touchPacket,scrollPacket,spsCodec,parsePacket} from './protocol.js';
const hex=data=>Buffer.from(data).toString('hex');
test('control wire format matches scrcpy 3.3.4',()=>{
  assert.equal(hex(keyPacket(3)),'0000000000030000000000000000');
  assert.equal(hex(textPacket('abc')),'0100000003616263');
  assert.equal(hex(textPacket('abc',true)),'0800000000000000000100000003616263');
  const p={x:100,y:200,width:1080,height:1920};
  assert.equal(hex(touchPacket(0,p)),'0200fffffffffffffffe00000064000000c804380780ffff0000000000000000');
  assert.equal(hex(scrollPacket(p,0,-1)),'0300000064000000c8043807800000f80000000000');
  assert.equal(new DataView(touchPacket(1,p).buffer).getUint16(22),0);
  assert.equal(new TextDecoder().decode(textPacket('你好',true).slice(14)),'你好');
});
test('Annex B SPS determines actual decoder profile',()=>{
  assert.equal(spsCodec(Uint8Array.from([0,0,0,1,0x67,0x64,0,0x28,0,0])),'avc1.640028');
  assert.equal(spsCodec(Uint8Array.from([0,0,1,0x67,0x42,0x80,0x1f,0,0])),'avc1.42801f');
  assert.throws(()=>spsCodec(new Uint8Array(12)));
});
test('packet flags and microsecond timestamps are independent',()=>{
  const b=new ArrayBuffer(16),v=new DataView(b);v.setBigUint64(0,(1n<<62n)|123456n);v.setUint32(8,4);
  const p=parsePacket(b);assert.equal(p.key,true);assert.equal(p.config,false);assert.equal(p.timestamp,123456);assert.equal(p.data.length,4);
  v.setUint32(8,5);assert.throws(()=>parsePacket(b));
});
