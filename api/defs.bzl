""" Defines bq_schema_proto_library for generating bigquery schemas. """
load("@rules_proto//proto:defs.bzl", "proto_common")

def _bq_schema_proto_library_impl(ctx):
    """ Implementation of bq_schema_proto_library

    Logic is inspired by _py_proto_aspect_impl in https://github.com/protocolbuffers/protobuf/blob/main/bazel/py_proto_library.bzl
    """
    generated_sources = proto_common.declare_generated_files(
        actions = ctx.actions,
        proto_info = ctx.attr.src[ProtoInfo],
        extension = ".schema",
    )
    additional_args = ctx.actions.args()
    additional_args.add("--bq-schema_opt=single-message")
    proto_common.compile(
        actions = ctx.actions,
        proto_info = ctx.attr.src[ProtoInfo],
        proto_lang_toolchain_info = ctx.attr._proto_lang_toolchain[proto_common.ProtoLangToolchainInfo],
        generated_files = generated_sources,
        additional_args = additional_args,
    )
    return [DefaultInfo(
        files = depset(generated_sources),
    )]

bq_schema_proto_library = rule(
    doc = """ Generates a bigquery schema from a proto file using protoc-gen-bq-schema

    Args:
        src: Label of the proto_library rule for the bigquery schema
    
    Example:

    proto_library(
        name = "example_proto",
        srcs = ["example.proto"]
    )

    bq_schema_proto_library(
        name = "example_bq_schema_proto",
        src = ":example_proto"
    )

    """,
    implementation = _bq_schema_proto_library_impl,
    attrs = {
        "src": attr.label(providers = [ProtoInfo]),
        "_proto_lang_toolchain": attr.label(default = "//api:bq_schema_proto_lang_toolchain", providers = [proto_common.ProtoLangToolchainInfo])
    }
)
