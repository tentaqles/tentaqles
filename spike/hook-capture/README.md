# Hook capture spike

This folder is a one-time capture. It learns which JSON fields Claude Code
really sends to hooks. The code is throwaway. It is not part of the product.

## Privacy

The captured payloads hold your commands, prompts and file paths. They stay
on your machine. Only the report from `reduce.py` leaves your machine.
Delete the scratch folder when you are done.

## How to run it

1. Pick a scratch folder inside one client's folder, for example
   `C:\repos\<client>\hook-capture-scratch`.
2. Run `python spike/hook-capture/setup.py <that folder>`.
3. Open a terminal in that folder and start Claude Code as you normally do.
   If Claude Code asks whether to trust this folder or its hooks, say yes.
4. In that one session, do each of these, one per prompt, and let each one finish:
   a. Ask: "Run this exact shell command: ls /no/such/folder-xyz" (a failed Bash command).
   b. Ask: "Run this exact PowerShell command with the PowerShell tool if you have one: Get-Item C:\no\such\file-xyz" (a failed PowerShell command; skip if Claude has no PowerShell tool).
   c. Ask: "Use the Edit tool to replace the text ZZZ-not-there with x in a file named nothing-here.txt, without creating it first" (a failed edit).
   d. Ask: "Run this exact shell command: echo capture-ok" (a successful command).
   e. Ask: "Run this exact shell command: sleep 60", and press Esc while it runs (an interrupted call).
   f. Ask: "Run this exact shell command: git push --dry-run", and when Claude Code asks for permission, choose No (a declined call). If it does not ask, skip.
   g. If an MCP server is connected: ask for one MCP tool call that will fail (for example a lookup of an id that does not exist). Skip if none is connected.
   h. Type `/exit` to end the session.
5. Run `python spike/hook-capture/reduce.py <that folder>\.capture` and send the printed report.
   Then delete the scratch folder.
