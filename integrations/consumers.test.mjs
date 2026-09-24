import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const read = p => fs.readFileSync(new URL(p, import.meta.url),'utf8');
test('Omarchy timestamp helpers consume only the v2 captured clocks', () => {
  const source = read('../omarchy-plugin/Panel.qml');
  const ctx = vm.createContext({});
  for (const name of ['logClockOf','filedOf','entryURL']) {
    const fn = source.match(new RegExp('  function '+name+'\\([^]*?\\n  }'));
    assert.ok(fn, name);
    vm.runInContext(fn[0], ctx);
  }
  const entry = {type:'todo',done:true,display_at:'2026-09-06T23:40:00-07:00',filed_at:'2026-09-05T08:30:00+02:00',refs:['gh:pr:github.example/team/repo#3']};
  assert.equal(ctx.logClockOf(entry),'23:40');
  assert.equal(ctx.filedOf(entry),'2026-09-05 08:30');
  assert.equal(ctx.entryURL(entry),'https://github.example/team/repo/pull/3');
  for (const [ref, url] of [
    ['gh:issue:github.com/drdreo/Athena#22', 'https://github.com/drdreo/Athena/issues/22'],
    ['gh:issue:github.example/team/repo#3', 'https://github.example/team/repo/issues/3'],
    ['gh:issue:github.com@evil.example/team/repo#3', ''],
    ['gh:issue:github.com/team/repo#0', ''],
  ]) {
    assert.equal(ctx.entryURL({...entry, refs: [ref]}), url, ref);
  }
});

test('Windows issue and PR links use separate destinations', t => {
  if (spawnSync('pwsh', ['-NoProfile', '-Command', 'exit 0']).error?.code === 'ENOENT') {
    t.skip('PowerShell is not installed; Windows links are statically checked only');
    return;
  }
  const fn = read('../windows-plugin/daylog-tray.ps1').match(/function Get-EntryUrl\([^]*?\n}/);
  assert.ok(fn);
  const script = `${fn[0]}
    $cases = @(
      @('gh:issue:github.com/drdreo/Athena#22', 'https://github.com/drdreo/Athena/issues/22'),
      @('gh:issue:github.example/team/repo#3', 'https://github.example/team/repo/issues/3'),
      @('gh:pr:github.com/drdreo/Athena#22', 'https://github.com/drdreo/Athena/pull/22'),
      @('gh:issue:github.com@evil.example/team/repo#3', ''),
      @('gh:issue:github.com/team/repo#0', '')
    )
    foreach ($case in $cases) {
      if ((Get-EntryUrl @{ refs = @($case[0]) }) -cne $case[1]) { throw "Wrong destination: $($case[0])" }
    }`;
  const result = spawnSync('pwsh', ['-NoProfile', '-NonInteractive', '-Command', script], {encoding: 'utf8'});
  assert.equal(result.status, 0, result.stderr || String(result.error));
});

test('all widgets remove old timestamp and entry-level PR contracts', () => {
  for (const file of ['../omarchy-plugin/Panel.qml','../swiftbar-plugin/daylog.1m.js','../windows-plugin/daylog-tray.ps1']) {
    const source = read(file);
    assert.ok(source.includes('display_at'), file);
    assert.ok(source.includes('filed_at'), file);
    assert.doesNotMatch(source,/done_ts|original_type|\be\.pr\b|\$E\.pr\b|\bentry\.pr\b|\$Entry\.pr\b|\be\.ts\b|\$E\.ts\b/i);
  }
  // Static only: a PowerShell parser/WinForms runtime is not available on macOS.
  const win = read('../windows-plugin/daylog-tray.ps1');
  assert.match(win,/DAYLOG_DIR/);
  assert.match(win,/done',.*'--source', 'human:widget'/);
  assert.match(win,/Unsupported view version/);
  assert.ok(win.includes('^gh:(pr|issue):'));
  assert.ok(win.includes("$kind = if ($Matches[1] -eq 'issue') { 'issues' } else { 'pull' }"));
  assert.ok(win.includes('https://$($Matches[2])/$($Matches[3])/$kind/$($Matches[4])'));
});
