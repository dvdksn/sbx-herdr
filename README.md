# sbx-herdr

A small quality-of-life plugin for using Docker Sandboxes (`sbx`) in Herdr.
It shows sandbox agents in the sidebar, so you can tell which panes are running
Claude, Codex, or a shell without switching between them.

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
Cloud, SSH, and `sbx env exec` aren't supported.
For kit-based launches, use `--name` to help identify the sandbox.

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
