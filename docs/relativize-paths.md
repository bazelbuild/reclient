# Relativizing compiler command paths

CMake and other build systems can emit absolute compiler, source, include, and
output paths. A remote worker resolves paths within the action input root,
which may live at a different location from the client checkout.

Enable `-relativize_paths` on **reproxy** to rewrite recognized absolute paths
under an action's `exec_root` relative to its working directory before remote
input processing. The option defaults to false. It applies to remote and racing
execution; it does not change local-only or local-execution/remote-cache actions.

For example, with `exec_root=/checkout` and working directory `build`:

```text
/checkout/tools/clang++ -I/checkout/include -c /checkout/src/a.cc -o /checkout/build/a.o
```

becomes:

```text
../tools/clang++ -I../include -c ../src/a.cc -o a.o
```

Declare source and toolchain inputs through the existing input configuration;
paths to uploaded files must fall under the execution root.

GNU-style compiler options and clang-cl path options are supported, including
joined and separate `/I` and `/FI`, object/PDB outputs, and recognized `/clang:`
forwarded path flags. Windows drive-letter and UNC paths use case-insensitive
root matching. Windows clients targeting non-Windows workers also normalize
recognized relative path arguments to forward slashes. Directory-valued paths
retain their trailing separator, and make dependency targets (`-MT`/`-MQ`)
remain literal, including when forwarded through `/clang:`.
Definitions, warning options, and prefix-map expressions retain their original
values.

Recognized absolute paths outside the execution root cause the action to fail,
including when local fallback is enabled. The error identifies the offending
paths. Worker pseudo-filesystem paths under `/proc`, `/sys`, and `/dev` are
preserved.

## Scope

Rewriting applies to recognized command-line arguments. Response-file contents
and paths embedded in file contents are unchanged.

Working-directory canonicalization has been validated with Linux LLVM builds.
For Windows clients targeting Linux workers, use `canonicalize_working_dir=false`,
as in the native Windows LLVM integration.
