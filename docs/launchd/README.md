# Athena on macOS launchd

First initialize/configure a **fresh** store, absolute pi path, and approved projects/scopes. These commands do not run a model unless you explicitly activate a job with eligible approved inputs.

```sh
daylog --data-dir "$HOME/daylog-v2" setup --schedule
plutil -lint "$HOME/Library/LaunchAgents/dev.daylog.athena.plist"
# Inspect ProgramArguments and paths, then explicitly activate:
daylog --data-dir "$HOME/daylog-v2" setup --schedule --activate
```

The generated user LaunchAgent wakes every 60 seconds and runs the absolute binary with `--data-dir ... tick`. Reconciliation and curation are one-shot; OS locking serializes worker instances. Each tick also checks whether a GitHub PR poll is due (every 300 seconds by default), concurrently with Athena. This works with the macOS panel closed. Set `github_poll_seconds` in the store config to 0 to disable automatic PR polling or 60–86400 to change the interval. Failed attempts are throttled and leave the old snapshot intact; manual refresh still works immediately. Machine config contains pi/PATH, so no interactive shell profile is needed. Output/error logs are private under the store; rotate them manually if needed.

Inspect/pause:

```sh
launchctl print "gui/$(id -u)/dev.daylog.athena"
launchctl bootout "gui/$(id -u)/dev.daylog.athena"
# Preview decisions without processing reports (still uses the model):
daylog --data-dir "$HOME/daylog-v2" curate --once --dry-run
```

After stopping the job, `daylog --data-dir ... setup --uninstall-resources` removes unchanged owned files and exact hook registrations. It retains store/config/history and refuses to delete human-modified files.

This scheduler does not implicitly poll GitHub. Schedule a separate absolute `daylog --data-dir ... poll gh` command if desired, with an authenticated `gh` available to that job.

The plist is tested in scratch HOME and linted on macOS. On 2026-09-09, the real LaunchAgent was activated and observed successfully retrying and publishing a report through the configured Pi/Luna model using its scheduled environment. This validates this Mac's setup, not other machines or native-hook capture coverage.
