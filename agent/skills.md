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

## Installing one

Skills are catalogued at [skills.sh](https://skills.sh) and installed with the
ecosystem's own CLI:

```
sentineldesk-agent skills install https://github.com/vercel-labs/skills --skill find-skills
```

Start with `find-skills`, because it teaches the agent to look for the others —
after it, asking for a capability is enough and the agent searches.

That command hands the work to `npx skills`. Writing our own installer would
mean re-implementing repository fetching, version pinning and a path map for
seventy-odd agents, to end up putting a Markdown file in a directory we already
read; not having to own that was the point of following the convention.

It installs as the `universal` agent, which the CLI maps to `.agents/skills` —
the neutral path, and one this runtime reads. Not as `claude-code`, though that
also lands somewhere visible here: claiming to be another agent to get a file
into a directory is true until their path map changes, at which point skills
installed "as Claude Code" go where Claude Code now keeps them and this runtime
is looking somewhere else, for a reason nobody wrote down.

Then it **moves what it just installed into `.sentineldesk/skills`**. The
neutral directory is a fine drop point and a wrong home: where a file lives is
who owns it, and a skill installed for this agent should sit under this agent's
name rather than under a shared one it happens to be able to write to.

That move is a bridge, not the end state. `sentineldesk` is not in the CLI's
path map — there are seventy-odd agents in it and we are not one — and the end
state is being in it, at which point the CLI writes to `.sentineldesk/skills`
directly and the move goes away. It is a move rather than a copy so there is
never a moment where the same skill exists twice and somebody edits the copy
that is not being read.

Only what this command installs is moved. **Nothing changes about what gets
read**: a skill installed for Claude Code or opencode still works here, which is
most of what following the convention bought in the first place.

`skills-lock.json` is left where the CLI put it. It records where each skill came
from and its hash, which is worth keeping: it is how a vendored skill is told
apart from a hand-written one, and how you check that one has not quietly
changed under you.

The command is printed before it runs, in full. A skill is instructions the
agent will follow with whatever permissions it holds, which is exactly what
makes the ecosystem useful and exactly what is worth being deliberate about.

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
