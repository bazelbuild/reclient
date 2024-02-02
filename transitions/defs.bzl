"""Provides cc_platform_binary"""
_CXXOPT = "//command_line_option:cxxopt"
_HOST_CXXOPT = "//command_line_option:host_cxxopt"

def _set_platform_impl(settings, attr):
    ''' The implementation if the _platform_transition transition.

    This transition
        - Appends all cxxopts provided to "attr.cxxopt" to --cxxopt and --host_cxxopt
    '''
    return {
        _CXXOPT: settings.get(_CXXOPT, []) + attr.cxxopt,
        _HOST_CXXOPT: settings.get(_CXXOPT, []) + attr.cxxopt
    }

_platform_transition = transition(
    implementation = _set_platform_impl,
    inputs = [_CXXOPT, _HOST_CXXOPT],
    outputs = [_CXXOPT, _HOST_CXXOPT],
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

    ctx.actions.run_shell(
        inputs = [cc_binary_outfile],
        outputs = [outfile],
        command = "cp %s %s" % (cc_binary_outfile.path, outfile.path),
        mnemonic = "Copy",
    )

    files = [outfile]
    result = []
    if "pdb_file" in actual_binary.output_groups:
        cc_binary_pdbfile = actual_binary.output_groups.pdb_file.to_list()[0]
        pdbfile = ctx.actions.declare_file(
            ctx.label.name + ".pdb",
            sibling = outfile,
        )
        files.append(pdbfile)
        result.append(OutputGroupInfo(pdb_file = depset([pdbfile])))
        ctx.actions.run_shell(
            inputs = [cc_binary_pdbfile],
            outputs = [pdbfile],
            command = "cp %s %s" % (cc_binary_pdbfile.path, pdbfile.path),
            mnemonic = "Copy",
        )

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

def cc_platform_binary(name, cxxopt = None, visibility = None, tags = None, **kwargs):
    """This macro is a dropin replacement for cc_binary.

    This appends all cxxopts provided to "cxxopt" to --cxxopt and --host_cxxopt.
    This is different from setting "copt" on a cc_binary target as this will also apply to all dependencies.

    Args:
        name: The name of this target.
        cxxopt: Optional. A list of cxxopts to pass to the compiler when building this target.
        visibility: Optional. The visibility of the target.
        tags: Optional. Tags to pass to the native cc_binary rule
        **kwargs: Arguments to pass to the native cc_binary rule
    """
    native_binary_name = name + "_native"
    _cc_platform_binary(
        name = name,
        cxxopt = cxxopt,
        actual_binary = native_binary_name,
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
