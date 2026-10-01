#!/bin/sh
# Isolate prompts, history, and CSV output from the developer's environment.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
for dependency in vhs ttyd ffmpeg; do
    command -v "$dependency" >/dev/null 2>&1 || {
        echo "Missing $dependency. Install VHS and its dependencies; see assets/README.md." >&2
        exit 1
    }
done
recording_dir=$(mktemp -d "${TMPDIR:-/tmp}/krm-vhs.XXXXXX")
trap 'rm -rf "$recording_dir"' EXIT HUP INT TERM
mkdir -p "$recording_dir/assets"
cp "$root/assets/demo.tape" "$recording_dir/demo.tape"
cd "$recording_dir"
export PATH="$root/bin:$PATH"
export TERM=xterm-256color COLORTERM=truecolor
unset NO_COLOR BASH_ENV ENV PROMPT_COMMAND
vhs demo.tape
cp assets/krm-demo.gif "$root/assets/krm-demo.gif"
