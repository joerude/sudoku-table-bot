#!/usr/bin/env bash
# Pull-based deploy for the server the bot runs on: when origin/main has moved,
# fast-forward to it, rebuild the image, recreate the container and check that
# the new build came up. Run it from cron every couple of minutes:
#
#   */2 * * * * /path/to/repo/deploy/autodeploy.sh >> /path/to/repo/deploy/autodeploy.log 2>&1
#
# Nothing reaches into the server from outside and GitHub holds no keys: the
# server only fetches a public repository. The outcome is sent to the bot's
# owner in Telegram when BOT_TOKEN and OWNER_TG_ID are set in .env.
set -euo pipefail
export PATH="/usr/local/bin:/usr/bin:/bin:$PATH"

SERVICE=bot
CONTAINER=sudoku-bot

cd "$(dirname "$(readlink -f "$0")")/.."

# One run at a time: a build takes longer than the cron interval.
exec 9>.autodeploy.lock
flock -n 9 || exit 0

log() { echo "$(date '+%F %T') $*"; }

# env_value NAME prints a value from .env without sourcing the file.
env_value() { { sed -n "s/^$1=//p" .env 2>/dev/null || true; } | tail -n 1 | tr -d '"'"'"'\r'; }

# notify TEXT tells the owner in Telegram, silently. Best-effort.
notify() {
  local token chat
  token=$(env_value BOT_TOKEN)
  chat=$(env_value OWNER_TG_ID)
  [ -n "$token" ] && [ -n "$chat" ] || return 0
  curl -fsS -m 20 -o /dev/null "https://api.telegram.org/bot${token}/sendMessage" \
    --data-urlencode "chat_id=${chat}" --data-urlencode "text=$1" \
    --data-urlencode "disable_notification=true" || log "notify failed"
}

# give_up SHA TEXT: report a commit that cannot be deployed, once. The marker
# keeps the next runs quiet until main moves again.
give_up() {
  log "$2"
  echo "$1" > .autodeploy.skip
  notify "⚠️ $2"
  exit 1
}

git fetch --quiet origin main
old=$(git rev-parse HEAD)
new=$(git rev-parse origin/main)
[ "$old" = "$new" ] && exit 0
[ "$(cat .autodeploy.skip 2>/dev/null || true)" = "$new" ] && exit 0

short=$(git rev-parse --short "$new")
if [ "$(git rev-parse --abbrev-ref HEAD)" != main ]; then
  give_up "$new" "Деплой $short пропущен: на сервере открыта не ветка main"
fi
if ! git diff --quiet || ! git diff --cached --quiet; then
  give_up "$new" "Деплой $short пропущен: на сервере изменены файлы репозитория"
fi
if ! git merge --ff-only --quiet origin/main; then
  give_up "$new" "Деплой $short пропущен: main на сервере разошелся с origin/main"
fi
subject=$(git log -1 --format=%s)
log "deploying $short: $subject"

# Keep the running image, so a build that does not start can be rolled back
# without touching git (HEAD stays on the new commit and waits for a fix).
image=$(docker compose config --images | head -n 1)
repo=${image%%:*}
have_rollback=no
if [ -n "$(docker image ls -q "$image")" ]; then
  docker tag "$image" "$repo:rollback"
  have_rollback=yes
fi

# COMMIT is stamped into the binary (see Dockerfile). A new value also forces
# the go build layer to run, so a stale build cache cannot ship the old binary.
export COMMIT="$short"
if ! docker compose build "$SERVICE"; then
  log "build failed"
  notify "⚠️ Деплой $short: сборка упала, работает прежняя версия"
  exit 1
fi

up() { docker compose up -d --force-recreate --no-build "$SERVICE"; }

# came_up SINCE [BUILD]: the container logged its start (and the build stamp).
came_up() {
  local logs
  for _ in $(seq 1 30); do
    logs=$(docker compose logs --since "$1" "$SERVICE" 2>&1 || true)
    if grep -q "bot started" <<< "$logs" && { [ -z "${2:-}" ] || grep -q "build $2" <<< "$logs"; }; then
      return 0
    fi
    sleep 3
  done
  return 1
}

since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
if ! up; then
  # A failed recreate can leave a renamed container holding the name.
  log "recreate failed, removing leftover containers"
  { docker ps -a --format '{{.Names}}' | grep -E "(^|_)${CONTAINER}\$" || true; } | xargs -r docker rm -f
  up || log "recreate failed again"
fi

if came_up "$since" "$short"; then
  log "deployed $short"
  notify "🚀 Выкатил $short: $subject"
  exit 0
fi

log "build $short did not start, last log lines:"
docker compose logs --tail 30 "$SERVICE" 2>&1 || true
if [ "$have_rollback" = yes ]; then
  docker tag "$repo:rollback" "$image"
  since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  if up && came_up "$since"; then
    log "rolled back to the previous image"
    notify "⚠️ Деплой $short не поднялся, вернул прошлую сборку. Лог: deploy/autodeploy.log"
    exit 1
  fi
fi
notify "🛑 Деплой $short не поднялся, откат не удался: бот не работает. Лог: deploy/autodeploy.log"
exit 1
