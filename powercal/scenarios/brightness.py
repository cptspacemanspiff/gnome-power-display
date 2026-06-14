"""Display-power calibration sweep: average power at each backlight level, as a percentage.

Levels are percentages of the panel's ``max_brightness`` -- ``brightness-50`` is 50%, regardless
of whether the raw scale is 0-937, 0-96000, etc. The max is resolved lazily at apply-time
(``backlight_percent``), so building/listing scenarios never touches sysfs and works on any panel.
"""

from powercal import Scenario, backlight_percent

PERCENTS = (0, 25, 50, 75, 100)


def build():
    return [
        Scenario(
            name=f"brightness-{pct}",
            setup=backlight_percent(pct),   # pct% of max_brightness, resolved at run time
            settle_s=5,                     # let the battery averaging window flush after the change
            target_w=0.2,                  # measure until the 2-sigma error bar is <= 0.05 W
            meta={"percent": pct},
        )
        for pct in PERCENTS
    ]
