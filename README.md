# sbx-herdr

A small quality-of-life plugin for using Docker Sandboxes (`sbx`) in Herdr.
It can register existing sandboxes as Herdr SSH machines and shows sandbox
agents in local panes, so you can tell which panes are running Claude, Codex,
or a shell without switching between them.

Keep using your usual `sbx` commands. No launcher, key binding, or Herdr config
changes needed.

## Install

Requires macOS or Linux, Herdr 0.9.3+, Docker Sandboxes, and Go 1.23+.

```sh
herdr plugin install dvdksn/sbx-herdr
herdr plugin action invoke dvdksn.sbx-herdr.refresh
```

Then run `sbx` in a local Herdr pane:

```sh
sbx run claude
sbx exec -it my-sandbox bash -il
sbx env run
```

Agents appear as `sbx-claude`, `sbx-codex`, and so on, including kit agents
(a `sbx-kit-devin` sandbox appears as `sbx-devin`). Shells appear as
`sbx-shell`. Environment runs appear as their sandbox's agent when the sandbox
is named by `--name` or a literal `name:`, otherwise as `sbx-env`. Labels may
take a few seconds to update.

The plugin supports local `sbx run`, `sbx exec`, and `sbx env run` commands.
It doesn't yet identify agents launched from inside a sandbox shell.
Automatic attachment detection does not support cloud, SSH, or `sbx env exec`.
For kit-based launches, use `--name` to help identify the sandbox.

## Sandbox manager

Configure sandbox SSH access once with `sbx setup ssh`, then open the manager
in your local Herdr session:

```sh
herdr plugin action invoke dvdksn.sbx-herdr.manage
```

A floating popup lists local sandboxes and saved connections. Use `h/l` or
Left/Right to switch between **Add connection** and **Remove connection**,
`j/k` or Up/Down to select, and Enter to act. Press `r` to refresh or `q`/Esc
to close. Starting a stopped sandbox and removing a connection both ask for
confirmation with `y`.

The table separates **Sandbox state** (Running/Stopped) from **Herdr**
(Added/Not added). **Added (off)** means the saved connection is disabled.
In the remove view, **Herdr session** names the remote terminal session;
`default` is Herdr's standard session.

Adding calls Herdr's native machine setup for `<sandbox>.sbx`, using the
`default` session. Herdr handles installation and approval prompts. Existing
connections are reused and enabled if disabled. Removing a connection leaves
the sandbox and remote sessions intact, and works for deleted sandboxes too.

The manager runs on Local. Herdr popups belong to the machine whose server
opens them; a local popup cannot currently display over a remote machine view.
Select **Local** before opening the manager. Remote panes use Herdr's own agent
detection; the plugin's automatic labels describe local SBX attachments.

### Keyboard shortcut

Add this to `~/.config/herdr/config.toml` (or your `HERDR_CONFIG_PATH`):

```toml
[[keys.command]]
key = "prefix+shift+s"
type = "plugin_action"
command = "dvdksn.sbx-herdr.manage"
description = "Manage sandboxes"
```

Run `herdr server reload-config`, then press your prefix (default `Ctrl+B`)
followed by `Shift+S` to open the manager. Replace any older `add-sandbox` or
`remove-sandbox` bindings with this one.

## Contributing

Issues and pull requests are welcome. To build and run the checks:

```sh
make test check build
```

After rebuilding a linked plugin, reload it:

```sh
herdr plugin disable dvdksn.sbx-herdr
sleep 5
herdr plugin enable dvdksn.sbx-herdr
herdr plugin action invoke dvdksn.sbx-herdr.refresh
```
