import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const code = fs.readFileSync(new URL('./daylog.1m.js', import.meta.url), 'utf8');
const plugin = vm.createContext({});
vm.runInContext(code, plugin);

test('v2 renderer uses captured display/filed clocks, never old timestamps or live entry decoration', () => {
  const entry = {id:'entry', type:'work', source:'agent:pi', tldr:'Fixed a race',
    display_at:'2026-08-23T23:30:00-07:00', recorded_at:'2026-08-24T09:00:00Z',
    refs:['gh:pr:github.example/owner/repo#42'],
    ts:'2000-01-01T01:00:00Z', pr:{checks:'failing',url:'https://wrong.example'}};
  assert.equal(plugin.logClockOf(entry), '23:30');
  assert.equal(plugin.entryURL(entry), 'https://github.example/owner/repo/pull/42');
  assert.ok(!plugin.entryText(entry,true).includes('failing'));
  assert.equal(plugin.filedOf({...entry,type:'todo',done:true,filed_at:'2026-08-22T07:00:00-07:00'}), '2026-08-22 07:00');
  const day = {version:2,date:'2026-08-23',entries:[entry],open_todos:[],needs_triage:[],prs:[]};
  const output = plugin.render(day,{bin:'/opt/daylog',dataDir:'/fresh/store',nowMs:Date.now(),self:'/plugin',statePath:'/state'}).join('\n');
  assert.match(output,/23:30.*Fixed a race/);
  assert.match(output,/https:\/\/github.example\/owner\/repo\/pull\/42/);
  assert.ok(!output.includes('wrong.example'));
});

test('actions carry the selected store and explicit human identity; proposals need adoption first', () => {
  const ctx = {bin:'/opt/daylog', dataDir:'/fresh/store with spaces'};
  const proposal = {id:'todo',type:'todo',tldr:'Review result',source:'agent:pi',display_at:'2026-08-23T10:00:00Z'};
  const lines = [];
  plugin.todoRow(proposal, ctx, lines, true);
  assert.ok(lines.some(l=>l.includes('Accept')));
  assert.ok(!lines.some(l=>l.includes('Mark done')));
  assert.ok(lines.some(l=>l.includes('human:widget')));
  assert.ok(lines.some(l=>l.includes('param1=--data-dir param2="/fresh/store with spaces"')));
});
