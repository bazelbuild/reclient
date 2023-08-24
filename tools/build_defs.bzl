"""Provides go_proto_checkedin_test
"""

load(
    "@io_bazel_rules_go//go:def.bzl",
    "GoSource",
)

def _extract_go_src(ctx):
    """Thin rule that exposes the GoSource from a go_library."""
    return [DefaultInfo(
        files = depset(ctx.attr.library[GoSource].srcs),
        runfiles = ctx.runfiles(files = ctx.attr.library[GoSource].srcs),
    )]

extract_go_src = rule(
    implementation = _extract_go_src,
    attrs = {
        "library": attr.label(
            providers = [GoSource],
        ),
    },
)

def go_proto_checkedin_test(name, proto):
    """
    Asserts that any checked-in .pb.go code matches bazel gen.

    Args:
      name: Name for test target.
      proto: go_proto_library rule for pb.go files.
    """
    genfile = name + "_genfile"
    extract_go_src(
        name = genfile,
        library = proto,
    )

    native.sh_test(
        name = name,
        srcs = ["//tools:diff_gen_vs_workspace.sh"],
        args = [
            native.package_name(),
            "$(locations " + genfile + ")",
        ],
        deps = ["@bazel_tools//tools/bash/runfiles"],
        target_compatible_with = select({
            "@platforms//os:linux": [],
            "@platforms//os:macos": [],
            "//conditions:default": ["@platforms//:incompatible"],
        }),
        data = [
            genfile,
        ] + native.glob(["*.pb.go"]),
    )

    native.sh_binary(
        name = name + "_copy_pbgo_files",
        srcs = ["//tools:copy_to_workspace.sh"],
        args = [
            native.package_name(),
            "$(execpaths " + genfile + ")",
        ],
        data = [
            genfile,
        ],
    )
