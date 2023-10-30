ScannerProvider = provider(fields = ["scanner"])
scanners = ["clangscandeps", "goma", "clangscandeps-service", "goma-service"]

def _impl(ctx):
    scanner = ctx.build_setting_value
    if scanner not in scanners:
        fail(str(ctx.label) + " include scanner can only be {" +
             ", ".join(scanners) + "} but was set to " +
             scanner)
    return ScannerProvider(scanner = scanner)

include_scanner_rule = rule(
    implementation = _impl,
    build_setting = config.string(flag = True),
)

# Refer to go/rbe/dev/x/playbook/upgrading_clang_scan_deps
# to update clang-scan-deps version.
LLVM_COMMIT = "c4c5e79dd4b4c78eee7cffd9b0d7394b5bedcf12"
LLVM_SHA256 = "2bc1ff5a49b6419622e507bb7ede95a6f53b4579d2f160a9a65c8350185146e5"

SDK_COMMIT = "e00bd323ce426cd1c55dec2f152ffcc20eb4f503"
PROTOC_GEN_BQ_SCHEMA_VERSION = "v0.0.0-20190119112626-026f9fcdf705"
GO_GRPC_VERSION = "v1.56.2"
GO_PROTO_VERSION = "v1.25.0"
