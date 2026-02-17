"""Module extension for pkg_config repository rules."""

load("//third_party/glib_bazel:pkg_config.bzl", "pkg_config")

def _pkg_config_ext_impl(mctx):
    for mod in mctx.modules:
        for lib in mod.tags.lib:
            pkg_config(name = lib.name, pkg = lib.pkg)

pkg_config_ext = module_extension(
    implementation = _pkg_config_ext_impl,
    tag_classes = {
        "lib": tag_class(attrs = {
            "name": attr.string(mandatory = True),
            "pkg": attr.string(mandatory = True),
        }),
    },
)
