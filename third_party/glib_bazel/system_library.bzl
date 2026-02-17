"""Rule that creates a CcInfo provider with system includes and linkopts."""

load("@rules_cc//cc/common:cc_info.bzl", "CcInfo")
load("@rules_cc//cc/common:cc_common.bzl", "cc_common")

def _system_library_impl(ctx):
    compilation_ctx = cc_common.create_compilation_context(
        system_includes = depset(ctx.attr.includes),
        defines = depset(ctx.attr.defines),
    )
    linker_input = cc_common.create_linker_input(
        owner = ctx.label,
        user_link_flags = depset(ctx.attr.linkopts),
    )
    linking_ctx = cc_common.create_linking_context(
        linker_inputs = depset([linker_input]),
    )
    return [CcInfo(
        compilation_context = compilation_ctx,
        linking_context = linking_ctx,
    )]

system_library = rule(
    implementation = _system_library_impl,
    attrs = {
        "includes": attr.string_list(doc = "Absolute system include paths"),
        "defines": attr.string_list(doc = "Preprocessor defines"),
        "linkopts": attr.string_list(doc = "Linker flags"),
    },
    provides = [CcInfo],
)
