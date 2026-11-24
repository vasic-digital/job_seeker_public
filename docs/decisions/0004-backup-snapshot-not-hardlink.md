# 0004 Pre-migration backup is a VACUUM INTO snapshot, not a hardlink

| | |
|---|---|
| Status | ACCEPTED (deviation from the task text of JSK-T016 and FR-057 wording "hardlinked") |
| Date | 2026-11-24 |

A hardlink shares the inode with the live database. SQLite rewrites pages in place on checkpoint, so the
"backup" changes with the database (`TestHardlinkIsNotASnapshot` measures this). The pre-migration backup is
therefore a `VACUUM INTO` snapshot: its own inode, mode 0600, `PRAGMA integrity_check` must return `ok`, and the
migration is refused (`ErrBackupFailed`) when the backup cannot be made or verified, or no backup directory is
configured for an existing database. Hardlinks remain appropriate for the section 9 backups of FILES that are
deleted rather than modified in place (not applicable to the SQLite file).
