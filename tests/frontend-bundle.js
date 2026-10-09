const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const html = fs.readFileSync('panel/index.html','utf8');
for (const match of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)) {
  if (!/\bsrc\s*=/.test(match[1]) && !/application\/ld\+json/.test(match[1])) new vm.Script(match[2]);
}
const start = html.indexOf('function parseBundleClient(s) {');
const end = html.indexOf('// Pasting a bundle',start);
assert.ok(start>=0 && end>start);
const parse = vm.runInNewContext(html.slice(start,end)+'; parseBundleClient',{TextDecoder,Uint8Array,atob});
const legacy = parse('hsh1_192.0.2.10_17001_10.70.1.1_10.70.1.2_syntheticToken123456_8888-8889_fou443-55555_tr-kcp');
assert.equal(legacy.transport,'kcp');
const data = {frp_transport:'kcp',local_pub:'192.0.2.10',frp_port:17001,local_gre:'10.70.1.1',peer_gre:'10.70.1.2',token:'synthetic',raw_ports:['tcp:8888','udp:8889']};
const managed = parse('hsh2_'+Buffer.from(JSON.stringify(data)).toString('base64url'));
assert.equal(managed.transport,'kcp'); assert.equal(managed.port,17001); assert.equal(managed.ports[1],'udp:8889');
assert.equal(parse('hsh2_invalid'),null);
assert.ok(parse('hsh1_192.0.2.10_17001_10.70.1.1_10.70.1.2_token_tr-invalid').error);
assert.ok(!html.includes("showPeerToken(${p.id}, '${esc(nm)}')"));
assert.ok(!html.includes("classList.toggle('hidden', eng === 'frp');\n  if ($('engHint'))"));
assert.ok(html.includes('class="fg" id="wrapIranPorts"'));
const portStart=html.indexOf('function validManagedPortInput(value) {');
const portEnd=html.indexOf('function addEditPort()',portStart);
const validPort=vm.runInNewContext(html.slice(portStart,portEnd)+'; validManagedPortInput');
for(const port of ['tcp:8888','udp:2052','tcp:8080=80','udp:8880-8889','tcp:8000-8002=80-82']) assert.ok(validPort(port),port);
for(const port of ['tcp:0','udp:65536','tcp:3-1','udp:80=90-91','80;id',"80');alert(1)"]) assert.ok(!validPort(port),port);
assert.ok(html.includes("if (transportVal && engineVal !== 'frp') payload.transport = transportVal"));
const renderStart=html.indexOf('function renderTunnel(st) {');
const renderEnd=html.indexOf('// shared cache:',renderStart);
const elements={};
const render=vm.runInNewContext(html.slice(renderStart,renderEnd)+'; renderTunnel',{
  $: id => elements[id] ||= {classList:{add(){},remove(){}},querySelectorAll(){return []}},
  document:{activeElement:null},window:{},esc:String,ICO_SRV:'server',ICO_GRE:'GRE',ICO_FRP:'FRP',renderTunChips(){}
});
render({role:'iran',gre:{exists:true,name:'gre-tunnel'},frp_up:true,proxy_ports:[8888],proxies:[]});
assert.ok(!html.includes("peerRemoveClick(${p.id}, '${esc(nm)}')"));
console.log('PASS inline JavaScript syntax, secure/legacy bundle transport, malformed bundle and peer-name handler');
