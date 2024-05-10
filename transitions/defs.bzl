"""Provides cc_platform_binary"""
_PLATFORMS = "//command_line_option:platforms"
_CPU = "//command_line_option:cpu"
_EXEC_PLATFORMS = "//command_line_option:extra_execution_platforms"
_CXXOPT = "//command_line_option:cxxopt"
_HOST_CXXOPT = "//command_line_option:host_cxxopt"

def _set_platform_impl(settings, attr):
    ''' The implementation if the _platform_transition transition.

    This transition
        - Appends all cxxopts provided to "attr.cxxopt" to --cxxopt and --host_cxxopt
        - If "attr.platform" is provided then --platforms and
            --extra_execution_platforms are overwritten to be this platform
    '''
    output = dict(settings)
    output[_CXXOPT] += attr.cxxopt
    output[_HOST_CXXOPT] += attr.cxxopt
    if attr.platform:
        output[_PLATFORMS] = str(attr.platform)
        output[_EXEC_PLATFORMS] = [str(attr.platform)]
    return output

_platform_transition = transition(
    implementation = _set_platform_impl,
    inputs = [_PLATFORMS, _EXEC_PLATFORMS, _CXXOPT, _HOST_CXXOPT],
    outputs = [_PLATFORMS, _EXEC_PLATFORMS, _CXXOPT, _HOST_CXXOPT],
)

def _copy_file(ctx, input, output):
    ctx.actions.run_shell(
        inputs = [input],
        outputs = [output],
        command = "cp %s %s" % (input.path, output.path),
        mnemonic = "Copy",
        # Sensible defaults copied from https://github.com/bazelbuild/bazel-skylib/blob/main/rules/private/copy_common.bzl
        execution_requirements = {
            "no-remote": "1",
            "no-cache": "1",
        },
    )

def _cc_platform_binary_impl(ctx):
    """ The implementation of _cc_platform_binary rule.

    The outputs of the provided cc_binary dependency are copied
    and retured by this rule.

    Args:
        ctx: The Starlark rule context.

    Returns:
        CCInfo, DefaultInfo and OutputGroupInfo providers forwarded
        from the actual_binary dependency.
    """
    actual_binary = ctx.attr.actual_binary[0]
    cc_binary_outfile = actual_binary[DefaultInfo].files.to_list()[0]
    extension = ".exe" if cc_binary_outfile.path.endswith(".exe") else ""
    outfile = ctx.actions.declare_file(ctx.label.name + extension)

    _copy_file(ctx, cc_binary_outfile, outfile)

    files = [outfile]
    result = []

    if DebugPackageInfo in actual_binary:
        wrapped_dbginfo = actual_binary[DebugPackageInfo]
        if actual_binary[DebugPackageInfo].stripped_file:
            _copy_file(ctx, actual_binary[DebugPackageInfo].stripped_file, ctx.outputs.out_stripped_file)
        result.append(
            DebugPackageInfo(
                target_label = ctx.label,
                stripped_file = ctx.outputs.out_stripped_file if wrapped_dbginfo.stripped_file else None,
                unstripped_file = outfile,
            ),
        )

    if "pdb_file" in actual_binary.output_groups:
        cc_binary_pdbfile = actual_binary.output_groups.pdb_file.to_list()[0]
        pdbfile = ctx.actions.declare_file(
            ctx.label.name + ".pdb",
            sibling = outfile,
        )
        files.append(pdbfile)
        result.append(OutputGroupInfo(pdb_file = depset([pdbfile])))
        _copy_file(ctx, cc_binary_pdbfile, pdbfile)

    # The following ensures that when a cc_platform_binary is included as a data
    # dependency that the executable is found at the correct path within the
    # .runfiles tree.
    wrapped_runfiles = actual_binary[DefaultInfo].data_runfiles.files.to_list()
    if cc_binary_outfile in wrapped_runfiles:
        # Delete the entry for ..._native_binary
        wrapped_runfiles.remove(cc_binary_outfile)
    data_runfiles = depset(direct = [outfile] + wrapped_runfiles)

    result.append(DefaultInfo(
        executable = outfile,
        data_runfiles = ctx.runfiles(files = data_runfiles.to_list()),
        files = depset(files),
    ))
    if CcInfo in actual_binary:
        result.append(actual_binary[CcInfo])
    return result

