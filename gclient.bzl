def _gclient_repository_rule(ctx):
    ctx.report_progress("Configuring Gclient...")
    _config(ctx)
    ctx.report_progress("Cloning {}...".format(ctx.attr.remote))
    _clone(ctx)
    ctx.report_progress("Applying required patches: {}...".format(ctx.attr.patches))
    _patch(ctx)
    ctx.report_progress("Syncing to {}...".format(ctx.attr.revision))
    _sync(ctx)
    ctx.report_progress("Saving version...")
    _version(ctx)
    ctx.report_progress("Optionally stripping...")
    _strip(ctx)
    ctx.report_progress("Running gn gen...")
    _gn(ctx)

    ctx.report_progress("Trimming build.ninja file from regeneration...")
    _trim_build(ctx)
    ctx.report_progress("Trimming extra files...")
    _clean_extra_files(ctx)
    ctx.report_progress("Adding BUILD file...")
    _add_build_file(ctx)

def _config(ctx):
    if __is_windows(ctx) and len(ctx.attr.gclient_vars_windows) > 0:
        __execute(ctx, __prefix(ctx, "gclient") + ["config", ctx.attr.remote, "--custom-var=" + ctx.attr.gclient_vars_windows])
    else:
        __execute(ctx, __prefix(ctx, "gclient") + ["config", ctx.attr.remote])

def _clone(ctx):
    __execute(ctx, __prefix(ctx, "git") + ["clone", ctx.attr.remote])

def _sync(ctx):
    __execute(ctx, __prefix(ctx, "gclient") + ["sync", "-r", ctx.attr.revision, "--shallow"])

def _patch(ctx):
    for arg in ctx.attr.patch_args:
        if not arg.startswith("-p"):
            return False

    for patchfile in ctx.attr.patches:
        ctx.patch(patchfile, int(ctx.attr.patch_args[-1][2:]))

    # gclient sync will complain otherwise
    # this method misses added files, which must be added with `git add .`, but we
    # can add them when we need them.
    if ctx.attr.patches:
        __execute(ctx, __prefix(ctx, "git") + ["config", "user.email", "foundry-x@google.com"], wd = ctx.attr.base_dir)
        __execute(ctx, __prefix(ctx, "git") + ["config", "user.name", "Foundry X CI"], wd = ctx.attr.base_dir)
        __execute(ctx, __prefix(ctx, "git") + ["commit", "-am."], wd = ctx.attr.base_dir)

def _version(ctx):
    command = __prefix(ctx, "git") + ["log", "-1", "--pretty=format:%H@%ct"]
    st = ctx.execute(command, environment = __env(ctx), working_directory = ctx.attr.base_dir)
    if st.return_code != 0:
        __error(ctx.name, command, st.stdout, st.stderr)
    ctx.file("version", st.stdout)

def _add_build_file(ctx):
    if ctx.attr.build_file:
        ctx.file("BUILD.bazel", ctx.read(ctx.attr.build_file))
    elif ctx.attr.build_file_content:
        ctx.file("BUILD.bazel", ctx.attr.build_file_content)

def _strip(ctx):
    root = ctx.path(".")
    if ctx.attr.strip_prefix:
        target_dir = "{}/{}".format(root, ctx.attr.strip_prefix)
        if not ctx.path(target_dir).exists:
            fail("strip_prefix at {} does not exist in repo".format(ctx.attr.strip_prefix))
        for item in ctx.path(target_dir).readdir():
            ctx.symlink(item, root.get_child(item.basename))

def _gn(ctx):
    if __is_macos(ctx):
        # We cannot condition this repository rule on whether we want to build for an Intel target
        # or an Arm target. Thus we will generate the ninja files for both Intel and Arm in
        # different output directories, and conditionally run ninja in one of the output
        # directories depending on configuration.
        gn_args = ctx.attr.gn_args_macos_x86
        __execute(ctx, __prefix(ctx, "gn") + ["gen", "out", "--args={}".format(gn_args)], wd = ctx.attr.base_dir)
        gn_args = ctx.attr.gn_args_macos_arm64
        __execute(ctx, __prefix(ctx, "gn") + ["gen", "out_arm64", "--args={}".format(gn_args)], wd = ctx.attr.base_dir)
        return
    gn_args = ctx.attr.gn_args_linux
    if __is_windows(ctx):
        gn_args = ctx.attr.gn_args_windows
    __execute(ctx, __prefix(ctx, "gn") + ["gen", "out", "--args={}".format(gn_args)], wd = ctx.attr.base_dir)

# When using sandbox, ninja will think it has to rebuild "build.ninja"
# using gn, but we don't want to build steps to rely on gn at all.
def _trim_build(ctx):
    # https://stackoverflow.com/questions/5694228/sed-in-place-flag-that-works-both-on-mac-bsd-and-linux
    __execute(ctx, __prefix(ctx, "sed") + ["-i.bak", "-e", "s/^build build\\.ninja.*$//g", "out/build.ninja"], wd = ctx.attr.base_dir)
    __execute(ctx, __prefix(ctx, "sed") + ["-i.bak", "-e", "s/^  generator.*$//g", "out/build.ninja"], wd = ctx.attr.base_dir)
    __execute(ctx, __prefix(ctx, "sed") + ["-i.bak", "-e", "s/^  depfile.*$//g", "out/build.ninja"], wd = ctx.attr.base_dir)
    __execute(ctx, __prefix(ctx, "rm") + ["-f", "out/build.ninja.bak"], wd = ctx.attr.base_dir)

