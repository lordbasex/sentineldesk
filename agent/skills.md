# Skills

A skill is instructions somebody wrote for a kind of work, in Markdown, that the
agent reads when it decides the work applies.

The layout is not ours. It is what Claude Code and opencode both look for, and
following it means a skill somebody already wrote works here unchanged, and one
written here works there. A convention with several implementations is worth
more than a better one with a single implementation.

## Where they go

```
.sentineldesk/skills/<name>/SKILL.md        this project
.claude/skills/<name>/SKILL.md              this project, Claude Code's layout
.agents/skills/<name>/SKILL.md              this project
.opencode/skills/<name>/SKILL.md            this project, opencode's layout
~/.sentineldesk/skills/<name>/SKILL.md      every project
~/.claude/skills/<name>/SKILL.md            every project
~/.config/opencode/skills/<name>/SKILL.md   every project
~/.agents/skills/<name>/SKILL.md            every project
```

Nearest wins. The project directories are searched **walking up** from the
working directory to the top of the git worktree — so running the agent from a
subdirectory still finds what the project ships — and a skill beside the work
beats one in the home directory, which is the direction that makes checking a
skill into a repository worth doing.

## Plugins you already installed

A Claude Code plugin turns out to be, structurally, a directory with a `skills/`
folder in it — the same `<name>/SKILL.md` this reads. So a plugin installed
through a marketplace puts ordinary skills on disk, and those are picked up too:

```
/plugin install superpowers@superpowers-marketplace   # in Claude Code
sentineldesk-agent skills                             # they are there
```

Read from `~/.claude/plugins/installed_plugins.json` rather than by walking the
plugin directories, because the marketplace checkout beside it holds the whole
catalogue — every plugin offered, not the ones somebody chose.

**Only the skills come across.** A plugin can also ship commands, hooks, agents
and LSP wiring; those hook into another program's internals and mean nothing
here. A plugin that is mostly skills works completely; one that is mostly hooks
contributes whatever skills it happens to carry and no more.

`sentineldesk-agent skills` lists what was found and **which file it came from**,
because "it is not being picked up" is the entire failure mode of a convention
with this many search paths.

## What one looks like

```markdown
---
name: deploy-desktop
description: Rebuild the container image and restart the desktop, the way this repository expects it.
---

## Instructions

- `make up` builds both image variants and restarts the compose stack. It does
  not run tests; run `make test` first if you changed Go.
- The desktop takes about 15 seconds to come back. Wait for the MCP socket
  rather than sleeping a fixed amount.
```

YAML frontmatter with `name` and `description`, then Markdown. `name` may be
omitted and defaults to the directory. `description` may not: without one there
is no way for the agent to know when the skill applies, and a skill that is
never opened is a file nobody will notice is broken.

## How they reach the model, and why not all at once

**Only the descriptions go in the prompt.** One line each, plus a `skill_read`
tool. The model calls it with a name and gets the body.

This is the catalogue problem again, and this project already measured that one:
the tool catalogue is 98% of what a turn costs, which is why a run offers a core
set and reaches the rest through `tool_search`. Ten skills as ten documents in
every prompt of every turn would be the same mistake with a different noun. Ten
summaries cost ten lines, and the one that gets opened is the one that was
needed.

A machine with no skills pays nothing: no catalogue block, and `skill_read` is
not offered.

`skill_read` never leaves the runtime. The desktop has no idea what a skill is
and should not — these are instructions for the model, read from the working
directory, and sending them through the MCP socket would mean the daemon growing
a file reader for files that are none of its business.

## Writing a good one

The description is doing more work than it looks like. It is the only thing the
model sees until it decides to open the file, so it has to say **when** to use
the skill and not only what it is. "Rebuild the image and restart the desktop" is
a title; "Rebuild the container image and restart the desktop, the way this
repository expects it" tells a model reading a goal about restarting something
that this is the file to open.

The body should carry what somebody LEARNED, not what they could have guessed.
`make up` rebuilds and restarts — a model can work that out. That it does not
run tests, that the desktop takes about fifteen seconds, and that a connected
viewer gets dropped without warning are three things that cost somebody an
afternoon, and they are why the file exists.
