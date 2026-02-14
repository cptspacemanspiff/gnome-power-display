"""Module extension for nFPM toolchain."""

load(":nfpm.bzl", "nfpm_toolchain_repo")

def _nfpm_ext_impl(mctx):
    nfpm_toolchain_repo(name = "nfpm_toolchain")

nfpm = module_extension(
    implementation = _nfpm_ext_impl,
)
