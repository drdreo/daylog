# Athena on macOS launchd

First initialize/configure a **fresh** store, absolute pi path, and approved projects/scopes. These commands do not run a model unless you explicitly activate a job with eligible approved inputs.

```sh
daylog --data-dir "$HOME/daylog-v2" setup --schedule
plutil -lint "$HOME/Library/LaunchAgents/dev.daylog.athena.plist"
# Inspect ProgramArguments and paths, then explicitly activate:
daylog --data-dir "$HOME/daylog-v2" setup --schedule --activate
```

The generated user LaunchAgent wakes every 60 seconds and runs the absolute binary with `--data-dir ... tick`. Reconciliation and curation are one-shot; OS locking serializes worker instances. Machine config contains pi/PATH, so no interactive shell profile is needed. Output/error logs are private under the store; rotate them manually if needed.

Inspect/pause:

```sh
launchctl print "gui/$(id -u)/dev.daylog.athena"
launchctl bootout "gui/$(id -u)/dev.daylog.athena"
# Or pause publication without disabling intake:
daylog --data-dir "$HOME/daylog-v2" setup --mode shadow
```

After stopping the job, `daylog --data-dir ... setup --uninstall-resources` removes unchanged owned files and exact hook registrations. It retains store/config/history and refuses to delete human-modified files.

This scheduler does not implicitly poll GitHub. Schedule a separate absolute `daylog --data-dir ... poll gh` command if desired, with an authenticated `gh` available to that job.

The plist is generated/tested in scratch HOME and linted on macOS. **No real LaunchAgent was installed or started during implementation.** Live scheduling and provider authentication still require deliberate operational verification.
