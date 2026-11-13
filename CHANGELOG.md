# Changelog

## 0.1.1-stella.2

- `devvm forward`: two calls at once start one forward; a forward is found by its pid file and its arguments, so `--close` never signals another process; `stop` and `rm` close a VM's forwards; changing a running VM's ports keeps them.
- `devvm create` forwards no ports unless `--ports` is given.
- Snapshots never show as VMs in `devvm ls` or `devvm space`; a snapshot ID that would name a project VM is refused.
- `--vm NAME` resolves the same way for every verb; `rm --vm` deletes a VM whose folder is gone.

## 0.1.1-stella.1

The Stella fork (keepmeside/devvm, branch `stella/headless`), on 0.1.0. Hidden verbs for driving devvm from a program, without a terminal and without editing `~/.ssh/config`:

- `devvm create [--json] [--from-snapshot ID]`: makes the folder's VM like `devvm` does, or starts it, and returns instead of opening a shell.
- `devvm start [--vm NAME]`: starts a VM and sets it up as before its shell.
- `devvm exec [--vm NAME] [-i] [--root] -- ARGV...`: runs a command over devvm's own ssh config, with its exit code; `-i` passes input, `--root` runs it with `sudo -n -H`.
- `devvm forward [--vm NAME] [--json] [--close] PORT`: forwards a guest loopback port to a free loopback port the computer chooses, so two VMs can forward the same guest port.
- `devvm snapshot save [--vm NAME] ID`, `devvm snapshot rm ID`, `devvm snapshot [--json]`: a VM saved as a Lima clone (stopped first, started again after), deleted, listed.
- `--vm NAME` on `stop`, `rm`, `start`, `exec`, `forward` and `snapshot save`: acts on the VM named, from its folder; `rm --vm` deletes the VM while its folder is still there, never the folder.

## 0.1.0

Released 2026-11-06.

The first release of devvm, which gives each project folder an isolated VM of its own, ready for you or a coding agent to work in. It runs on macOS 13 or newer and on Linux, on arm64 and x86_64, with Lima 2.0 or newer. It's tested on Apple silicon Macs and on Linux on both arm64 and x86_64, but not yet on Intel Macs.

- **A VM for each project folder:** running `devvm` in a folder makes its VM, or opens the one it has, and gives you a shell in it at the same path as on your machine. The VM mounts only that folder read-write, and with `--clone` it gets a clone of the repo of its own instead, whose work you fetch with `git fetch devvm`.
- **Ready in seconds:** every VM is a copy of a base image that devvm builds once, and the toolchains and coding agents are shared tools that VMs mount read-only. devvm detects node, python, rust and go from the project's files, and the built-in `docker`, `nix` and `desktop` layers, your own layers from `devvm layer new`, a project's `devcontainer.json` and `--from` with a container image change what a VM starts from.
- **Coding agents in the sandbox:** Claude Code, Codex and opencode run in the VM with your settings, skills and MCP servers. devvm keeps their settings and logins in a store of its own on your machine, `devvm agent import` copies them in from an agent you already use, `devvm agent login` makes an agent's login from the VM, and `devvm agent save` brings a VM's settings back to the store after you review the diff.
- **Network tiers and secrets:** every VM goes through a proxy that devvm runs for it, and its tier, which is open, internet, allowlist or none, decides what the proxy lets it reach. New VMs get the allowlist tier. On every tier, your secrets, such as your GitHub token and Claude Code token, stay on your machine, because the proxy puts them into the VM's requests.
- **`.git` protection:** on macOS, devvm locks a project's git hooks and config with a flag that even root in the VM can't clear, so code in the VM can't make your next git command on this machine run something.
- **Reaching VMs:** each running VM can be reached by name from ssh and from editors such as VS Code (`devvm code`), with your SSH agent only when you allow it. A VM forwards the ports you ask for, `devvm link` lets one VM reach a service that another VM publishes, and `devvm desktop` opens a VM's desktop in Screen Sharing on macOS, or serves it to a VNC viewer of your own on Linux.
- **Managing VMs and space:** `devvm ls`, `devvm stop`, `devvm restart`, `devvm update` and `devvm rm` manage VMs, and `devvm images`, `devvm gc`, `devvm space` and `devvm retention` show and clean up the images, shared tools and exports devvm keeps. devvm never deletes a project VM on its own, because it may hold unsaved work.
- **Installing and upgrading:** install devvm with Homebrew (`brew trust --formula devvmhq/tap/devvm`, then `brew install devvmhq/tap/devvm`) or with the install script, which checks the release's minisign signature and checksum and asks before it installs what devvm needs, such as Lima. `devvm doctor` checks this machine, and `devvm upgrade` installs a newer release once it has checked the signature.
- **For scripts:** many commands take `--json`, and `--no-input` makes devvm fail where it would ask, naming the flag that answers the question.
