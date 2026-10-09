#!/usr/bin/env python3
"""Execute real menu functions with a recording manager; no host mutations."""
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import tempfile

source = (Path(__file__).resolve().parents[1] / 'hashem.sh').read_text(encoding='utf-8')
names = ['managed_json_request', 'managed_registry_present', 'menu_add_peer',
         'peer_list_pretty', 'menu_edit_peer', 'menu_remove_peer', 'menu_fast_foreign',
         'setup_foreign_server', 'menu_show_peer_bundle', 'menu_restart_peers',
         'menu_tunnel', 'mask_sensitive', 'cli_dial']
definitions = []
for name in names:
    found = re.findall(r'^' + name + r'\(\) \{\n.*?^\}', source, re.M | re.S)
    assert found, name
    definitions.append(found[-1])

with tempfile.TemporaryDirectory(prefix='hashem-menu-') as directory:
    root = Path(directory)
    helper = root / 'record.py'
    helper.write_text('''import json,os,sys
args=sys.argv[1:]; body=None
if "--request-file" in args: body=json.load(sys.stdin)
with open(os.environ["CALLS"],"a") as f:
 f.write(json.dumps({"args":args,"body":body,"bundle_in_env":any("hsh2_synthetic" in v for v in os.environ.values())})+"\\n")
if args[0]==os.environ.get("FAIL_COMMAND"): sys.exit(7)
if args[0]=="peer-list": print(os.environ["PEERS"])
elif args[0]=="peer-token": print("hsh2_synthetic")
else: print(json.dumps({"status":"pending"}))
''', encoding='utf-8')
    peers = {'peers': [{'id': 1, 'name': 'NL', 'managed': True, 'raw_ports': ['8888', '8889']}]}
    environment = dict(os.environ, CALLS=str(root / 'calls'), PEERS=json.dumps(peers),
                       PANEL_CONFIG_DIR=str(root / 'config'))
    mock = '\n'.join([
        'managed_cli() { python3 ' + shlex.quote(str(helper)) + ' "$@"; }',
        'cli_remove_peer() { managed_cli remove-peer "$@"; }',
        'cli_setup_foreign() { echo "LEGACY_FALLBACK"; }',
        'clear() { :; }; show_banner() { :; }; pause_prompt() { :; }',
        'GREEN= RED= CYAN= YELLOW= NC=',
    ])

    def run(function, lines, fail_command='', expected=0):
        (root / 'calls').write_text('', encoding='utf-8')
        result = subprocess.run(['bash', '-c', '\n'.join(definitions) + '\n' + mock + '\n' + function],
                                input='\n'.join(lines) + '\n', text=True, capture_output=True,
                                env=dict(environment, FAIL_COMMAND=fail_command), timeout=15)
        assert result.returncode == expected, (function, result.returncode, result.stderr)
        calls = [json.loads(line) for line in (root / 'calls').read_text(encoding='utf-8').splitlines()]
        return calls, result.stdout

    calls, _ = run('menu_add_peer', ['TR "quoted"', '192.0.2.10', '192.0.2.30', '', '', '', '8880,2052', ''])
    body = calls[-1]['body']
    assert body['id'] == 2 and body['frp_transport'] == 'kcp'
    assert body['local_gre'] == '10.70.2.1' and body['peer_gre'] == '10.70.2.2'
    assert body['frp_port'] == 17002 and body['name'] == 'TR "quoted"'
    assert calls[-1]['args'] == ['add-peer', '--request-file', '-']
    calls, _ = run('menu_add_peer', ['TR', '192.0.2.10', '192.0.2.30', '', '', '', 'udp:2052', 'tcp'])
    assert calls[-1]['body']['frp_transport'] == 'tcp'
    _, out = run('menu_add_peer', ['TR', '192.0.2.10', '192.0.2.30', '', '', '', '2052', ''], 'add-peer', 1)
    assert 'Local activation is PENDING' not in out
    calls, _ = run('menu_tunnel', ['2', '1', 'hsh2_synthetic', '192.0.2.30', '0'])
    assert calls[-1]['body'] == {'bundle': 'hsh2_synthetic', 'local_public': '192.0.2.30'}
    assert calls[-1]['args'] == ['setup-foreign', '--request-file', '-']
    assert not calls[-1]['bundle_in_env']
    run('menu_fast_foreign', ['hsh1_untrusted'], expected=1)
    calls, _ = run('setup_foreign_server', ['hsh2_synthetic', '192.0.2.20'])
    assert calls[-1]['body']['local_public'] == '192.0.2.20'
    calls, _ = run('menu_tunnel', ['4', '1', '0'])
    assert calls[-1]['args'] == ['peer-token', '--id', '1']
    calls, _ = run('menu_edit_peer', ['1', '', '', 'tcp:8080=80,udp:2052', 'kcp'])
    assert calls[-1]['body'] == {'id': 1, 'raw_ports': ['tcp:8080=80,udp:2052'], 'frp_transport': 'kcp'}
    calls, _ = run('menu_remove_peer', ['1'])
    assert calls[-1]['args'] == ['remove-peer', '--id', '1']
    calls, _ = run('menu_restart_peers', [])
    assert calls[-1]['args'] == ['restart-peer', '--id', '1']
    _, out = run("mask_sensitive 'bundle=hsh2_synthetic'", [])
    assert 'hsh2_synthetic' not in out and 'MASKED_PAIRING_BUNDLE' in out
    (root / 'config').mkdir()
    (root / 'config/peers.json').write_text(json.dumps(peers), encoding='utf-8')
    _, out = run('cli_dial status', [])
    assert out.strip() == 'available=0'
    run('cli_dial public', [], expected=1)
print('PASS managed menu add/default KCP/TCP, Fast Setup, bundle, edit/remove/restart, failure and secret handling')
