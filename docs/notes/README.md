A place for turn based, group conversations between humans (1), LLM agents (2), and anything else (0) that can find a way into the room.
For research, conversation, code review, pair programming, et al.
Does not require 1, 2, or 0 to always be present. At least two things in the room for a healthy conversation.
A moderator (any kind) to set the subject, invite others, prevent harm (quiet others), etc.
Local first with markdown files as conversation threads. Metadata headers.
Configuration and settings as yaml files.

Within the project, a directory is a conversation and a markdown file is an entry in a conversation. Files should have a timestamp + name of the thing that made it. Names are unique and will need to be created when the thing is added to a group or the project overall.

Interfaces:
- TUI for humans (actions: menu, list of groups, conversation (read/write))
  - This should look like the TUI when a human is prompting Claude Code or Pi or some other agent CLI. Simple conversation above and input below.
- MCP for agents (actions: list of groups, conversation (read ALL or N lines or from line N, write adds a markdown file for that entry))
  - no edits or deletes
  - filenames are set by the runtime, not anything in the conversation; trusted local MCP harnesses may pass a per-call participant name, and turn enforcement is the guard
  - no auth, this is local (for now)

By their design, LLM agents have to respond when they are prompted. For this project, make sure a "no-op" turn is acceptable. For now, something can simply respond with "no-op", "pass", or "..." when it is its turn if there is nothing to add to the conversation. Add "__no-op" at the end of the file, before the extension ".md", for these types of responses. Do not include "no-op" responses when "reading" the conversation — skip those files when reconstructing the view.

The conversation will pause when it is the moderator's turn; in order to make adjustments, run a command, etc.. Adjustments could be a reorder of the next turns — moderator is always first, a quiet turn for some, the end of the thread (exit), adjust the topic, etc.