_cc_platform_binary = rule(
    implementation = _cc_platform_binary_impl,
    doc = """ Builds the provided actual_binary with changes to the platform and/or features.

    This applies the following flag changes when building actual_binary
        - Appends all cxxopts provided to "cxxopt" to --cxxopt and --host_cxxopt
        - If "platform" is provided then --platforms and
            --extra_execution_platforms are overwritten to be this platform

    This rule is otherwise a dropin replacement for cc_binary.
    """,
    attrs = {
        "actual_binary": attr.label(
            doc = "The binary to be built with the applied transition",
            providers = [CcInfo],
            cfg = _platform_transition,
        ),
        "cxxopt": attr.string_list(
            default = [],
            doc = "If specified, actual_binary and its dependencies will be built with the given cxxopts.",
        ),
        "platform": attr.label(
            default = None,
            doc = "If specified, actual_binary and its dependencies will be built for the given platform.",
            mandatory = False,
            providers = [platform_common.PlatformInfo],
        ),
        "out_stripped_file": attr.output(),
        # This attribute is required to use starlark transitions. It allows
        # allowlisting usage of this rule. For more information, see
        # https://bazel.build/extending/config#user-defined-transitions
        "_allowlist_function_transition": attr.label(
            default = "@bazel_tools//tools/allowlists/function_transition_allowlist",
        ),
    },
    # Making this executable means it works with "$ bazel run".
    executable = True,
)

def cc_platform_binary(name, platform = None, cxxopt = None, visibility = None, tags = None, **kwargs):
    """This macro is a dropin replacement for cc_binary.

    This applies the following flag changes when building the binary
        - Appends all cxxopts provided to "cxxopt" to --cxxopt and --host_cxxopt
            This is different from setting "copts" on a cc_binary target as this
            will also apply to all dependencies.
        - If "platform" is provided then --platforms and
            --extra_execution_platforms are overwritten to be this platform

    Args:
        name: The name of this target.
        platform: Optional. A plaform target to use when building this target.
        cxxopt: Optional. A list of cxxopts to pass to the compiler when building this target.
        visibility: Optional. The visibility of the target.
        tags: Optional. Tags to pass to the native cc_binary rule
        **kwargs: Arguments to pass to the native cc_binary rule
    """
    native_binary_name = name + "_native"
    _cc_platform_binary(
        name = name,
        platform = platform,
        cxxopt = cxxopt,
        actual_binary = native_binary_name,
        out_stripped_file = name + ".stripped",
        visibility = visibility,
    )
    if tags == None:
        tags = []
    native.cc_binary(
        name = native_binary_name,
        visibility = ["//visibility:private"],
        tags = tags + ["manual"],
        **kwargs
    )


_FEATURES = "//command_line_option:features"
def _dbg_transition_impl(settings, _attr):
    ''' The implementation if the _dbg_transition transition.

    This transition
        - Appends 'dbg' to the --features flag
    '''
    return {
        _FEATURES: settings.get(_FEATURES, []) + ["dbg"],
    }


_dbg_transition = transition(
    implementation = _dbg_transition_impl,
    inputs = [_FEATURES],
    outputs = [_FEATURES],
)


def _dbg_target_impl(ctx):
    return [
        DefaultInfo(files = ctx.attr.src[0].files),
        ctx.attr.src[0][OutputGroupInfo],
    ]


dbg_target = rule(
    doc = '''
    Passes through the files of the given src target with the 'dbg' feature enabled.
    ''',
    implementation = _dbg_target_impl,
    attrs = {
        "src": attr.label(cfg = _dbg_transition),
        # This attribute is required to use starlark transitions. It allows
        # allowlisting usage of this rule. For more information, see
        # https://bazel.build/extending/config#user-defined-transitions
        "_allowlist_function_transition": attr.label(
            default = "@bazel_tools//tools/allowlists/function_transition_allowlist",
        ),
    },
)

def _target_platform_transition_impl(settings, attr):
    ''' The implementation if the _target_platform_transition transition.

    This transition
        - Overrides --cpu to be attr.cpu is attr.cpu is not None
        - Overrides --platforms to be attr.platform is attr.platform is not None
    '''
    output = dict(settings)
    if attr.platform:
        output[_PLATFORMS] = [attr.platform]
    if attr.cpu:
        output[_CPU] = attr.cpu
    return output

_target_platform_transition = transition(
    implementation = _target_platform_transition_impl,
    inputs = [_PLATFORMS, _CPU],
    outputs = [_PLATFORMS, _CPU],
)

def _platform_filegroup_impl(ctx):
    return [ctx.attr.filegroup[0][DefaultInfo]]

_platform_filegroup = rule(
    implementation = _platform_filegroup_impl,
    doc = """A filegroup like rule that applies a transition to use the given cpu and or platform.""",
    attrs = {
        "filegroup": attr.label(
            mandatory = True,
            cfg = _target_platform_transition,
        ),
        "platform": attr.label(),
        "cpu": attr.string(),
        # This attribute is required to use starlark transitions. It allows
        # allowlisting usage of this rule. For more information, see
        # https://bazel.build/extending/config#user-defined-transitions
        "_allowlist_function_transition": attr.label(
            default = "@bazel_tools//tools/allowlists/function_transition_allowlist",
        ),
    },
)

def platform_filegroup(name, srcs, platform, cpu, **kwargs):
    native.filegroup(
        name = name + "_native",
        srcs = srcs,
    )
    _platform_filegroup(
        name = name,
        filegroup = ":" + name + "_native",
        platform = platform,
        cpu = cpu,
        **kwargs,
    )