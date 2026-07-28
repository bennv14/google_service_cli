#!/usr/bin/env sh
# Regenerate the shell completion scripts shipped in the release archives.
# Run from the repo root; GoReleaser calls this as a `before` hook.
set -e

rm -rf completions
mkdir -p completions

for sh in bash zsh fish; do
	go run . completion "$sh" >"completions/gsvc.$sh"
done
