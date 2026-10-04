**I made Age of Empires 3 listen to me so I can play lazier.** 🎮🗣️

After work I just want to chill and play AoE3. Hotkeys, idle villagers and building placement all keep my hands busy, and I wanted something easier. So this weekend I built a small tool: I say a phrase and the game gets the keystrokes.

🗣️ "train five villagers"
🗣️ "idle three come here" sends idle villagers to wherever my mouse is
🗣️ "build house here"
🗣️ "cut wood", "mine gold", "build tower here"
🗣️ "घर बनाओ" works too, because it understands Hindi as well as English

How it works:
• Vosk handles speech recognition fully offline. No cloud, no API keys, no LLM bill.
• A virtual uinput keyboard and mouse, so the game sees real key presses (it even works through Proton)
• The recognizer only listens for my command phrases, so random talk mostly gets ignored
• Custom bindings in the game's own hotkeys.con call AoE3 console commands, like "pick the next idle villager and send it to the cursor without moving the camera"

Honestly, the bigger reason I built it was to learn by doing. Some of what I learned:

1. "cut wood" didn't work at first. The recognizer runs in grammar mode and only knows phrases I list, and I never listed that one. The model knew the words fine; my config was the problem.
2. AoE3 has no "tower" building. It's called an outpost, so now "build tower" maps to the outpost.
3. Firing a command early makes the game feel fast, but it cuts the sentence mid-word, and the leftover "wood" showed up as a separate stray phrase.
4. Now it saves every voice clip with what it heard and what it did. Later I can fine-tune on my own accent, still locally and for free.

None of this is a product. It's a gaming quality-of-life hack that taught me more about speech pipelines than any tutorial did.

Build the silly thing. You learn the real thing along the way.

#buildinpublic #gaming #speechrecognition #python #learningbydoing #ageofempires
