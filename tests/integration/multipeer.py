#!/usr/bin/env python3
"""Real Linux GRE + official FRP 0.71.0 traffic, with a test service supervisor.
Run only inside the dedicated privileged QA container; never on a VPS/host.
"""
import concurrent.futures
import hashlib
import http.client
import json
import os
import pathlib
import secrets
import socket
import ssl
import subprocess
import time

if not pathlib.Path('/.dockerenv').exists() or os.geteuid() != 0:
    raise SystemExit('Disposable privileged Docker container required')

ROOT = pathlib.Path('/work')
BIN = ROOT / 'build/qa/gre-panel'
RUN = ROOT / 'build/qa' / ('integration-' + secrets.token_hex(4))
RUN.mkdir()
NODES = {'ir': '192.0.2.10', 'nl': '192.0.2.20', 'tr': '192.0.2.30'}
children, namespaces, checks = [], [], []
prefix = 'hq' + secrets.token_hex(2)

def command(args, **kw):
    try:
        return subprocess.run([str(x) for x in args], check=True, text=True,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=45, **kw).stdout
    except subprocess.CalledProcessError as exc:
        raise RuntimeError(f'{args[0]} exited {exc.returncode}: {exc.stderr[:2000]}') from None

def node_env(node):
    d = RUN / node
    return ['env', 'GRE_PANEL_DIR=' + str(d), 'HASHEM_UNIT_DIR=' + str(d / 'units'),
            'TERM=dumb', 'PATH=' + str(ROOT / 'tests/integration') + ':' + os.environ['PATH']]

def node_command(node, args, **kw):
    return command(['ip', 'netns', 'exec', prefix + node, *node_env(node), *args], **kw)

def cli(node, operation, body=None, flags=()):
    args = ['bash', ROOT / 'hashem.sh', operation, *flags]
    if body is not None: args += ['--request-file', '-']
    text = node_command(node, args, input=json.dumps(body) if body is not None else None)
    return json.loads(text) if text.strip().startswith('{') else text

def launch(node, args):
    log = open(RUN / (node + '-' + pathlib.Path(str(args[0])).name + '.log'), 'ab')
    proc = subprocess.Popen(['ip', 'netns', 'exec', prefix + node, *node_env(node), *map(str, args)], stdout=log, stderr=log)
    log.close(); children.append(proc); return proc

def wait_for(fn, timeout=75):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            return fn()
        except Exception as exc:
            last = exc; time.sleep(.3)
    raise AssertionError(str(last))

def echo(port, label, proto='tcp'):
    data = secrets.token_bytes(256)
    family = socket.SOCK_DGRAM if proto == 'udp' else socket.SOCK_STREAM
    with socket.socket(socket.AF_INET, family) as s:
        s.settimeout(4); s.connect((NODES['ir'], port)); s.sendall(data)
        received = s.recv(4096)
    assert received == label.encode() + b':' + data, (port, proto, 'wrong data/peer')

def check(name, fn):
    fn(); checks.append({'check': name, 'result': 'PASS'})
    print('PASS ' + name, flush=True)

def config_hash(node, peer_id):
    return hashlib.sha256((RUN / node / 'managed' / str(peer_id) / 'frp.toml').read_bytes()).hexdigest()

session = {}

def api(method, path, body=None):
    # The test explicitly trusts the pinned generated certificate. This context
    # is test-only; product management clients independently verify the pin.
    context = ssl.create_default_context()
    context.load_verify_locations(RUN / 'ir/tls/server.crt')
    context.check_hostname = False
    conn = http.client.HTTPSConnection(NODES['ir'], 7443, context=context, timeout=20)
    headers = {'Content-Type': 'application/json'}
    if session:
        headers['Cookie'] = session['cookie']; headers['X-CSRF-Token'] = session['csrf']
    conn.request(method, path, json.dumps(body) if body is not None else None, headers)
    r = conn.getresponse(); data = json.loads(r.read())
    cookies = r.getheaders()
    conn.close()
    if r.status >= 400: raise RuntimeError(str(data))
    return data, cookies

