#!/usr/bin/env python3
"""Real legacy mux rendering and managed refusal; no service mutations."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

source = (Path(__file__).resolve().parents[1] / 'hashem.sh').read_text(encoding='utf-8')
names = ['managed_registry_present', 'perf_get_tcpmux', 'perf_tcpmux_new',
         'perf_tcpmux_lines', 'perf_apply', 'cli_perf']
definitions = []
for name in names:
    found = re.findall(r'^' + name + r'\(\) \{\n.*?^\}', source, re.M | re.S)
    assert found, name
    definitions.append(found[-1])
with tempfile.TemporaryDirectory(prefix='hashem-perf-') as directory:
    root = Path(directory)
    perf = root / 'perf.json'
    env = dict(os.environ, PERF_FILE=str(perf), PANEL_CONFIG_DIR=str(root), PERF_TCPMUX='')

    def run(command, expected=0):
        result = subprocess.run(['bash', '-c', '\n'.join(definitions) + '\n' + command],
                                env=env, text=True, capture_output=True, timeout=10)
        assert result.returncode == expected, (result.returncode, result.stderr)
        return result.stdout

    perf.write_text('{}', encoding='utf-8')
    assert run('perf_get_tcpmux').strip() == ''
    assert run('perf_tcpmux_lines').strip() == 'transport.tcpMux = false'
    perf.write_text('{"tcp_mux":true}', encoding='utf-8')
    assert run('perf_tcpmux_lines').strip() == 'transport.tcpMux = true\ntransport.tcpMuxKeepaliveInterval = 30'
    perf.write_text('{"tcp_mux":false}', encoding='utf-8')
    assert run('perf_get_tcpmux').strip() == '0'
    registry = root / 'peers.json'
    registry.write_text(json.dumps({'peers': [{'id': 1, 'managed': True}]}), encoding='utf-8')
    before = perf.read_bytes(), registry.read_bytes()
    for command in ['perf_apply', 'cli_perf tcpmux on', 'cli_perf apply', 'cli_perf reset']:
        run(command, expected=1)
        assert before == (perf.read_bytes(), registry.read_bytes())
    assert 'unavailable' in run('cli_perf status')
    registry.write_text('{"peers":', encoding='utf-8')
    run('perf_apply', expected=1)
print('PASS legacy TCP mux unset/on/off and managed/corrupt registry refuses mutation')
