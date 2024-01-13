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
LLVM_COMMIT = "82e851a407c52d65ce65e7aa58453127e67d42a0"
LLVM_SHA256 = "c45d3e776d8f54362e05d4d5da8b559878077241d84c81f69ed40f11c0cdca8f"

SDK_COMMIT = "67d0b1858999b5ca013caa62922d5a08e44feb77"
PROTOC_GEN_BQ_SCHEMA_VERSION = "v0.0.0-20190119112626-026f9fcdf705"
GO_GRPC_VERSION = "v1.56.2"
GO_PROTO_VERSION = "v1.25.0"
