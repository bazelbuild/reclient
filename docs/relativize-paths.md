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

The execution root must cover the toolchain and source/build inputs that need to
be uploaded. Relativization changes command arguments; it does not discover or
upload additional toolchain files. Declare those inputs through the existing
input and toolchain configuration mechanisms.

GNU-style compiler options and clang-cl path options are supported, including
joined and separate `/I` and `/FI`, object/PDB outputs, and recognized `/clang:`
forwarded path flags. Windows drive-letter and UNC paths use case-insensitive
root matching. Windows clients targeting non-Windows workers also normalize
recognized relative path arguments to forward slashes.

Absolute paths outside the execution root are preserved and reported. Enable
`-fail_on_paths_outside_exec_root` with `-relativize_paths` to reject these actions.
Such a rejection also prevents remote-local fallback from hiding the error.

## Limits

- The rewriter recognizes command-line path spellings; it does not rewrite
  arbitrary file contents. It reports response-file arguments because their
  contents are not rewritten by this pass.
- Preprocessor definitions, warning options, prefix-map expressions, and other
  values whose meaning is not a filesystem path are deliberately preserved.
- `/proc`, `/sys`, and `/dev` paths are preserved as worker filesystem paths.
- Paths embedded in generated filenames or file contents require build-system
  handling. This option alone does not guarantee matching action keys across
  checkouts: inputs, working directories, environment, and all other command
  properties must also agree.
- Use without working-directory canonicalization until the interaction has
  dedicated coverage. Existing canonicalization changes other parts of the
  command and input tree independently of this pass.
