---
title: "When State Is Keyed by Absolute Path, Syncing Files Isn't Enough"
date: "2026-07-31"
description: "A tool that keys per-project state by absolute working directory. Copy the files to a second machine with a different home directory and you don't merge history — you accumulate parallel copies of it. One repository had three. On identifiers derived from environment facts, and why making the environments agree beats translating forever."
tags:
  [
    "tooling",
    "developer-experience",
    "sync",
    "unix",
    "portability",
  ]
---

I work on the same projects from a Linux desktop and a MacBook. My editor config, shell and dotfiles have followed me between machines for years through a git repo, and I assumed extending that to a tool's local state was a matter of picking a sync mechanism.

Then I listed the state directory and found this:

```
-home-vicente-github-r10-r10-hub
-Users-paulovicente-github-r10-r10-hub
-Users-paulovicente-github-r10-hub
```

Three directories. One repository. Two of them on the same machine.

## The mechanism

The tool keys per-project state by the absolute working directory it was launched from, flattened into a directory name. `/home/vicente/github/r10/r10-hub` becomes `-home-vicente-github-r10-r10-hub`.

That's a reasonable design. It needs a stable per-project identifier, it can't rely on the project being a git repo, and the working directory is right there. It's also the thing that makes the state non-portable, because the identifier encodes a fact about the machine — where your home directory lives.

Linux gives me `/home/vicente`. macOS gives me `/Users/paulovicente`. So the same project has two identities, and copying files between machines produces both of them side by side rather than one merged history. The third directory was my own fault: I'd cloned the same repo to `~/github/r10-hub` on one machine and `~/github/r10/r10-hub` on the other, splitting the history again without any OS involvement.

The failure is quiet. Nothing errors. The tool works perfectly on both machines. You just can't resume yesterday's session, because as far as the tool is concerned yesterday happened in a different project.

## Two ways out

**Translate on every sync.** Rewrite the directory names, and the absolute paths inside the files, each time state moves. Works, and it's what I ended up doing for the one-time transfer.

**Make the environments agree.** Pick one canonical path and make it valid on both machines, so the identifier is the same everywhere and no translation is needed.

The second is better, and one direction is much cheaper than the other. On Linux, `/Users` is unused, so:

```bash
sudo mkdir -p /Users && sudo ln -s /home/vicente /Users/paulovicente
```

Now `/Users/paulovicente/github/r10/r10-hub` resolves on both machines, and if I habitually `cd` there, every session writes to one directory regardless of which machine I'm on. The reverse — creating `/home` on macOS — needs `/etc/synthetic.conf` and fights autofs, which already claims that path. Doable, more fragile, and there's no reason to pick the harder direction.

This is the general shape: when an identifier is derived from an environment-specific fact, you can either translate forever or normalise the fact once. Translation has to be re-run and can be forgotten; normalisation is a one-time cost that keeps paying.

## The one-time transfer, and what it taught me

I still had to move existing state across, so I packaged it. That surfaced three categories I hadn't thought about.

**Renaming isn't enough on its own.** The directory names are the index, but absolute paths are also embedded *inside* files — run manifests recording a project path, config listing repository roots, transcripts recording a working directory. 31 files needed rewriting. Which raised the question of what *not* to rewrite: `.git` object files contain paths in compressed blobs, and a blind search-and-replace corrupts the repository. Skipping `.git/` entirely was the fix.

**Some state is machine-born and must not travel.** This is the list I'd write down before packaging anything:

- A live session registry with PIDs. Copy it and the other machine believes in processes that don't exist there.
- Shell snapshots captured from the local shell. Wrong shape on a different OS.
- A compiled binary. Mine is ELF; the MacBook is arm64. There's a marker file recording `uname -sm` next to it precisely so a stale binary gets rebuilt, and both belong nowhere near the archive.
- Device-bound credentials. Copying an auth token that's pinned to a device produces a confusing half-working state; logging in fresh takes ten seconds.
- Caches and plugin trees. Regenerated, and they were 40% of the bytes.

I built the archive as an **allowlist**, not everything-minus-exclusions. It's safer to forget to include something than to forget to exclude a private key.

**Tracked config files should not contain absolute paths.** My path rewrite modified two files that were tracked in git. That would have left the second machine with a permanently dirty working tree and a conflict on the next pull.

The real fix wasn't better rewriting — it was making the files machine-agnostic. A config listing `["/home/vicente/github/r10"]` became `["~/github/r10"]`, with the consumer expanding `~` at read time:

```bash
roots="$(jq -r '.repos[]? // empty' "$CONFIG" | sed "s|^~|$HOME|")"
```

One file now serves both machines. If you sync config through git and a file needs different contents per machine, that's a design smell in the file, not a problem to solve in the sync layer.

## On mechanism: git is the wrong tool for this half

Config through git works well: small, low-churn, and history is genuinely useful.

State is the opposite. Mine is 26MB of append-only session transcripts, one file per session. Every commit stores a new blob of a growing file, so the repository would gain tens of megabytes a week for history nobody will read. And two machines both writing a shared `history.jsonl` produces merge conflicts in a file you would never want to resolve by hand.

The property that makes continuous file sync viable here is that **each session writes its own uniquely-named file**. Two machines almost never touch the same file, so the conflict risk that usually kills this approach mostly doesn't apply. That's worth checking before choosing a mechanism — it's a property of the data layout, not of the sync tool.

## What I'd take from this

**Check what your identifiers are made of.** Anything derived from a hostname, a home directory, a mount point or a username is machine-local, however stable it looks. That's fine until the state has to move.

**Prefer normalising the environment over translating the data.** A symlink is smaller than a migration script, and it can't be forgotten.

**Package with an allowlist and know what's machine-born.** Live PIDs, compiled binaries, and device credentials all look like ordinary files.

**A file that needs different contents on different machines shouldn't be tracked.** Either make it path-independent or don't sync it. Rewriting tracked files at transfer time trades one problem for a permanent one.
