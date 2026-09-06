// daylog adapter v1: pi 0.85.1. No tools, prompts, or continuation are injected.
import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { spawn } from 'node:child_process';
import { readFileSync } from 'node:fs';

export function invoke(binary: string, dataDir: string, args: string[], payload?: unknown): Promise<string> {
  return new Promise((resolve, reject) => {
    const child = spawn(binary, args, { env: { ...process.env, DAYLOG_DIR: dataDir }, stdio: ['pipe', 'pipe', 'pipe'] });
    let output = ''; let error = ''; let tooLarge = false;
    const timer = setTimeout(() => child.kill('SIGKILL'), 5000);
    child.stdout.on('data', chunk => { output += chunk; if (output.length > 65536) { tooLarge = true; child.kill('SIGKILL'); } });
    child.stderr.on('data', chunk => { error = (error + chunk).slice(0, 2048); });
    child.on('error', err => { clearTimeout(timer); reject(err); });
    child.on('close', code => { clearTimeout(timer); code === 0 && !tooLarge ? resolve(output) : reject(new Error(error || 'daylog capture failed')); });
    child.stdin.on('error', () => {});
    child.stdin.end(payload === undefined ? '' : JSON.stringify(payload));
  });
}

export default function (pi: ExtensionAPI) {
  if (process.env.DAYLOG_INTERNAL === '1') return;
  // Installed beside this extension; paths are persisted, not shell-dependent.
  const settings = JSON.parse(readFileSync(new URL('./daylog-settings.json', import.meta.url), 'utf8'));
  const call = (args: string[], input?: unknown) => invoke(settings.binary, settings.data_dir, args, input);
  process.env.DAYLOG_SOURCE = 'agent:pi';
  pi.on('message_end', (event, ctx) => {
    if (event.message.role !== 'user') return;
    const branch = ctx.sessionManager.getBranch();
    const user = [...branch].reverse().find(e => e.type === 'message' && e.message.role === 'user');
    if (user) process.env.DAYLOG_TURN_ID = user.id;
  });
  pi.on('session_start', async (_event, ctx) => {
    delete process.env.DAYLOG_TURN_ID;
    try {
      await call(['capture', '--adapter', 'pi', '--harness-version', '0.85.1'], {
        adapter_version: 1, harness_version: '0.85.1', hook_event_name: 'SessionStart',
        session_id: ctx.sessionManager.getSessionId(), cwd: ctx.cwd,
      });
    } catch (err) { if (ctx.hasUI) ctx.ui.notify(String(err), 'warning'); }
  });
  pi.on('agent_settled', async (_event, ctx) => {
    const branch = ctx.sessionManager.getBranch();
    const entry = [...branch].reverse().find(e => e.type === 'message' && e.message.role === 'assistant');
    if (!entry || entry.type !== 'message' || entry.message.role !== 'assistant') return;
    const user = [...branch].reverse().find(e => e.type === 'message' && e.message.role === 'user');
    const message = entry.message;
    const text = message.content.filter(p => p.type === 'text').map(p => p.text).join('\n');
    try {
      await call(['capture', '--adapter', 'pi', '--harness-version', '0.85.1'], {
        adapter_version: 1, harness_version: '0.85.1', hook_event_name: 'agent_settled',
        session_id: ctx.sessionManager.getSessionId(), turn_id: user?.id || '',
        native_id: entry.id, timestamp: entry.timestamp, cwd: ctx.cwd,
        last_assistant_message: text.slice(0, 12000),
        terminal: message.stopReason === 'stop' ? 'completed' : 'interrupted',
        parent_session: process.env.DAYLOG_PARENT_SESSION_ID || '', task_id: process.env.DAYLOG_TASK_ID || '',
      });
    } catch (err) { if (ctx.hasUI) ctx.ui.notify(String(err), 'warning'); }
  });
}
