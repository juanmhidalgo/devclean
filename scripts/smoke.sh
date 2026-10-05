#!/usr/bin/env bash
# Smoke test: builds devclean and runs it end to end against a throwaway
# HOME/XDG sandbox, checking behaviors the unit tests cover only piecewise.
#
# Never touches the real home, config, state or systemd units, and never runs
# `schedule install` for real (it only checks a cadence that is rejected
# before anything is written). Docker is only read, except for two throwaway
# images it creates and removes. Exits 1 if any check fails.
#
# Usage: scripts/smoke.sh
set -uo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
SB=$(mktemp -d)
BIN=$SB/devclean
IMAGES=(dc-smoke-scratch-a:1 dc-smoke-scratch-b:2 dc-smoke-scratch-c:1 dc-smoke-keep:1)
cleanup() {
	docker image rm "${IMAGES[@]}" >/dev/null 2>&1
	rm -rf "$SB"
}
trap cleanup EXIT

go build -o "$BIN" "$REPO/cmd/devclean" || exit 1

FAILED=0
check() { # check <description> <command...>
	local desc=$1
	shift
	if "$@"; then echo "PASS  $desc"; else echo "FAIL  $desc"; FAILED=1; fi
}
dc() {
	HOME=$SB/home XDG_CONFIG_HOME=$SB/cfg XDG_STATE_HOME=$SB/state \
		XDG_CACHE_HOME=$SB/cache XDG_DATA_HOME=$SB/data "$BIN" "$@"
}
config() { printf "$@" >"$SB/cfg/devclean/config.toml"; }
# candidates <json file>: one "<path|volume|image_id> <tier>" line each.
candidates() {
	python3 -c 'import json,sys
for c in json.load(open(sys.argv[1]))["candidates"]:
    print(c.get("path") or c.get("volume") or c.get("image_id") or c.get("action"), c["tier"])' "$1"
}
old() { touch -d 2020-01-01 "$@"; }
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
	GIT_AUTHOR_DATE=2020-01-01T00:00:00Z GIT_COMMITTER_DATE=2020-01-01T00:00:00Z
g() { git -C "$1" -c user.name=t -c user.email=t@example.com "${@:2}"; }

H=$SB/home W=$SB/home/work
mkdir -p "$SB/cfg/devclean" "$H"

# A project repo with two stale node_modules, one of them under an exclude.
mkdir -p "$W/keep/p/node_modules" "$W/q/node_modules"
touch "$W/keep/p/package.json" "$W/q/package.json"
g "$W" init -q && g "$W" add . && g "$W" commit -qm c
old "$W/.git/HEAD" "$W/.git/index" "$W"/keep/p/* "$W"/q/*

# Excludes: "~" and relative config entries resolve against home, --root
# against the working directory.
for exclude in "~/work/keep" "work/keep"; do
	config '[scan]\nexcludes = ["%s"]\n' "$exclude"
	(cd "$W" && dc report --root . --only projects --json) >"$SB/out.json"
	check "--root . with exclude \"$exclude\" keeps only q/node_modules" \
		test "$(candidates "$SB/out.json")" = "$W/q/node_modules stale"
done

# A home versioned for its dotfiles lends no git signal to untracked dirs.
touch "$H/.bashrc" && g "$H" init -q && g "$H" add .bashrc && g "$H" commit -qm dotfiles
mkdir -p "$H/Downloads/x/node_modules" && touch "$H/Downloads/x/package.json"
old "$H/.git/HEAD" "$H/.git/index" "$H"/Downloads/x/*
config ''
dc report --root "$H/Downloads" --only projects --json >"$SB/out.json"
check "untracked dir under a dotfiles home is never stale" \
	test -z "$(candidates "$SB/out.json")"

# observe --root walks part of the roots: not the daily walk.
dc observe --root "$W" >/dev/null 2>&1
check "observe --root does not record the daily walk" \
	python3 -c 'import json,sys; sys.exit(bool(json.load(open(sys.argv[1])).get("walked")))' \
	"$SB/state/devclean/history.json"

# A history save failure after deleting still reports and notifies, exit 1.
config 'notify_command = "cat > %s/notification"\n' "$SB"
rm -f "$SB/state/devclean/history.json.lock"
mkdir -p "$SB/state/devclean/history.json.lock/x" # makes only the save fail
dc clean --root "$W" --only projects --yes --quiet >"$SB/out.txt" 2>&1
code=$?
check "history save failure exits 1" test "$code" = 1
check "history save failure still prints the outcome" grep -q "^Deleted 2" "$SB/out.txt"
check "history save failure still notifies" grep -q "saving history" "$SB/notification"
rm -rf "$SB/state/devclean/history.json.lock"

# An invalid cadence is rejected before any unit is written.
config 'clean_cadence = "garbage"\n'
dc schedule install >/dev/null 2>&1
code=$?
check "invalid clean_cadence exits 1" test "$code" = 1
check "invalid clean_cadence writes no unit" test ! -e "$SB/cfg/systemd/user"

if docker info >/dev/null 2>&1; then
	printf 'FROM scratch\nLABEL devclean-smoke=1\n' >"$SB/Dockerfile"
	docker build -q -t "${IMAGES[0]}" "$SB" >/dev/null && docker tag "${IMAGES[0]}" "${IMAGES[1]}"
	printf 'FROM scratch\nLABEL devclean-smoke=2\n' >"$SB/Dockerfile"
	docker build -q -t "${IMAGES[2]}" "$SB" >/dev/null && docker tag "${IMAGES[2]}" "${IMAGES[3]}"
	multi=$(docker image inspect -f '{{.Id}}' "${IMAGES[0]}")
	mixed=$(docker image inspect -f '{{.Id}}' "${IMAGES[2]}")
	config '[docker]\nephemeral_globs = ["dc-smoke-scratch-*"]\n'
	dc report --only docker --json >"$SB/out.json"
	check "image with two ephemeral tags is one candidate" \
		test "$(candidates "$SB/out.json" | grep -c "^$multi ")" = 1
	check "image with a non-ephemeral tag is not a candidate" \
		test "$(candidates "$SB/out.json" | grep -c "^$mixed ")" = 0
	if [ -n "$(docker volume ls -q)" ]; then
		check "volumes are reported as \"volume\"" \
			python3 -c 'import json,sys
c=[c for c in json.load(open(sys.argv[1]))["candidates"] if (c.get("command") or "").startswith("docker volume rm ")]
sys.exit(not c or any("volume" not in x or "action" in x for x in c))' "$SB/out.json"
	else
		echo "SKIP  volumes are reported as \"volume\": no docker volumes"
	fi
else
	echo "SKIP  docker checks: daemon not reachable"
fi

exit "$FAILED"
