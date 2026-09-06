import assert from 'node:assert/strict';
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
});
