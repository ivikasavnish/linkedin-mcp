**I shipped a LinkedIn MCP server this week. Then I ran a security review on it.** 🔍

It found 3 holes in about 500 lines of Go. None were exotic. All were the kind you skip when a tool "only runs on localhost".

**1. Any file could become a public "image"**
The tool took a file path and uploaded it. Nothing stopped an AI agent, tricked by a prompt hidden in a web page, from attaching ~/.ssh/id_rsa.
*Fix:* check the file's actual bytes, not the extension. Only real jpg/png/gif goes out.

**2. "Localhost" is not authentication**
The server listened on 127.0.0.1 with no key. Any process on my machine could post as me. A web page could even reach it through DNS rebinding.
*Fix:* a required bearer key, plus rejecting any request whose Host isn't local.

**3. Config read from the current folder**
It loaded .env from wherever it started. Clone a repo with a planted .env and the server takes its settings.
*Fix:* read config from one fixed path only.

**The lesson:**
MCP servers hand real powers to an AI agent: your files, your accounts, your name. Treat every tool input as untrusted, because the agent may be repeating something it read.

Bonus: the review also caught that delete didn't work at all. 🙃

Building MCP tools? What's on your security checklist? 👇

#MCP #AISecurity #ClaudeCode #Golang #AppSec #DeveloperTools
