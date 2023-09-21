# User Guide for `reproxystatus`

`reproxystatus` is a binary that can be used to monitor build stats for running
instances of `reproxy`.
Source code can be found in this directory.

## Usage

Invoking `reproxystatus` with no arguments will print build stats for all
running instances of `reproxy` with `--server_address=unix://...` or
`--server_address=pipe://...`.

If `reproxy` is running on a tcp ip and port, or if you only want to show stats
for one instance then you must pass the same `--server_address` to
`reproxystatus` that was passed to `reproxy`.

To see live output throughout a build, wrap the call to `reproxystatus` with a
tool like `watch`, eg:

```
$ watch /path/to/reproxystatus
```

## Sample output

```
$ /path/to/reproxystatus
Reproxy(unix:///path/to/unix.sock) is OK
Actions completed: 26935 (26756 cache hit, 179 racing local)
Actions in progress: 20
```
