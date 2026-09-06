# Athena on Linux systemd

Initialize/configure a fresh store and absolute pi/agent paths first:

```sh
daylog --data-dir "$HOME/daylog-v2" setup --schedule
# Inspect ~/.config/systemd/user/daylog-athena.{service,timer}
daylog --data-dir "$HOME/daylog-v2" setup --schedule --activate
systemctl --user status daylog-athena.timer
journalctl --user -u daylog-athena.service
```

The generated oneshot uses a quoted absolute executable and `--data-dir`, UMask 0077, and `tick` (bounded reconciliation then curation). Worker PATH/auth directory are persisted machine config; no daemon or interactive-shell exports are required. Scheduler strings escape systemd percent/dollar expansion and path quoting.

Pause/remove:

```sh
systemctl --user disable --now daylog-athena.timer
systemctl --user stop daylog-athena.service
daylog --data-dir "$HOME/daylog-v2" setup --uninstall-resources
systemctl --user daemon-reload
```

Or choose shadow to pause publication while continuing intake. Saved shadow results never automatically flush into live mode.

`daylog-poll-gh.service/.timer` remain separate snapshot-only polling templates. Set their absolute binary/data paths and an authenticated gh/PATH explicitly before enabling them; they are not installed implicitly by Athena setup.

Linux amd64 is cross-compiled; scheduler/locking/fsync behavior needs **Linux runtime testing**. macOS unit/race/process tests do not establish Linux power-loss guarantees.
