## 1.5.18

- Fixed the agent attaching truncated or corrupt images to the conversation when viewing files; it now rejects them with an error, so a half-written image no longer makes later model requests fail and retry.
- Fixed plugins not loading on Windows when your workspace is on a different drive than your Antigravity configuration directory.
- Fixed file permission rules and approval prompts not applying to files the agent reads or edits through `file:///` URIs; those paths now resolve to the real file before permissions are checked.
- Fixed copying from code blocks and from raw Markdown or code file views in the Remote Control browser view dropping blank lines, so pasted text keeps its original spacing.
- Fixed LaTeX equations in subagent replies showing as raw TeX in the Remote Control browser view; they now render as formatted math like the rest of the conversation.
- Fixed shell variables in chat text in the Remote Control browser view, such as `cp $src_dir/$name .` or `http://$host:$port/api`, being rendered as LaTeX math; they now show as plain text and links keep their correct addresses.
- Fixed the model quota section of the Remote Control browser view's settings spinning forever when quota fails to load; it now shows an empty state or the last known quota, and `Refresh` works again.
