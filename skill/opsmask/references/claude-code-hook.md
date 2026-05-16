# Claude Code Hook

`opsmask install claude-code` installs a project-scoped Claude Code Bash hook.
When active, non-trivial Bash commands are rewritten through OpsMask before the
command output reaches the agent context. The command itself may still be
visible in Claude Code's tool-call UI; the protection is for stdout/stderr
bytes.

The regular `opsmask exec` path still rejects shells unless the trusted project
policy allows the requested command. The Claude Code hook is a separate,
project-opted-in mode gated by a per-user hook secret and audit logging.

Skip-list pass-throughs such as `ls`, `pwd`, and narrowly-shaped `git status`
are written to `pass_through.log`. Wrapped commands write `source: "hook"`
records to `exec.log`.
