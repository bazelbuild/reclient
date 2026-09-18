"""Apply llvm_configure to produce a llvm-project repo."""

load("@llvm-raw//utils/bazel:configure.bzl", "llvm_configure")

def _llvm_project_impl(ctx):
    llvm_configure(name = "llvm-project")

llvm_project = module_extension(
    implementation = _llvm_project_impl,
)
