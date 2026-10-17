# Post 7 — findai (2026-10-17)

LinkedIn: urn:li:share:7517265778287497216 (link card → https://github.com/ivikasavnish/findai)
X: https://x.com/i/status/2111500093769875868

## LinkedIn

I keep forgetting `find` syntax. Was it -iname or -name? -type d or -type f? Does -mtime -7 mean newer or older?

So I wrapped it. Now:

find -ai "find folder named apiorm"

→ $ find . -type d -name apiorm
→ ./Desktop/apiorm

How it works:
🔹 The AI turns plain English into find arguments, returned as a JSON array
🔹 It prints the command it built, so you learn the syntax while you use it
🔹 It runs /usr/bin/find directly, without going through a shell, so there's no injection risk
🔹 It asks before running anything with -delete, -exec or -ok
🔹 Without -ai, it hands off to plain find. Scripts don't notice the difference

Backends: Claude CLI (default) or a local model through Ollama, for fully offline use.

~150 lines of Go. One binary. Drop it in ~/.local/bin and it replaces find.

Open source, MIT: https://github.com/ivikasavnish/findai

Which CLI tool should get the -ai treatment next? tar? ffmpeg? awk?

#golang #cli #linux #devtools #ai #opensource

## X

Never remember `find` flags again.

find -ai "find folder named apiorm"
→ $ find . -type d -name apiorm

AI writes the command, shows it, runs it. Asks before -delete/-exec. Plain find unchanged.

Go, Claude or local Ollama.
github.com/ivikasavnish/findai

#golang #cli #linux
