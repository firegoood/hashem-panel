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

// First-paint theme setup runs before dashboard/i18n initialization. It must
// not fetch an authenticated dashboard or create a rejected startup promise.
const themeStart = html.indexOf('function setTheme(t) {');
const themeEnd = html.indexOf("$('themeBtn').onclick", themeStart);
let dashboardCalls = 0, loginHidden = true;
const themeDocument = {documentElement:{dataset:{}}, body:{dataset:{}}};
const setTheme = vm.runInNewContext(html.slice(themeStart,themeEnd) + '; setTheme', {
  document:themeDocument,
  $: id => id === 'app' ? {classList:{contains:() => loginHidden}} : null,
  localStorage:{setItem(){}},
  refreshDash: async () => { dashboardCalls++; }
});
setTheme('dark');
assert.equal(themeDocument.documentElement.dataset.t, 'dark');
assert.equal(themeDocument.body.dataset.t, 'dark');
assert.equal(dashboardCalls, 0, 'first paint must not call the uninitialized dashboard');
loginHidden = false;
setTheme('light');
assert.equal(dashboardCalls, 1, 'authenticated theme changes must repaint the dashboard');
console.log('PASS first-paint theme initializes without requesting an uninitialized dashboard');

(async () => {
  const perfStart = html.indexOf('async function loadPerf() {');
  const perfEnd = html.indexOf('function updateTuningUI(', perfStart);
  const controls = {};
  let perfReply = {global_available:false, sync_details:'Managed peers', tcp_mux_live:''};
  const load = vm.runInNewContext('let perfState;\n' + html.slice(perfStart, perfEnd) + '; loadPerf', {
    $: id => controls[id] ||= {style:{}},
    api: async () => perfReply,
    updateTuningUI(){},
    toastErr(title, message){ throw new Error(title + ': ' + message); }
  });
  await load();
  for (const id of ['perfApplyBtn','perfApplyTopBtn','perfSaveOnlyBtn','perfResetBtn','perfSaveTuningBtn','perfSaveChaffBtn','perfDpiToggleBtn','perfMuxCheck']) {
    assert.equal(controls[id].disabled, true, id);
  }
  assert.match(controls.perfMuxLive.textContent, /unavailable/);
  perfReply = {global_available:true, tcp_mux:true, tcp_mux_set:true, tcp_mux_live:'on'};
  await load();
  assert.equal(controls.perfApplyBtn.disabled, false);
  assert.equal(controls.perfMuxCheck.disabled, false);
  assert.match(controls.perfMuxLive.textContent, /Live toml: on/);
  console.log('PASS performance UI refuses global managed mutations and displays legacy live mux');
})().catch(error => { console.error(error); process.exitCode = 1; });
