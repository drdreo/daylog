import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, copyFileSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

test('settled adapter sends neutral bounded intake, never starts another turn', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'daylog-adapter-'));
  const oldSource = process.env.DAYLOG_SOURCE;
  const oldInternal = process.env.DAYLOG_INTERNAL;
  delete process.env.DAYLOG_INTERNAL;
  try {
    copyFileSync(new URL('./daylog.ts', import.meta.url), join(dir, 'index.ts'));
    const fake = join(dir, 'fake-daylog');
    writeFileSync(fake, `#!${process.execPath}\nlet b='';process.stdin.on('data',c=>b+=c);process.stdin.on('end',()=>{require('fs').writeFileSync(${JSON.stringify(join(dir, 'input.json'))},b);process.stdout.write('{}\\n')});`, { mode: 0o700 });
    writeFileSync(join(dir, 'daylog-settings.json'), JSON.stringify({ binary: fake, data_dir: join(dir, 'data') }));
    const { default: extension } = await import(pathToFileURL(join(dir, 'index.ts')));
    const handlers = new Map();
    extension({ on: (name, fn) => handlers.set(name, fn) });
    const branch = [
      { type: 'message', id: 'u', message: { role: 'user' } },
      { type: 'message', id: 'a', timestamp: '2026-09-06T10:00:03Z', message: { role: 'assistant', stopReason: 'stop', content: [{ type: 'thinking', thinking: 'private' }, { type: 'text', text: 'Implemented locking.' }] } },
    ];
    const ctx = { cwd: dir, hasUI: false, sessionManager: { getBranch: () => branch, getSessionId: () => 's' } };
    await handlers.get('session_start')({}, ctx);
    handlers.get('message_end')({ message: { role: 'user' } }, ctx);
    await handlers.get('agent_settled')({}, ctx);
    const input = JSON.parse(readFileSync(join(dir, 'input.json'), 'utf8'));
    assert.equal(input.last_assistant_message, 'Implemented locking.');
    assert.equal(input.native_id, 'a');
    assert.equal(input.turn_id, 'u');
    assert.equal(process.env.DAYLOG_TURN_ID, 'u');
    assert.equal(handlers.has('agent_end'), false);
    assert.equal(handlers.has('turn_end'), false);
    process.env.DAYLOG_INTERNAL = '1';
    extension({ on: () => assert.fail('internal editor must not register capture') });
  } finally {
    if (oldSource === undefined) delete process.env.DAYLOG_SOURCE; else process.env.DAYLOG_SOURCE = oldSource;
    if (oldInternal === undefined) delete process.env.DAYLOG_INTERNAL; else process.env.DAYLOG_INTERNAL = oldInternal;
    delete process.env.DAYLOG_TURN_ID;
    rmSync(dir, { recursive: true, force: true });
  }
});
