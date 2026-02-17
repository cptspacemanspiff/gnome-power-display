package main

import (
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

func loadCSS() {
	provider := gtk.NewCSSProvider()
	provider.LoadFromString(`
		.stats-bar {
			padding: 8px 12px;
			background: alpha(@accent_bg_color, 0.15);
			border-radius: 8px;
		}
		.stat-title {
			font-size: 11px;
			color: alpha(@window_fg_color, 0.7);
		}
		.stat-value {
			font-size: 18px;
			font-weight: bold;
			color: @accent_color;
		}
		.time-range-bar {
			padding: 4px 0;
		}
	`)
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(),
		provider,
		gtk.STYLE_PROVIDER_PRIORITY_APPLICATION,
	)
}
