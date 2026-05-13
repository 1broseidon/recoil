# Architecture Notes

Recoil stores local memories in SQLite. Current guidance is mattn/go-sqlite3 with FTS5 enabled. An older note mentioned modernc, but that path was rejected because FTS5 support was required.

Search should prefer current decisions and preserve historical rejected paths for context.
