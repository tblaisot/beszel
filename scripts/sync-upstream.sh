#!/usr/bin/env bash
# Rebase the fork branch onto the latest upstream release tag.
#
#   clean rebase + tests pass          -> fork branch is force-pushed (with lease)
#   conflicts, resolved by Claude      -> only sync/<tag> is pushed; a human merges it
#   unresolved conflicts / red tests   -> nothing is pushed beyond sync/<tag>, exit 1
#
# Env: FORK_BRANCH (fork)  UPSTREAM_URL  TARGET_TAG (default: newest vX.Y.Z)
#      PUSH=false for a dry run   MAX_AI_ATTEMPTS (5)
#      AI=claude|copilot (claude)  conflict resolver, only started when needed
#        claude : local login or ANTHROPIC_API_KEY      copilot: COPILOT_GITHUB_TOKEN (fine-grained PAT) or local login
set -euo pipefail

BRANCH=${FORK_BRANCH:-fork}
UPSTREAM_URL=${UPSTREAM_URL:-https://github.com/henrygd/beszel.git}
PUSH=${PUSH:-true}
MAX_AI_ATTEMPTS=${MAX_AI_ATTEMPTS:-5}
TESTS=${TESTS:-"go test -tags=testing,no_ui ./..."}
AI=${AI:-claude}
AI_USED=false

log() { printf '\n==> %s\n' "$*" >&2; }
out() { [ -n "${GITHUB_OUTPUT:-}" ] && echo "$1=$2" >>"$GITHUB_OUTPUT" || true; }

[ -z "$(git status --porcelain --untracked-files=no)" ] || { echo "working tree not clean" >&2; exit 1; }
git config rerere.enabled true # replays resolutions already made once
git config user.name >/dev/null || git config user.name "fork-sync[bot]"
git config user.email >/dev/null || git config user.email "fork-sync@users.noreply.github.com"

git remote get-url upstream >/dev/null 2>&1 || git remote add upstream "$UPSTREAM_URL"
git fetch --quiet upstream --tags --force

tag=${TARGET_TAG:-$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1)}
[ -n "$tag" ] || { echo "no upstream release tag found" >&2; exit 1; }
out tag "$tag"

if git merge-base --is-ancestor "$tag" "$BRANCH"; then
  log "$BRANCH already contains $tag"; out result up-to-date; exit 0
fi

old_base=$(git merge-base "$BRANCH" "$tag")
work=sync/$tag
git switch -q -C "$work" "$BRANCH"
log "rebasing $(git rev-list --count "$old_base..$BRANCH") commits onto $tag"

ai_ready() {
  case $AI in
    claude) [ -n "${ANTHROPIC_API_KEY:-}" ] || command -v claude >/dev/null ;;
    copilot) [ -n "${COPILOT_GITHUB_TOKEN:-}" ] || command -v copilot >/dev/null ;;
    *) echo "unknown AI=$AI (claude|copilot)" >&2; exit 1 ;;
  esac
}

# ai_run <prompt> <extra shell tool> : agent may edit files and read git state, never move history
ai_run() {
  AI_USED=true
  case $AI in
    claude)
      command -v claude >/dev/null || npm install -g @anthropic-ai/claude-code >/dev/null
      claude -p "$1" --permission-mode acceptEdits \
        --allowedTools "Read" "Edit" "Write" "Grep" "Glob" \
        "Bash(git status:*)" "Bash(git diff:*)" "Bash(git show:*)" "Bash(git log:*)" "Bash(git add:*)" "Bash($2:*)"
      ;;
    copilot)
      command -v copilot >/dev/null || npm install -g @github/copilot >/dev/null
      copilot -p "$1" -s --no-ask-user --allow-tool=write --allow-tool='shell(git:*)' --allow-tool="shell($2:*)" \
        --deny-tool='shell(git rebase)' --deny-tool='shell(git commit)' --deny-tool='shell(git push)' \
        --deny-tool='shell(git reset)' --deny-tool='shell(git checkout)'
      ;;
  esac
}

resolve_with_ai() {
  ai_run "You are in the middle of 'git rebase --onto $tag' of a fork of henrygd/beszel.
During a rebase 'ours' is upstream $tag and 'theirs' is the fork commit being replayed.
Resolve every conflicted file (git diff --name-only --diff-filter=U): keep the upstream change AND the
intent of the fork commit (see 'git show REBASE_HEAD' and CLAUDE.md, FORK.md). Rules:
- src/locales/** are generated: take upstream's version, do not hand-merge.
- No conflict markers may remain. 'git add' each resolved file. Do NOT run git rebase yourself.
- Adapt fork code to renamed/changed upstream APIs when the conflict implies it, nothing else." go
}

attempts=0
if ! git rebase --onto "$tag" "$old_base" 2>&1 | tail -3; then
  while [ -d "$(git rev-parse --git-path rebase-merge)" ] || [ -d "$(git rev-parse --git-path rebase-apply)" ]; do
    attempts=$((attempts + 1))
    if [ "$attempts" -gt "$MAX_AI_ATTEMPTS" ] || ! ai_ready; then
      git rebase --abort; out result conflict-unresolved
      echo "rebase needs a human (attempts=$attempts, AI=$AI)" >&2; exit 1
    fi
    log "conflict, asking $AI (attempt $attempts)"
    resolve_with_ai || true
    if [ -n "$(git diff --name-only --diff-filter=U)" ] || git diff --check | grep -q '^[^ ]*:[0-9]*: leftover conflict marker'; then
      continue
    fi
    GIT_EDITOR=true git rebase --continue 2>&1 | tail -3 || true
  done
fi

log "running tests"
if ! eval "$TESTS"; then
  if [ "$AI_USED" = true ] || ! ai_ready; then
    out result tests-failed
    [ "$PUSH" = true ] && git push --force-with-lease origin "$work"
    exit 1
  fi
  # clean rebase but upstream broke the fork semantically: one repair attempt
  ai_run "'$TESTS' fails after rebasing the fork onto upstream $tag. Fix the fork code (not upstream's, not the tests unless they encode removed behaviour). Do not commit." go || true
  eval "$TESTS" || { out result tests-failed; git checkout -q .; [ "$PUSH" = true ] && git push --force-with-lease origin "$work"; exit 1; }
  git commit -qam "fix: adapt fork to upstream $tag"
fi

if [ "$PUSH" != true ]; then log "dry run, not pushing"; out result dry-run; exit 0; fi

if [ "$AI_USED" = true ]; then
  git push --force-with-lease origin "$work"
  out result needs-review
  log "AI touched this rebase: review and merge branch $work into $BRANCH by hand"
else
  git tag -f "backup/$BRANCH-before-$tag" "$BRANCH"
  git push -f origin "backup/$BRANCH-before-$tag"
  git push --force-with-lease="$BRANCH" origin "$work:$BRANCH"
  out result synced
  log "$BRANCH is now based on $tag"
fi
