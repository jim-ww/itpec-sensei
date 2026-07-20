#!/usr/bin/env bash
# Shrinks the question bank's PNGs (data/questions/images/**/*.png) using
# ffmpeg's palette quantization (256 colors, no dithering) — near-lossless
# for the line-art/text/diagram screenshots, since they
# already use a small color range; ~33% smaller in testing.
#
# Usage: scripts/optimize_images.sh [dir]   (default: data/questions/images)
set -euo pipefail

dir="${1:-data/questions/images}"
if [ ! -d "$dir" ]; then
	echo "error: $dir not found" >&2
	exit 1
fi

optimize_one() {
	local src="$1"
	local tmp
	tmp="$(mktemp "${src}.XXXXXX.png")"
	if ffmpeg -y -loglevel error -i "$src" \
		-lavfi "split[a][b];[a]palettegen=max_colors=256:reserve_transparent=0[p];[b][p]paletteuse=dither=none" \
		"$tmp"; then
		local before after
		before=$(stat -c%s "$src")
		after=$(stat -c%s "$tmp")
		if [ "$after" -lt "$before" ]; then
			mv "$tmp" "$src"
		else
			rm -f "$tmp"
		fi
	else
		echo "ffmpeg failed on $src" >&2
		rm -f "$tmp"
		return 1
	fi
}
export -f optimize_one

find "$dir" -name '*.png' -print0 | xargs -0 -P "$(nproc)" -I{} bash -c 'optimize_one "$@"' _ {}
