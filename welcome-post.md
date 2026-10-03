**Posting to LinkedIn from my terminal now.** 🚀

I built a small MCP server so Claude (or any MCP client) can publish straight to LinkedIn. This post went out through it.

**Why?** The LinkedIn editor has no bold, no italics, no headings. And if you paste text with (brackets), @mentions or underscores, the API quietly cuts your post short.

**What it handles:**
▸ **Formatting** — **bold** and *italic* text like this, which the post box can't do
▸ **Safe text** — reserved characters escaped, so nothing gets truncated
▸ **Clickable hashtags** — turned into proper tags
▸ **Images & link cards** — attach media or a URL preview
▸ **Drafts** — save it, review it, publish when ready
▸ **Company pages** — post as an organization, not just yourself

**How I use it:**
1. Write the idea in plain words
2. Let Claude shape and format it
3. Check the preview, then publish

Small tool, written in Go, runs locally. Your token never leaves your machine.

Should I open-source it? Tell me in the comments 👇

#MCP #ClaudeCode #LinkedIn #Golang #DeveloperTools #Automation
