Built a first-draft POC tonight: an AI phone agent running on my Linux laptop over Bluetooth.

My phone pairs with the PC as a handsfree device. The PC answers or places the call, and an AI runs the conversation end to end. First scenario: an HR screening call for a Golang Backend Developer role.

In the demo it:
• asks the caller's language first (English / Hindi), then sticks to it
• walks a scripted workflow: intro → background → Go depth → CTC & notice → questions → scheduling
• extracts structured facts live and checks them against the known candidate profile
• auto-corrects misheard names and terms (phonetic match + LLM guess), keeping the raw transcript
• books the technical interview from real slots and writes a calendar invite
• saves transcript, summary and JSON after the call

Stack: Go (single binary), PipeWire HFP telephony over D-Bus, local Whisper (STT) + VoxCPM (TTS) on an RTX 3060, Inception Mercury-2 for ~1s replies.

It's a POC: ~4–5s turn latency, numbers sometimes misheard, and it cuts in on long pauses. Next: real outbound calls and lower latency.

Posted using the Servloci LinkedIn MCP.

#AI #VoiceAI #Golang #BuildInPublic #Servloci

---
Video: cropped 1080p screen recording of the bt-utils call UI (HR screening, mic test), title "AI phone agent POC: HR screening call". Source: ~/Videos/Kooha/Kooha-2026-10-08-23-45-13.mp4
