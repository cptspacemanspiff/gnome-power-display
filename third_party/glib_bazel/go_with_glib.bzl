"""Go library macro that auto-injects system cdeps for cgo targets.

Define a function per dep-set, then reference it from gazelle_override:
    "gazelle:map_kind go_library go_library_gtk4 //third_party/glib_bazel:go_with_glib.bzl"
"""

load("@rules_go//go:def.bzl", _go_library = "go_library")

def _go_library_with_cdeps(extra_cdeps, name, cgo = False, cdeps = None, **kwargs):
    cdeps = list(cdeps or [])
    if cgo:
        for dep in extra_cdeps:
            if dep not in cdeps:
                cdeps.append(dep)
    _go_library(name = name, cgo = cgo, cdeps = cdeps, **kwargs)

# Dep-set: just glib/gobject/gio
def go_library_glib(name, **kwargs):
    _go_library_with_cdeps(["@@//third_party/glib_bazel:glib"], name, **kwargs)

# Dep-set: glib + gtk4
def go_library_gtk4(name, **kwargs):
    _go_library_with_cdeps(["@@//third_party/glib_bazel:glib", "@@//third_party/glib_bazel:gtk4"], name, **kwargs)

# Dep-set: glib + gtk4 + libadwaita
def go_library_adwaita(name, **kwargs):
    _go_library_with_cdeps(["@@//third_party/glib_bazel:glib", "@@//third_party/glib_bazel:gtk4", "@@//third_party/glib_bazel:libadwaita"], name, **kwargs)
