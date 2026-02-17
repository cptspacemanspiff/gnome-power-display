"""Bazel rule to download and run nFPM for packaging."""

_NFPM_VERSION = "2.45.0"

_NFPM_PLATFORMS = {
    "linux_amd64": struct(
        url = "https://github.com/goreleaser/nfpm/releases/download/v{version}/nfpm_{version}_Linux_x86_64.tar.gz",
        sha256 = "940f0c3ba8e2c9cc5669026a1c0c20453403b9c32ea4c66fd25426bcbe605a84",
    ),
    "linux_arm64": struct(
        url = "https://github.com/goreleaser/nfpm/releases/download/v{version}/nfpm_{version}_Linux_arm64.tar.gz",
        sha256 = "",
    ),
}

def _nfpm_toolchain_repo_impl(rctx):
    os = rctx.os.name
    arch = rctx.os.arch

    if "linux" not in os:
        fail("nfpm toolchain only supports Linux, got: " + os)

    if arch == "amd64" or arch == "x86_64":
        platform = "linux_amd64"
    elif arch == "aarch64" or arch == "arm64":
        platform = "linux_arm64"
    else:
        fail("Unsupported architecture: " + arch)

    info = _NFPM_PLATFORMS[platform]
    url = info.url.format(version = _NFPM_VERSION)

    kwargs = {"url": url, "sha256": "ecc22e147cb3e22105699977e3e53be0012eb8ee12a5e8e8864d01669a0c2bff"}
    if info.sha256:
        kwargs["sha256"] = info.sha256

    rctx.download_and_extract(**kwargs)

    rctx.file("BUILD.bazel", 'exports_files(["nfpm"])\n')

nfpm_toolchain_repo = repository_rule(
    implementation = _nfpm_toolchain_repo_impl,
    attrs = {},
)

def _nfpm_pkg_impl(ctx):
    nfpm = ctx.executable._nfpm

    staging = ctx.actions.declare_directory(ctx.label.name + "_staging")
    config = ctx.file.config

    packager = ctx.attr.packager
    version = ctx.attr.version
    name = ctx.attr.package_name if ctx.attr.package_name else ctx.label.name

    arch = ctx.attr.arch
    if packager == "rpm":
        rpm_arch = {"amd64": "x86_64", "arm64": "aarch64"}.get(arch, arch)
        out_name = "{}-{}-1.{}.rpm".format(name, version, rpm_arch)
    else:
        out_name = "{}_{}_{}".format(name, version, arch) + ".deb"
    out_file = ctx.actions.declare_file(out_name)

    # Collect all srcs
    src_files = []
    for target in ctx.attr.srcs:
        src_files.extend(target.files.to_list())

    # Build short_path -> staging_path remap from file_map
    remap = {}
    for label_str, dest in ctx.attr.file_map.items():
        # Find the file whose short_path contains the label's package/name
        # Label strings like "//cmd/power-monitor-daemon" need matching
        needle = label_str.lstrip("/").lstrip(":")
        remap[needle] = dest

    commands = []

    for f in src_files:
        # Check if this file matches any remap entry
        dest_path = None
        for needle, dest in remap.items():
            if needle in f.short_path:
                dest_path = staging.path + "/" + dest
                break

        if not dest_path:
            dest_path = staging.path + "/" + f.short_path

        commands.append("mkdir -p $(dirname {dest}) && cp {src} {dest}".format(
            dest = dest_path,
            src = f.path,
        ))

    # Copy config into staging root
    commands.append("cp {src} {dest}".format(
        src = config.path,
        dest = staging.path + "/" + config.basename,
    ))

    # Compute relative path from staging dir to output file
    # Both are under bazel-out/..., so use a helper
    out_abs = out_file.path
    staging_depth = staging.path.count("/") + 1
    rel_output = ("../" * staging_depth) + out_abs

    commands.append(
        "cd {staging} && {nfpm} package --config {config} --packager {packager} --target {output}".format(
            staging = staging.path,
            nfpm = ("../" * staging_depth) + nfpm.path,
            config = config.basename,
            packager = packager,
            output = rel_output,
        ),
    )

    env = {}
    if version:
        env["VERSION"] = version

    ctx.actions.run_shell(
        outputs = [out_file, staging],
        inputs = src_files + [config],
        tools = [nfpm],
        command = " && ".join(commands),
        env = env,
        mnemonic = "NfpmPackage",
        progress_message = "Packaging %s as %s" % (name, packager),
    )

    return [DefaultInfo(files = depset([out_file]))]

nfpm_pkg = rule(
    implementation = _nfpm_pkg_impl,
    attrs = {
        "config": attr.label(
            mandatory = True,
            allow_single_file = [".yaml", ".yml"],
            doc = "nFPM configuration file (nfpm.yaml)",
        ),
        "srcs": attr.label_list(
            allow_files = True,
            doc = "All source files referenced in the nfpm config contents section",
        ),
        "file_map": attr.string_dict(
            doc = "Map from substring of source short_path to destination path in staging dir. " +
                  "Use this to place Bazel outputs where the nfpm config expects them. " +
                  'E.g. {"cmd/power-monitor-daemon": "build/power-monitor-daemon"}',
        ),
        "arch": attr.string(
            default = "amd64",
            doc = "Package architecture (e.g. amd64, arm64).",
        ),
        "packager": attr.string(
            default = "rpm",
            values = ["rpm", "deb", "apk", "archlinux"],
            doc = "Package format to produce",
        ),
        "package_name": attr.string(
            doc = "Output package name (without extension). Defaults to rule name.",
        ),
        "version": attr.string(
            default = "0.1.0",
            doc = "Package version. Passed as $VERSION env var.",
        ),
        "_nfpm": attr.label(
            default = "@nfpm_toolchain//:nfpm",
            allow_single_file = True,
            executable = True,
            cfg = "exec",
        ),
    },
    doc = "Builds a system package (RPM, DEB, etc.) using nFPM.",
)
