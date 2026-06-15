# Shell integration (OSC 133 prompt markers)

Constellate tracks per-session activity — `active`, `idle`, `awaiting-input`, or `unknown` — and
surfaces it as badges in the sidebar, overview grid, and dashboard.

**Without shell integration**, activity is inferred from output timing: a session that has been
quiet for a while is guessed to be idle. A session that last printed a prompt-like line may be
inferred as awaiting input, but the heuristic can be wrong.

**With OSC 133 prompt markers** (the FinalTerm / iTerm2 / VS Code shell-integration protocol),
the agent receives explicit signals from your shell at each prompt boundary, which lets Constellate
reliably distinguish "idle at a prompt" from "running a command" and "a command that has paused
waiting for you to type."

## Is this already enabled?

Many modern terminal setups already emit OSC 133 markers automatically:

- **iTerm2** — enabled by default with shell integration installed (`it2-utilities`)
- **VS Code integrated terminal** — built-in (`terminal.integrated.shellIntegration.enabled`)
- **Warp** — built-in
- **Starship** — emits `precmd` / `preexec` equivalents; configure `[shell]` section to enable OSC 133

If your prompt already produces these, no extra configuration is needed.

## Manual setup

Add the following snippets to your shell rc file. The three marker sequences are:

| Sequence | Meaning |
|---|---|
| `\e]133;A\a` | Prompt about to be drawn (prompt-start) |
| `\e]133;C\a` | Command about to execute (command-start) |
| `\e]133;D\a` | Command finished (command-finished) |

### bash (`~/.bashrc`)

```bash
# Constellate / OSC 133 shell integration
__constellate_prompt_start() { printf '\e]133;A\a'; }
__constellate_command_start() { printf '\e]133;C\a'; }
__constellate_command_done()  { printf '\e]133;D\a'; }

PROMPT_COMMAND="${PROMPT_COMMAND:+$PROMPT_COMMAND; }__constellate_prompt_start"

# Wrap PS0 to emit command-start just before execution
PS0='$(__constellate_command_start)'

# Emit command-done after each command via DEBUG + PROMPT_COMMAND combo
# (simplest approach: emit D at the next prompt boundary)
PROMPT_COMMAND="${PROMPT_COMMAND}; __constellate_command_done"
```

A simpler single-line alternative if you are not chaining other `PROMPT_COMMAND` hooks:

```bash
PROMPT_COMMAND='printf "\e]133;D\a\e]133;A\a"'
PS0='$(printf "\e]133;C\a")'
```

### zsh (`~/.zshrc`)

```zsh
# Constellate / OSC 133 shell integration
function _constellate_precmd()  { print -Pn '\e]133;A\a\e]133;D\a' }
function _constellate_preexec() { print -Pn '\e]133;C\a' }

autoload -Uz add-zsh-hook
add-zsh-hook precmd  _constellate_precmd
add-zsh-hook preexec _constellate_preexec
```

## Fallback behaviour

Shell integration is entirely opt-in. Without it, Constellate falls back to output-timing analysis
combined with a screen-tail heuristic (scanning the last visible line for common prompt patterns).
The `unknown` activity state is shown when neither method produces a confident signal. No data is
ever sent off-machine; the markers are consumed by the in-process VT emulator.
