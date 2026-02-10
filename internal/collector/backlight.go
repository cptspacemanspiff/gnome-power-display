package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CollectBacklight reads backlight brightness from /sys/class/backlight/*.
func CollectBacklight() (*BacklightSample, error) {
	matches, err := filepath.Glob("/sys/class/backlight/*")
	if err != nil {
		return nil, fmt.Errorf("glob backlight: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no backlight found")
	}

	dir := matches[0]
	brightness, err := readIntFile(filepath.Join(dir, "brightness"))
	if err != nil {
		return nil, fmt.Errorf("read brightness: %w", err)
	}
	maxBrightness, err := readIntFile(filepath.Join(dir, "max_brightness"))
	if err != nil {
		return nil, fmt.Errorf("read max_brightness: %w", err)
	}

	return &BacklightSample{
		Timestamp:     time.Now().Unix(),
		Brightness:    brightness,
		MaxBrightness: maxBrightness,
	}, nil
}

func readIntFile(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}
