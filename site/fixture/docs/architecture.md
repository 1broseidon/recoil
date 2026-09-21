# Architecture

The parser reads TLE files and keeps a cache of parsed elements so repeated
runs skip the slow checksum pass. Cache entries are keyed by file content hash.

## Cache

Entries live in a SQLite file in WAL mode. Redis was considered and rejected
because installs must work offline with no daemon.