try:
    command(['ip', 'link', 'add', prefix + 'br', 'type', 'bridge'])
    command(['ip', 'link', 'set', prefix + 'br', 'up'])
    command(['ip', 'addr', 'add', '192.0.2.1/24', 'dev', prefix + 'br'])
    for node, ip in NODES.items():
        ns = prefix + node; command(['ip', 'netns', 'add', ns]); namespaces.append(ns)
        a, b = prefix + node + 'a', prefix + node + 'b'
        command(['ip', 'link', 'add', a, 'type', 'veth', 'peer', 'name', b])
        command(['ip', 'link', 'set', a, 'master', prefix + 'br'])
        command(['ip', 'link', 'set', a, 'up'])
        command(['ip', 'link', 'set', b, 'netns', ns])
        command(['ip', '-n', ns, 'addr', 'add', ip + '/24', 'dev', b])
        command(['ip', '-n', ns, 'link', 'set', b, 'up'])
        command(['ip', '-n', ns, 'link', 'set', 'lo', 'up'])
        (RUN / node / 'units').mkdir(parents=True)
        node_command(node, [BIN, 'init-panel'], input='Synthetic-test-Password-2026!')
    central_panel = launch('ir', [BIN])
    base = '/' + json.loads((RUN / 'ir/panel.json').read_text())['base_path']
    login, headers = wait_for(lambda: api('POST', base + '/api/login', {'username': 'admin', 'password': 'Synthetic-test-Password-2026!'}))
    session.update(csrf=login['csrf_token'], cookie='; '.join(v.split(';')[0] for k, v in headers if k.lower() == 'set-cookie'))
    for node, ports in [('nl', [8888, 8889]), ('tr', [8880, 2052])]: launch(node, [ROOT / 'tests/integration/echo.py', node.upper(), *ports])
    # Unrelated resources must survive all mutations.
    node_command('ir', ['ip', 'tunnel', 'add', 'unrelated-gre', 'mode', 'gre', 'local', NODES['ir'], 'remote', '192.0.2.99'])
    unrelated_before = node_command('ir', ['ip', '-j', 'tunnel', 'show', 'unrelated-gre'])
    bundles = {}
    for node, peer_id, ports in [('nl', 1, '8888,8889'), ('tr', 2, '8880,2052')]:
        body = {'id': peer_id, 'name': node.upper(), 'local_public': NODES['ir'], 'remote_public': NODES[node], 'local_gre': f'10.70.{peer_id}.1', 'peer_gre': f'10.70.{peer_id}.2', 'frp_port': 17000 + peer_id, 'frp_transport': 'kcp', 'ports': ports}
        if node == 'nl':
            body['role'] = 'iran'
            created, _ = api('POST',base+'/api/setup',body)
            bundles[node] = created['bundle']
        else:
            node_command('ir', ['bash', ROOT/'hashem.sh', 'menu'], input='\n'.join(['1','3','TR',NODES['ir'],NODES['tr'],'','','',ports,'','','0','0'])+'\n')
            bundles[node] = cli('ir', 'peer-token', flags=('--id', '2')).strip()
        if node == 'tr':
            node_command(node, ['bash', ROOT/'hashem.sh', 'menu'], input='\n'.join(['1','2','1',bundles[node],NODES[node],'','0','0'])+'\n')
        else:
            cli(node, 'setup-foreign', {'bundle': bundles[node], 'local_public': NODES[node]})
        launch(node, [BIN])
    for port, label in [(8888, 'NL'), (8889, 'NL'), (8880, 'TR'), (2052, 'TR')]:
        check('TCP ' + str(port), lambda p=port, l=label: wait_for(lambda: echo(p, l)))
        check('UDP round trip ' + str(port), lambda p=port, l=label: wait_for(lambda: echo(p, l, 'udp')))
    def kcp():
        for node, peer_id in [('nl', 1), ('tr', 2)]:
            text = (RUN / node / 'managed' / str(peer_id) / 'frp.toml').read_text()
            assert 'transport.protocol = "kcp"' in text
            assert f'serverAddr = "10.70.{peer_id}.1"' in text
            udp = node_command('ir', ['ss', '-uln'])
            assert f'10.70.{peer_id}.1:{17000+peer_id}' in udp
            assert f'0.0.0.0:{17000+peer_id}' not in udp
            cli(node, 'reconcile', flags=('--id', str(peer_id)))
    check('actual KCP listeners on two independent GRE links', kcp)
    check('interactive Add Peer defaults to KCP and Fast Setup pairs hsh2 without config edits',lambda: (echo(2052,'TR'), 'transport.protocol = \"kcp\"' in (RUN/'tr/managed/2/frp.toml').read_text() or (_ for _ in ()).throw(AssertionError('menu changed transport'))))
    first_hash = config_hash('ir',1)
    original = json.loads((RUN/'ir/peers.json').read_text())['peers'][0]
    same = {'name':'NL','local_public':NODES['ir'],'remote_public':NODES['nl'],'local_gre':'10.70.1.1','peer_gre':'10.70.1.2','frp_port':17001,'frp_transport':'kcp','ports':'8888,8889'}
    repeated = cli('ir','setup-iran',same)
    check('repeat setup is idempotent',lambda: (config_hash('ir',1)==first_hash and repeated['peer']['revision']==original['revision'] or (_ for _ in ()).throw(AssertionError('repeat mutated peer'))))
    central_panel.terminate(); central_panel.wait(timeout=10)
    central_panel = launch('ir',[BIN])
    check('application restart preserves both peer traffic',lambda:(echo(8888,'NL'),echo(2052,'TR')))
    login, headers = wait_for(lambda: api('POST',base+'/api/login',{'username':'admin','password':'Synthetic-test-Password-2026!'}))
    session.update(csrf=login['csrf_token'],cookie='; '.join(v.split(';')[0] for k,v in headers if k.lower()=='set-cookie'))
    def no_secrets():
        data, _ = api('GET', base + '/api/peers')
        for p in data['peers']:
            assert not p.get('token') and not p.get('management_secret')
            assert p['forwarding_health'] == 'UNKNOWN'
    check('status API redacts credentials and preserves unknown forwarding', no_secrets)
    for node, peer_id, remaining_port, label in [('nl', 1, 2052, 'TR'), ('tr', 2, 8888, 'NL')]:
        if node == 'nl': api('POST',base+'/api/action',{'action':'disable-peer','peer_id':peer_id})
        else: cli('ir', 'disable-peer', flags=('--id', str(peer_id)))
        check('disable ' + node + ' preserves other peer', lambda p=remaining_port, l=label: echo(p, l))
        if node == 'nl': api('POST',base+'/api/action',{'action':'enable-peer','peer_id':peer_id})
        else: cli('ir', 'enable-peer', flags=('--id', str(peer_id)))
        wait_for(lambda: echo(8888 if node == 'nl' else 2052, node.upper()))
    turkey_hash = config_hash('tr', 2)
    api('PATCH', base + '/api/peers', {'id': 1, 'engine':'frp', 'transport':'tcpmux', 'frp_transport':'kcp', 'raw_ports': ['tcp:8888=8888', 'udp:8888=8888', 'tcp:9999=8889']})
    cli('nl', 'reconcile', flags=('--id', '1'))
    check('WebUI port edit deploys FRPC mappings and preserves Turkey', lambda: (wait_for(lambda: echo(9999, 'NL')), echo(2052, 'TR'), config_hash('tr', 2) == turkey_hash or (_ for _ in ()).throw(AssertionError('Turkey changed'))))
    netherlands_hash = config_hash('nl',1)
    cli('ir','edit-peer-ports',{'id':2,'raw_ports':['8880','2052','tcp:9998=2052']})
    cli('tr','reconcile',flags=('--id','2'))
    check('CLI Turkey edit deploys mappings and preserves Netherlands',lambda:(wait_for(lambda:echo(9998,'TR')),echo(9999,'NL'),config_hash('nl',1)==netherlands_hash or (_ for _ in ()).throw(AssertionError('Netherlands changed'))))
    archive = RUN/'central.enc'
    node_command('ir',[BIN,'peer-manage','backup','--file',archive])
    node_command('ir',[BIN,'peer-manage','restore-backup','--file',archive,'--dry-run'])
    check('encrypted managed backup validates without plaintext credentials',lambda:(b'synthetic-peer' not in archive.read_bytes() and b'PRIVATE KEY' not in archive.read_bytes() or (_ for _ in ()).throw(AssertionError('plaintext backup'))))
    api('PATCH', base + '/api/peers', {'id': 1, 'frp_transport': 'tcp'})
    cli('nl', 'reconcile', flags=('--id', '1'))
    check('transport edit changes operational FRPC configuration', lambda: ('transport.protocol = "tcp"' in (RUN / 'nl/managed/1/frp.toml').read_text() or (_ for _ in ()).throw(AssertionError('transport unchanged'))))
    wait_for(lambda: echo(8888, 'NL'))
    before = config_hash('ir', 1)
    try: api('PATCH', base + '/api/peers', {'id': 1, 'raw_ports': ['tcp:2052']})
    except RuntimeError: pass
    else: raise AssertionError('duplicate public port accepted')
    check('invalid update preserves working state', lambda: (echo(8888, 'NL'), echo(2052, 'TR'), config_hash('ir', 1) == before or (_ for _ in ()).throw(AssertionError('invalid update mutated config'))))
    def stress():
        with concurrent.futures.ThreadPoolExecutor(max_workers=32) as pool: list(pool.map(lambda _: echo(2052, 'TR'), range(128)))
    check('128 actual KCP TCP sessions with payload verification', stress)
    cli('ir', 'remove-peer', flags=('--id', '1'))
    check('delete first peer preserves second traffic', lambda: echo(2052, 'TR'))
    check('owned first peer files removed', lambda: (not (RUN / 'ir/managed/1').exists() or (_ for _ in ()).throw(AssertionError('orphan'))))
    def owned_cleanup():
        assert 'hsh-gre-1' not in node_command('ir',['ip','-j','link','show'])
        assert 'hashem:peer:1' not in node_command('ir',['iptables-save'])
        assert not (RUN/'ir/units/hsh-frps-1.service').exists()
    check('first peer interface units and firewall rules removed',owned_cleanup)
    check('unrelated GRE unchanged', lambda: (node_command('ir', ['ip', '-j', 'tunnel', 'show', 'unrelated-gre']) == unrelated_before or (_ for _ in ()).throw(AssertionError('unrelated GRE changed'))))
    # Recreate from the same automated CLI workflow, without file editing.
    body = {'id': 1, 'name': 'NL', 'local_public': NODES['ir'], 'remote_public': NODES['nl'], 'local_gre': '10.70.1.1', 'peer_gre': '10.70.1.2', 'frp_port': 17001, 'frp_transport': 'kcp', 'ports': '8888,8889'}
    recreated = cli('ir', 'add-peer', body)
    cli('nl', 'remove-peer', flags=('--id', '1'))
    cli('nl', 'setup-foreign', {'bundle': recreated['bundle'], 'local_public': NODES['nl']})
    wait_for(lambda: echo(8888, 'NL'))
    cli('ir', 'remove-peer', flags=('--id', '2'))
    check('delete second peer preserves recreated first traffic', lambda: echo(8888, 'NL'))
    report = {'environment': 'privileged disposable Linux container; real kernel GRE; FRP 0.71.0; systemctl test supervisor', 'checks': checks, 'real_vps': False, 'systemd_reboot': 'NOT_EXECUTED'}
    (RUN / 'report.json').write_text(json.dumps(report, indent=2))
    (ROOT / 'build/qa/integration-report.json').write_text(json.dumps(report, indent=2))
finally:
    for proc in children:
        proc.terminate()
    for ns in namespaces:
        pids = subprocess.run(['ip', 'netns', 'pids', ns], capture_output=True, text=True).stdout.split()
        for pid in pids:
            try: os.kill(int(pid), 15)
            except ProcessLookupError: pass
        subprocess.run(['ip', 'netns', 'del', ns], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    subprocess.run(['ip', 'link', 'del', prefix + 'br'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