def _clean_extra_files(ctx):
    __execute(ctx, __prefix(ctx, "find") + [".", "-name", "BUILD", "-delete"])
    __execute(ctx, __prefix(ctx, "find") + [".", "-name", "BUILD.bazel", "-delete"])
    __execute(ctx, __prefix(ctx, "find") + [".", "-name", "WORKSPACE", "-delete"])
    __execute(ctx, __prefix(ctx, "find") + [".", "-name", "WORKSPACE.bazel", "-delete"])

    # Defining all the actual required files on //external/BUILD.goma is hard,
    # so instead we trim anything that may affect our action cacheability.
    delPaths = ["*/.git*", "*/.cipd*", "*/depot_tools*", "*/testdata*", "*/test/data*"]

    if __is_windows(ctx):
        for i, path in enumerate(delPaths):
            delPaths[i] = "'" + path + "'"

    if __is_windows(ctx) or __is_macos(ctx):
        __execute(ctx, __prefix(ctx, "find") + [".", "-path", "'*/buildtools/third_party/libc++*'", "-delete"])
        __execute(ctx, __prefix(ctx, "find") + [".", "-path", "'*/__pycache__*'", "-delete"])

    for path in delPaths:
        __execute(ctx, __prefix(ctx, "find") + [".", "-path", path, "-delete"])

    # symlink loop? /private/var/tmp/_bazel_ukai/8e91f546e4f29bd6c4479ddfa3f9b1f4/external/goma/client/build/mac_files/xcode_binaries/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk/System/Library/Frameworks/Ruby.framework/Versions/2.3/Headers/ruby/ruby
    __execute(ctx, __prefix(ctx, "find") + [".", "-type", "l", "-name", "ruby", "-delete"])

    # ERROR: /private/var/tmp/_bazel_ukai/8e91f546e4f29bd6c4479ddfa3f9b1f4/external/goma/BUILD.bazel:1:10: @goma//:srcs: invalid label 'client/build/mac_files/xcode_binaries/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk/usr/share/man/man3/APR::Base64.3pm' in element 27843 of attribute 'srcs' in 'filegroup' rule: invalid target name 'client/build/mac_files/xcode_binaries/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk/usr/share/man/man3/APR::Base64.3pm': target names may not contain ':'
    __execute(ctx, __prefix(ctx, "find") + [".", "-path", "*/share/man/*", "-delete"])

    __execute(ctx, __prefix(ctx, "find") + [".", "-name", "WORKSPACE.bazel", "-delete"])

def __execute(ctx, command, wd = ""):
    st = ctx.execute(command, environment = __env(ctx), working_directory = wd)
    if st.return_code != 0:
        __error(ctx.name, command, st.stdout, st.stderr, __env(ctx))

def __is_windows(ctx):
    return ctx.os.name.lower().find("windows") != -1

def __is_macos(ctx):
    return ctx.os.name == "mac os x"

def __env(ctx):
    if __is_windows(ctx):
        osenv = ctx.os.environ
        osenv["PATH"] = "C:\\src\\depot_tools;" + osenv["PATH"]
        osenv["DEPOT_TOOLS_WIN_TOOLCHAIN"] = "0"
        return osenv
    return ctx.os.environ

def __prefix(ctx, cmd):
    if __is_windows(ctx):
        return ["cmd", "/c", cmd]
    return [cmd]

def __error(name, command, stdout, stderr, environ):
    command_text = " ".join([str(item).strip() for item in command])
    fail("error running '%s' while working with @%s:\nstdout:%s\nstderr:%s\nenviron:%s" % (command_text, name, stdout, stderr, environ))

gclient_repository = repository_rule(
    implementation = _gclient_repository_rule,
    attrs = {
        "remote": attr.string(
            mandatory = True,
            doc = "The URI of the remote repository.",
        ),
        "revision": attr.string(
            mandatory = True,
            doc = "Specific revision to be checked out.",
        ),
        "strip_prefix": attr.string(
            doc = "Whether to strip a prefix folder.",
        ),
        "base_dir": attr.string(
            doc = "Base dir to run gn from.",
        ),
        "gclient_vars_windows": attr.string(
            doc = "Gclient custom vars on windows.",
        ),
        "gn_args_linux": attr.string(
            doc = "Args to pass to gn on linux.",
        ),
        "gn_args_macos_arm64": attr.string(
            doc = "Args to pass to gn on macos for Arm target.",
        ),
        "gn_args_macos_x86": attr.string(
            doc = "Args to pass to gn on macos for x86_64 target.",
        ),
        "gn_args_windows": attr.string(
            doc = "Args to pass to gn on windows.",
        ),
        "build_file": attr.label(
            allow_single_file = True,
            doc = "The file to be used as the BUILD file for this repo. Takes" +
                  "precedence over build_file_content.",
        ),
        "build_file_content": attr.string(
            doc = "The content for the BUILD file for this repository. " +
                  "Either build_file or build_file_content must be specified.",
        ),
        "patches": attr.label_list(
            default = [],
            doc =
                "A list of files that are to be applied as patches after" +
                "extracting the archive. **ONLY** supports Bazel-native patch" +
                "implementation.",
        ),
        "patch_args": attr.string_list(
            default = ["-p0"],
            doc =
                "The arguments given to the patch tool. Defaults to -p0." +
                "**ONLY** supports -p#.",
        ),
    },
)
