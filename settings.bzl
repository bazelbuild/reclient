ScannerProvider = provider(fields = ["scanner"])
scanners = ["clangscandeps", "goma"]

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
